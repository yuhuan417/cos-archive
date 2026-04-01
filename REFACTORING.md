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

**当前代码规模**：~1600 行，0 测试，Go 1.21+，单 package 结构。

**依赖状态（2026-04-01）**：
- 已移除失效的 COS SDK fork
- 当前使用官方 `github.com/tencentyun/cos-go-sdk-v5 v0.7.24`

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

### 1.4 ✅ 移除失效的 COS SDK fork

**问题**：`go.mod` 通过 `replace` 指向 `github.com/yuhuan417/cos-go-sdk-v5 v0.7.22-yuhuan417`，该仓库已不可访问，影响新环境拉取依赖与后续升级。

**处理**：
- 删除 `replace github.com/tencentyun/cos-go-sdk-v5 => github.com/yuhuan417/cos-go-sdk-v5 v0.7.22-yuhuan417`
- 保留官方 `github.com/tencentyun/cos-go-sdk-v5 v0.7.24`
- 对比确认 fork 的核心改动仅为 multipart upload 的 `ContentLength` 从 `int` 改为 `int64`，该补丁已被官方版本吸收

**验证方式**：`go build ./...`

---

### 1.5 ✅ 收口本地恢复链路与 browse 正确性

**问题**：`download -> link -> browse` 链路对索引落盘位置的约定不一致；同时 browse 的父目录判断逻辑有误。

**处理**：
- `download` 将远端索引持久化到 `TargetDir/meta.json.remote`
- `link` 直接读取 `TargetDir/meta.json.remote`
- `browse` 优先读取 `TargetDir/meta.json.remote`，兼容回退 `TargetDir/meta.json`
- 修复 browse 的目录归类判断，统一用 `index.Entries.Dirs` 判断父目录是否存在
- 为 browse 补充明确的 `Content-Type`

---

## 阶段二：P1 — 正确性收口与配置治理 ✅ 已完成

> 下一阶段先解决“静默失败”和“错误分支走偏”的问题，再做结构性重构。

### 2.1 ✅ 配置文件加载与 action 级校验

**现状问题**：
- `config.go` 中 `os.Open` 失败仍然直接返回默认配置
- `Threads <= 0` 时可能死锁或 panic
- `WorkingDir` / `TargetDir` / `Port` / `FilePaths` 没有按 action 做必填校验

**计划修改**：
- `getConfig()` 中 `os.Open` 失败直接 `Fatal`
- 新增 `validateConfig(action string, config Config)`，按 action 校验关键字段
- 至少覆盖：
  - `backup` / `download` / `fsck` / `verify`：`WorkingDir` 必填
  - `download` / `link` / `browse`：`TargetDir` 必填
  - `browse`：`Port` 必填
  - `backup` / `verify`：`FilePaths` 非空
  - `backup`：`Threads > 0`

---

### 2.2 ✅ 明确 `LoadRemote()` 的“首次运行”与“异常失败”分支

**现状问题**：
- `backupFiles()` 直接忽略 `remoteIndex.LoadRemote()` 返回值
- 远端确实不存在索引，与网络/鉴权失败，当前会走到同一条逻辑分支

**计划修改**：
- 引入明确的 `ErrIndexNotFound` 或等价错误分支
- `backup` 仅在“远端索引不存在”时按首次运行处理
- 其他 `LoadRemote()` 失败保持失败返回，不再静默降级

---

### 2.3 ✅ 补全 `chunkHash` 的 I/O 错误传播

**现状问题**：
- `io.Copy`、`io.CopyN`、`Seek` 返回值仍未检查
- 文件被截断、读取失败或 seek 失败时，仍可能产出错误 hash

**计划修改**：
- 检查所有 `io.Copy` / `io.CopyN` / `Seek` 错误
- 将这些错误继续向 `GenerateLocal` / `verify` / `download` 透传

---

### 2.4 ✅ `main.go` 收口 action 分发

**现状问题**：
- 未知 action 现在会静默退出成功
- 入口分支仍是长 `if/else`

**计划修改**：
- 改为 `switch`
- 为未知 action 增加 `default -> Fatal("Unknown action:", *action)`
- 在校验配置后再进入各 action

---

## 阶段三：P1.5 — 测试基线

> 先建立最低限度的回归保护，再继续做结构拆分和并发改造。

### 3.1 优先补单元测试

**优先测试的函数**：

| 函数 | 文件 | 理由 |
|------|------|------|
| `chunkHash` | chunk.go | 核心哈希算法，错误会导致数据丢失 |
| `ChunkKey.MarshalText` / `UnmarshalText` | chunk.go | 序列化正确性直接影响索引完整性 |
| `getConfig` / `validateConfig` | config.go | 配置解析和约束影响所有功能 |
| `Index.Load` / `Index.Save` | index.go | 本地索引读写已经成为恢复链路的一部分 |
| browse 目录归类逻辑 | browse.go | 最近刚修过，适合补回归测试 |

### 3.2 测试完成前暂缓的大改动

