# COS-Archive 重构计划

> 基于代码架构分析，本文档将重构建议转化为可执行的落地方案。
> 按优先级分阶段执行，每个阶段独立可交付、可验证。

---

## 背景与约束

COS-Archive 是基于腾讯云 COS **深度归档存储（DEEP_ARCHIVE）** 的增量备份工具。

**核心业务约束（重构时不可违反）**：
- HEAD 请求（GetHeader）极廉价，用于避免昂贵的上传/下载/删除
- Upload 后有 **180 天最低存储计费期**，提前删除仍需支付剩余费用
- Download 必须先 Restore（解冻），耗时数小时至天
- 代码中大量使用 HEAD 优先策略是**正确的成本优化**，重构不应改变此行为

**当前代码规模**：~1600 行，0 测试，Go 1.14，单 package 结构。

---

## 阶段一：P0 — 数据安全与正确性修复 ✅ 已完成

> 完成时间：2026-03-29 | 影响范围：config.go, chunk.go, download.go, index.go, cos.go, verify.go

### 1.1 ✅ 修复 `ChunkPrefix` JSON Tag（config.go）

**问题**：`config.json` 中的 `"Prefix"` 字段无法被反序列化到 `ChunkPrefix`，程序始终使用默认值 `"data/"`。
如果用户修改了 Prefix，修改不会生效。

**影响范围**：所有使用 `config.COS.ChunkPrefix` 的路径拼接（backup/restore/download/fsck/browse）。

**修改**：

```diff
 // config.go
 type COSConfig struct {
     URL         string
     ID          string
     Key         string
-    ChunkPrefix string
+    ChunkPrefix string `json:"Prefix"`
     Class       string
     Retries     int
 }
```

**验证方式**：修改 `config.json` 中 `Prefix` 为非默认值（如 `"test-data/"`），确认程序使用新值。

---

### 1.2 ✅ 修复关键路径的错误处理

#### 1.2.1 ✅ chunk.go — `chunkHash` 空指针崩溃

**问题**：`os.Open` 错误被 `_` 忽略，文件打开失败时 `f` 为 nil，`defer f.Close()` 会 panic。

```diff
 // chunk.go
-func chunkHash(path string, size int64) HashType {
+func chunkHash(path string, size int64) (HashType, error) {
     h := sha1.New()
-    f, _ := os.Open(path)
+    f, err := os.Open(path)
+    if err != nil {
+        return HashType{}, fmt.Errorf("chunkHash: open %s: %w", path, err)
+    }
     defer f.Close()
     if size < 0xF000 {
         io.Copy(h, f)
     } else {
         io.CopyN(h, f, 0x5000)
         f.Seek(size/3, 0)
         io.CopyN(h, f, 0x5000)
         f.Seek(size-0x5000, 0)
         io.CopyN(h, f, 0x5000)
     }
     var b HashType
     copy(b[:], h.Sum(nil))
-    return b
+    return b, nil
 }
```

**联动修改**（已全部完成）：
- ✅ `index.go` — `GenerateLocal` 中的哈希计算：失败时从索引移除该文件并 `slog.Error`
- ✅ `verify.go` — `verifySingleFile` 中的哈希校验：失败时标记为 unmatched
- ✅ `download.go` — `downloadChunk` 中的本地文件校验：哈希失败时跳过本地匹配检查，继续下载

#### 1.2.2 ✅ config.go — 配置解析错误静默

**问题**：`io.ReadAll` 和 `json.Unmarshal` 错误被忽略，配置文件格式错误时静默使用默认值。

```diff
 // config.go
-    byteValue, _ := io.ReadAll(jsonFile)
-    json.Unmarshal(byteValue, &config)
+    byteValue, err := io.ReadAll(jsonFile)
+    if err != nil {
+        Fatal("Read config file error:", err)
+    }
+    if err := json.Unmarshal(byteValue, &config); err != nil {
+        Fatal("Parse config file error:", err)
+    }
```

#### 1.2.3 ✅ index.go — `metaHash` 空文件处理