以下事项保留，但应放在测试之后：
- `Index` 拆分
- upload 并发模型调整
- browse 模板分离

---

## 阶段四：P2 — 可维护性与性能优化

> 正确性和测试稳定后，再做结构优化。

### 4.1 复用 COS 客户端实例

**问题**：`NewCOS()` 在项目中被多次调用，每次都会新建 `http.Client` 和 `cos.Client`。

**范围**：
- `index.go`：`downloadRemote` / `getRemoteHash` / `UploadRemote` / `getLatestIndex` / `DeleteOutdatedChunks`
- `chunk.go`：`scanRemoteChunksMap`
- 各 action 入口：避免重复构造 client

### 4.2 先理顺 upload 错误返回，再谈 `errgroup`

**问题**：
- `backup.go` 的 worker 内仍直接 `Fatal`
- 直接替换成 `errgroup` 收益有限，且错误模型仍然不清晰

**计划顺序**：
1. 让 `uploadPayload` 返回 `error`
2. 再考虑 `errgroup` 收口 worker 错误

### 4.3 拆分 `index.go`

当前 `index.go` 仍承担多种职责，建议在测试到位后拆分为：
- `types.go`
- `index.go`
- `scanner.go`

### 4.4 Browse 模板分离

当前 `browse.go` 仍有 100+ 行 HTML 字符串拼接，建议后续改为 `embed.FS + html/template`。

---

## 阶段五：P3 — 长期改进

> 可分多个迭代逐步推进

### 5.1 继续升级 Go 版本

```diff
// go.mod
-go 1.21
+go 1.22
```

当前代码已经至少需要 Go 1.21（已使用 `log/slog`）。后续如果团队环境允许，再统一提升到 Go 1.22+。

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

### 5.2 引入错误类型层次

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

### 5.3 添加 context 支持

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

### 5.4 添加进度显示

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

### 5.5 `restoreChunk` 限速值可配置化

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
✅ 2026-04-01 ─── 额外收尾
                  ├── ✅ 1.4 移除失效 COS SDK fork，切回官方 v0.7.24
                  ├── ✅ 1.5 收口 download/link/browse 索引链路
                  └── ✅ go.mod 最低版本同步到 Go 1.21
                  └── 验证：go build 通过
✅ 2026-04-01 ─── 阶段二（P1）已完成
                  ├── ✅ 2.1 配置文件加载失败直接退出
                  ├── ✅ 2.2 action 级配置校验
                  ├── ✅ 2.3 区分远端索引不存在与远端读取失败
                  ├── ✅ 2.4 chunkHash I/O 错误传播
                  └── ✅ 2.4 main.go switch/default 收口
                  └── 验证：go build && 覆盖错误配置/未知 action

下一步 ─── 阶段三（P1.5）
            ├── 3.1 建立最小测试集
            └── 3.2 为后续重构建立回归保护
            └── 验证：go test ./...

后续 ─── 阶段四（P2）
            ├── 4.1 复用 COS 客户端
            ├── 4.2 调整 upload 错误返回
            ├── 4.3 拆分 index.go
            └── 4.4 browse 模板分离
            └── 验证：go build && 对比重构前后备份结果

长期 ─── 阶段五（P3，按需）
            ├── 5.1 Go 1.22+
            └── 5.2-5.5 渐进式改进
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
| config.go | `os.Open` 失败后静默回落默认配置 | 🔴 高 | ✅ P1 已修复 |
| chunk.go | `os.Open` 错误忽略 → panic | 🔴 高 | ✅ P0 已修复（返回 error） |
| chunk.go | `io.Copy` / `io.CopyN` / `Seek` 错误未检查 | 🔴 高 | ✅ P1 已修复 |
| index.go | `metaHash` 中 `os.Open` 忽略 | 🟡 中 | ✅ P0 已修复（返回空 hash） |
| cos.go | `url.Parse` 错误忽略 | 🟡 中 | ✅ P0 已修复 |
| download.go | 无限重试循环 | 🔴 高 | ✅ P0 已修复（maxDownloadRetries=10） |
| index.go | `GenerateLocal` chunkHash 错误 | 🟡 中 | ✅ P0 已修复（移除并记录错误） |
| verify.go | `verifySingleFile` chunkHash 错误 | 🟡 中 | ✅ P0 已修复（标记 unmatched） |
| download.go | `downloadChunk` chunkHash 错误 | 🟡 中 | ✅ P0 已修复（跳过匹配检查） |
| backup.go | `LoadRemote()` 返回值被忽略 | 🔴 高 | ✅ P1 已修复 |
| main.go | 未知 action 静默退出成功 | 🟡 中 | ✅ P1 已修复 |
| backup.go:33 | `Fatal("Upload file error:", ...)` 终止进程 | 🟡 中 | 待 P1 处理 |
| cos.go:94 | `ScanFiles` 中 `Fatal` 终止 | 🟡 中 | 待 P1 处理 |
| 全项目 ~15 处 | `Fatal()` 替代 error 返回 | 🟢 低 | 待 P2 处理 |