**问题**：`os.Open` 错误被忽略，首次运行无本地缓存时计算空 hash，但实际行为是可接受的（空 hash 不等于远程 hash，会触发下载）。

```diff
 // index.go
 func (index *Index) metaHash(path string) string {
     h := sha1.New()
-    f, _ := os.Open(path)
-    defer f.Close()
-    io.Copy(h, f)
+    f, err := os.Open(path)
+    if err != nil {
+        // 文件不存在时返回空 hash，触发远程下载
+        return ""
+    }
+    defer f.Close()
+    io.Copy(h, f)
     return hex.EncodeToString(h.Sum(nil))
 }
```

#### 1.2.4 ✅ cos.go — `url.Parse` 错误处理

```diff
 // cos.go - NewCOS
-    u, _ := url.Parse(config.URL)
+    u, err := url.Parse(config.URL)
+    if err != nil {
+        Fatal("Invalid COS URL:", config.URL, err)
+    }
```

---

### 1.3 ✅ 下载无限循环增加最大重试

**问题**：`download.go` 中 `downloadChunk` 的 `for {}` 循环没有退出条件，如果解冻过期或网络持续异常会永远循环。

```diff
 // download.go
+const maxDownloadRetries = 10
+
 func downloadChunk(config Config, cm ChunksMap) {
     slog.Debug("Downloading chunks")
     c := NewCOS(config.COS)
     for key := range cm {
         k := chunkPath(key)
         cp := path.Join(config.TargetDir, "chunks", k[len(k)-2:])
         // ...
         p := path.Join(config.COS.ChunkPrefix, k)
         lp := path.Join(cp, k)
+        retries := 0
         for {
+            if retries >= maxDownloadRetries {
+                Fatal("Download exceeded max retries:", p)
+            }
             // ... existing logic ...
             err = c.DownloadFile(p, lp)
             if err != nil {
                 slog.Debug("Download file error", "path", p, "error", err)
+                retries++
             } else {
                 break
             }
         }
     }
 }
```

---

## 阶段二：P1 — 代码质量与可维护性

> 预计工时：3-5 小时 | 风险：中 | 每项可独立执行

### 2.1 复用 COS 客户端实例

**问题**：`NewCOS()` 在项目中被调用约 8 次，每次都会新建 `http.Client` 和 `cos.Client`。
HTTP 连接池无法复用，增加不必要的 TCP/TLS 握手开销。

**方案**：在 `Index` 中注入 COS 客户端，action 函数改为接收 `*COS` 参数。

```diff
 // index.go
 type Index struct {
     Entries       DirEnt
     DeletedChunks ChunkDeleteMarkMap
     config        Config
+    cos           *COS
 }

-func NewIndex(config Config) *Index {
+func NewIndex(config Config, c *COS) *Index {
     index := Index{}
     index.Entries.Files = make(FileInfoMap)
     index.Entries.Dirs = make(DirInfoMap)
     index.Entries.Links = make(LinkInfoMap)
     index.DeletedChunks = make(ChunkDeleteMarkMap)
     index.config = config
+    index.cos = c
     return &index
 }
```

**联动修改清单**：

| 文件 | 函数 | 修改内容 |
|------|------|---------|
| index.go | `downloadRemote` | 用 `index.cos` 替代 `NewCOS()` |
| index.go | `getRemoteHash` | 同上 |
| index.go | `UploadRemote` | 同上 |
| index.go | `getLatestIndex` | 同上 |
| index.go | `DeleteOutdatedChunks` | 同上 |
| backup.go | `backupFiles` | 创建 `*COS` 并传入 `NewIndex` |
| backup.go | `uploadFiles` → worker | 复用传入的 `*COS`（注意线程安全，可每个 worker 一个） |
| download.go | `downloadFiles` | 创建 `*COS` 并传入 |
| restore.go | `restoreFiles` | 同上 |
| fsck.go | `fsckRemote` | 同上 |
| verify.go | `verifyFiles` | 同上 |
| browse.go | `browseFiles` | 同上 |
| main.go | 各 action 分支 | 传递 `*COS` |

> **注意**：`cos.Client` 内部的 `http.Client` 是并发安全的（基于标准库），但 `upload` 的 worker 模式中每个 goroutine 可以共享同一个 `*COS` 实例。

---

### 2.2 用 `switch` 替代 if/else 链（main.go）

```diff
 // main.go
-    if *action == "browse" {
-        slog.Info("Action browse")
-        browseFiles(config)
-        return
-    }
-
-    lockFile, err := singleinstance.CreateLockFile(...)
-    // ...
-
-    if *action == "restore" {
-        slog.Info("Action: restore")
-        restoreFiles(config)
-    } else if *action == "download" {
-        // ...
-    } else if ...
+    if *action != "browse" {
+        lockFile, err := singleinstance.CreateLockFile(path.Join(config.WorkingDir, "pid.lock"))
+        if err != nil {
+            Fatal("An instance already exists")
+        }
+        defer lockFile.Close()
+    }
+
+    switch *action {
+    case "browse":
+        slog.Info("Action: browse")
+        browseFiles(config)
+    case "restore":
+        slog.Info("Action: restore")
+        restoreFiles(config)
+    case "download":
+        slog.Info("Action: download")
+        downloadFiles(config)
+    case "link":
+        slog.Info("Action: link")
+        linkFiles(config)
+    case "backup":
+        slog.Info("Action: backup")
+        backupFiles(config)
+    case "fsck":
+        slog.Info("Action: fsck")
+        fsckRemote(config)
+    case "verify":
+        slog.Info("Action: verify")
+        verifyFiles(config)
+    default:
+        Fatal("Unknown action:", *action)
+    }
```

---

### 2.3 拆分 index.go（374 行 → 3 个文件）

当前 `index.go` 包含了 6 种不同职责。建议拆分为：

| 新文件 | 行数(约) | 内容 |
|--------|---------|------|
| `types.go` | ~90 | `HashType`, `FileInfo`, `DirInfo`, `LinkInfo`, `DirEnt`, `FileInfoMap`, `DirInfoMap`, `LinkInfoMap`, `ChunkDeleteMarkMap` 及其序列化方法 |
| `index.go` | ~200 | `Index` struct, `NewIndex`, `Load`, `Save`, `LoadRemote`, `UploadRemote`, `getLatestIndex`, `getRemoteHash`, `metaHash`, `downloadRemote`, `DeleteOutdatedChunks` |
| `scanner.go` | ~80 | `scanSingleFile`, `GenerateLocal` |

**执行步骤**：
1. 创建 `types.go`，迁移数据结构定义（第 23-83 行）
2. 创建 `scanner.go`，迁移扫描逻辑（第 294-373 行）
3. `index.go` 保留索引管理逻辑
4. 同时将 `chunk.go` 中的 `ChunkKey` 定义移入 `types.go`
5. 确认编译通过（`go build ./...`）

---

### 2.4 Browse 模板分离（browse.go）

**问题**：100+ 行 HTML 直接用 `fmt.Fprintf` 拼接在 Go 代码中，难以维护。

**方案**：使用 `embed.FS` + `html/template`。

```
cos-archive/
├── templates/
│   ├── dir.html        # 目录列表页模板
│   ├── file.html       # 文件详情页模板
│   └── link.html       # 符号链接页模板
└── browse.go           # 使用 embed + template 渲染
```

```go
// browse.go
import "embed"

//go:embed templates/*.html
var templateFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))

func handleDir(m DirEnt, p string, w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    tmpl.ExecuteTemplate(w, "dir.html", struct {
        Path string
        DirEnt DirEnt
    }{p, m})
}
```

**同时修复**：`handleDir` 函数缺少 `Content-Type` header 的问题。

---

### 2.5 修复 browse.go 目录构建逻辑

**问题**：第 177 行检查 `index.Entries.Files[dp]` 来判断父目录是否存在，但应该检查 `index.Entries.Dirs[dp]`。

```diff
 // browse.go - browseFiles()
 for fp, fi := range index.Entries.Files {
     dp := path.Dir(fp)
     bn := path.Base(fp)
-    if _, ok := index.Entries.Files[dp]; !ok {
+    if _, ok := index.Entries.Dirs[dp]; !ok {
         dp = "/"
         bn = fp
     }
     // ...
 }
```

同样修复第 190 行和第 203 行的 Links 判断逻辑。

---

### 2.6 用 `errgroup` 替代手动 WaitGroup（backup.go）

```diff
 // backup.go
 import (
+    "golang.org/x/sync/errgroup"
-    "sync"
 )

-    wg := &sync.WaitGroup{}
-    ch := make(chan UploadCTX, config.Threads)
-    // ...
-    uploadFile := func(id int) {
-        defer wg.Done()
-        c := NewCOS(config.COS)
-        for ctx := range ch {
-            uploadPayload(c, config, ctx)
-        }
-    }
-    wg.Add(config.Threads)
-    for i := 0; i < config.Threads; i++ {
-        go uploadFile(i)
-    }
+    g, _ := errgroup.WithContext(context.Background())
+    g.SetLimit(config.Threads)
+    ch := make(chan UploadCTX, config.Threads)
+
+    // ... 发送任务到 ch ...
+
+    close(ch)
+    if err := g.Wait(); err != nil {
+        Fatal("Upload failed:", err)
+    }
```

> 需要 `go get golang.org/x/sync/errgroup`

---

## 阶段三：P2 — 长期改进

> 可分多个迭代逐步推进

### 3.1 升级 Go 版本

```diff
 // go.mod
-go 1.14
+go 1.22
```

升级后可用：
- `errors.Join`（合并多错误）
- `slices` / `maps` 标准库包
- 泛型（简化 map 操作）
- `slog` 成为标准库（已在使用）
- 更好的安全性和性能

**执行步骤**：
1. 修改 `go.mod` 版本
2. 运行 `go mod tidy` 更新依赖
3. 检查依赖兼容性
4. 编译测试

---

### 3.2 添加单元测试

**优先测试的函数**（按 ROI 排序）：

| 函数 | 文件 | 理由 |
|------|------|------|
| `chunkHash` | chunk.go | 核心哈希算法，错误会导致数据丢失 |
| `ChunkKey.MarshalText` / `UnmarshalText` | chunk.go | 序列化正确性直接影响索引完整性 |
| `buildChunksMap` | chunk.go | 备份去重的基础 |
| `getConfig` | config.go | 配置解析影响所有功能 |
| `parseInt64FromBytes` | chunk.go | 手写整数解析，无溢出检查 |

**测试文件结构**：
```
cos-archive/
├── chunk_test.go
├── config_test.go
└── index_test.go
```

**示例测试**：
```go
// chunk_test.go
func TestChunkKeyRoundTrip(t *testing.T) {
    original := ChunkKey{size: 12345, hash: HashType{0x01, 0x02, ...}}
    text, err := original.MarshalText()
    if err != nil {
        t.Fatal(err)
    }
    var decoded ChunkKey
    if err := decoded.UnmarshalText(text); err != nil {
        t.Fatal(err)
    }
    if original != decoded {
        t.Errorf("roundtrip failed: %v != %v", original, decoded)
    }
}
```

---

### 3.3 引入错误类型层次

```go
// errors.go
package main

import "errors"

var (
    ErrIndexNotFound  = errors.New("remote index not found")
    ErrChunkLost      = errors.New("chunk lost on remote")
    ErrConfigInvalid  = errors.New("invalid configuration")
    ErrRestorePending = errors.New("restore still in progress")
    ErrMaxRetries     = errors.New("exceeded max retries")
)
```

逐步替换 `Fatal()` 调用为 `return fmt.Errorf("...: %w", ErrXxx)`，使调用方可以用 `errors.Is` 做精细控制。

---

### 3.4 添加 context 支持

所有 COS 操作改为接收 `context.Context` 参数，支持 graceful shutdown：

```go
func backupFiles(ctx context.Context, config Config) error {
    // ...
    c.UploadFile(ctx, key, file, class, header)
    // ...
}
```

在 `main.go` 中注册信号处理：
```go
ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer cancel()
```

---

### 3.5 添加进度显示

备份和下载大量文件时添加进度信息：

```go
// 简单方案：定期打印
processed := atomic.Int64{}
go func() {
    ticker := time.NewTicker(5 * time.Second)
    for range ticker.C {
        slog.Info("Progress", "processed", processed.Load(), "total", total)
    }
}()
```

---

### 3.6 `restoreChunk` 限速值可配置化

```diff
 // config.go
 type Config struct {
     // ...
+    RestoreQPS int
 }

 // restore.go
-    rl := ratelimit.New(90)
+    qps := config.RestoreQPS
+    if qps <= 0 {
+        qps = 90 // 默认值
+    }
+    rl := ratelimit.New(qps)
```

---

## 执行路线图

```
✅ 2026-03-29 ─── 阶段一（P0）已完成
                  ├── ✅ 1.1 修复 ChunkPrefix JSON tag
                  ├── ✅ 1.2 修复错误处理（chunk/config/index/cos/verify）
                  └── ✅ 1.3 下载无限循环加重试上限
                  └── 验证：go build 通过

下一步 ─── 阶段二-前半（P1）
            ├── 2.1 复用 COS 客户端
            ├── 2.2 switch 替代 if/else
            └── 2.5 修复 browse 目录逻辑
            └── 验证：go build && 手动测试所有 action

后续 ─── 阶段二-后半（P1）
            ├── 2.3 拆分 index.go
            ├── 2.4 browse 模板分离
            └── 2.6 errgroup 替代 WaitGroup
            └── 验证：go build && 对比重构前后备份结果

长期 ─── 阶段三（P2，按需）
            ├── 3.1 升级 Go 版本
            ├── 3.2 添加单元测试
            └── 3.3-3.6 渐进式改进
```

---

## 风险与注意事项

1. **不要改变 HEAD 优先策略**：所有 `GetHeader` 前置检查都是关键的成本优化
2. **180 天计费期逻辑不可简化**：`DeleteOutdatedChunks` 中的日期计算是防止提前删除产生额外费用的核心保护
3. **遍历 map 时删除 key**：Go 中合法但需保持谨慎，重构时可考虑先收集待删除 key 再统一删除
4. **backup.go 的 nil 赋值**：第 42-44 行 `remoteIndex.Entries.{Files,Dirs,Links} = nil` 是为了释放大索引内存，重构时需保留此优化意图或用更安全的方式实现
5. **`parseInt64FromBytes`**：无溢出检查，极端情况下可能溢出。升级 Go 后可用 `strconv.ParseInt` 替代

---

## 附录：错误处理全景

| 位置 | 模式 | 严重程度 | 状态 |
|------|------|---------|------|
| config.go | `io.ReadAll` 错误忽略 | 🔴 高 | ✅ P0 已修复 |
| config.go | `json.Unmarshal` 错误忽略 | 🔴 高 | ✅ P0 已修复 |
| chunk.go | `os.Open` 错误忽略 → panic | 🔴 高 | ✅ P0 已修复（返回 error） |
| index.go | `metaHash` 中 `os.Open` 忽略 | 🟡 中 | ✅ P0 已修复（返回空 hash） |
| cos.go | `url.Parse` 错误忽略 | 🟡 中 | ✅ P0 已修复 |
| download.go | 无限重试循环 | 🔴 高 | ✅ P0 已修复（maxDownloadRetries=10） |
| index.go | `GenerateLocal` chunkHash 错误 | 🟡 中 | ✅ P0 已修复（移除并记录错误） |
| verify.go | `verifySingleFile` chunkHash 错误 | 🟡 中 | ✅ P0 已修复（标记 unmatched） |
| download.go | `downloadChunk` chunkHash 错误 | 🟡 中 | ✅ P0 已修复（跳过匹配检查） |
| backup.go:33 | `Fatal("Upload file error:", ...)` 终止进程 | 🟡 中 | 待 P1 处理 |
| cos.go:94 | `ScanFiles` 中 `Fatal` 终止 | 🟡 中 | 待 P1 处理 |
| 全项目 ~15 处 | `Fatal()` 替代 error 返回 | 🟢 低 | 待 P2 处理 |
