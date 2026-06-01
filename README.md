# COS Archive

[English](README.en.md)

COS Archive 是一个面向腾讯云 COS 归档存储的文件备份工具。它把可频繁访问的索引和真正占空间的数据块分开管理：索引始终使用标准存储，数据块可以上传到 `DEEP_ARCHIVE` 等低成本归档存储。常规备份、浏览、校验和清理尽量依赖索引、对象列表和自描述路径，避免读取归档对象内容，也避免把大量冷对象从归档层唤醒。

这个项目适合长期保存照片、个人文件、历史备份等冷数据：写入不频繁，恢复可以接受先解冻，日常维护希望尽量少触碰归档对象。

## 归档存储优化

归档存储便宜，但不适合频繁 `HEAD`、`GET` 或读取对象内容。COS Archive 的设计重点就是减少这类操作：

- **热索引，冷数据**：`meta.json.{timestamp}` 索引文件固定上传为 `STANDARD`，保存目录、文件、权限、mtime、符号链接、chunk size/hash 和删除标记；大数据 chunk 按配置上传到 `DEEP_ARCHIVE`。
- **路径自描述 chunk**：chunk 对象名是 `size-sha1`，例如 `12345-...`。很多统计和校验可以直接从路径恢复 size/hash，不需要读取对象 metadata。
- **用 LIST 替代逐对象探测**：`fsck`、`verify`、`restore`、`download` 会扫描 chunk prefix 构建远端 chunk map，而不是对每个 chunk 做 `HEAD`。
- **常规浏览不取冷数据**：`browse` 和 `mount` 只读取本地索引；FUSE 挂载可用于 `ls`、`find`、`stat`、`readlink`，不会拉取归档文件内容。
- **延迟删除适配最低计费期**：删除候选先进入 `DeletedChunks`，默认 7 天宽限；实际删除时会参考对象 `Last-Modified`，避免早于 180 天最低计费期删除。日志会输出删除数量和释放空间。
- **恢复显式分阶段**：先 `restore` 批量发起归档解冻，再 `download` 下载已解冻 chunk，最后 `link` 在本地重建目录树。

当前仍有少量必要的对象 metadata 操作：上传前会 `HEAD` 判断 chunk 是否已存在；过期删除时会 `HEAD` 读取 `Last-Modified` 做最低计费期保护；远端索引会用 `x-cos-meta-hash` 判断本地缓存是否可复用。这些操作集中在索引或少量候选对象上，不会在普通浏览和本地索引操作中读取归档对象内容。

## 功能

- 增量备份：未变化文件复用远端索引中的 hash，只上传新增 chunk。
- 内容去重：相同 size/hash 的文件只保留一个 chunk。
- 归档恢复：支持批量发起 COS restore 请求，并按 QPS 限流。
- 多版本索引：索引命名为 `meta.json.{timestamp}`，默认保留最近 30 个版本。
- 延迟删除：远端失去引用的 chunk 先标记，之后再清理。
- 本地浏览：通过 Web 页面或只读 FUSE 挂载查看索引内容。
- 链接恢复：下载 chunk 后用 hardlink 重建文件树，保留权限和 mtime。
- 一致性检查：对比远端 LIST 结果和索引，清理丢失或孤立 chunk 记录。

## 构建

需要 Go 1.25 或更新版本。

```bash
go build -o cos-archive
```

## 配置

先复制示例配置：

```bash
cp config.example.json config.json
```

示例：

```json
{
    "BasePath": "/",
    "FilePaths": [
        "/backup/1",
        "/backup/2"
    ],
    "WorkingDir": "/var/lib/cos-archive/work",
    "Threads": 4,
    "RestoreQPS": 90,
    "Port": "3389",
    "COS": {
        "URL": "https://example-1250000000.cos.ap-region.myqcloud.com",
        "ID": "your-cos-id",
        "Key": "your-cos-key",
        "Prefix": "data/",
        "Class": "DEEP_ARCHIVE",
        "Retries": 1
    }
}
```

参数说明：

| 参数 | 说明 |
| --- | --- |
| `BasePath` | 扫描备份路径时使用的根目录。 |
| `FilePaths` | 需要备份的路径列表，会与 `BasePath` 拼接。 |
| `WorkingDir` | 工作目录，用于锁文件和远端索引缓存。 |
| `Threads` | 上传并发数，默认 `4`。 |
| `RestoreQPS` | 发起解冻请求的限速，默认 `90`。 |
| `Port` | `browse` Web 服务端口。 |
| `COS.URL` | COS bucket URL。 |
| `COS.ID` | COS SecretId。 |
| `COS.Key` | COS SecretKey。 |
| `COS.Prefix` | chunk 对象前缀，默认 `data/`。 |
| `COS.Class` | 上传 chunk 的存储类型，默认 `DEEP_ARCHIVE`。 |
| `COS.Retries` | 上传遇到 `ServiceUnavailable` 时的重试次数。 |

`config.json` 已在 `.gitignore` 中忽略，不要提交真实 bucket 和密钥。

## 使用

通用格式：

```bash
./cos-archive -action=<action> -config=config.json [-target=<dir>] [-mountpoint=<dir>] [-verbose]
```

动作：

| action | 作用 |
| --- | --- |
| `backup` | 默认动作。扫描本地文件，上传新增 chunk，写入新索引。 |
| `restore` | 对远端 chunk 发起 COS 归档解冻请求。 |
| `download` | 下载远端索引和 chunk 到本地 `target`。 |
| `link` | 基于已下载 chunk 在 `target/restore` 重建文件树。 |
| `browse` | 读取本地索引并启动 Web 浏览。 |
| `mount` | 将本地索引挂载成只读 FUSE 文件系统。 |
| `fsck` | 对比远端 chunk 列表和索引，修复索引中的丢失/孤立记录。 |
| `verify` | 对本地文件、远端索引和远端 chunk 列表做一致性验证。 |

常用命令：

```bash
# 增量备份
./cos-archive -action=backup -config=config.json

# 发起归档解冻
./cos-archive -action=restore -config=config.json

# 下载解冻后的数据到本地缓存
./cos-archive -action=download -config=config.json -target=/path/to/cache

# 重建文件树
./cos-archive -action=link -config=config.json -target=/path/to/cache

# 浏览本地缓存中的索引
./cos-archive -action=browse -config=config.json -target=/path/to/cache

# 挂载索引视图
./cos-archive -action=mount -config=config.json -target=/path/to/cache -mountpoint=/path/to/mount
```

## 恢复流程

归档对象不能立即下载，完整恢复通常分四步：

1. 运行 `restore` 发起解冻请求。
2. 等待 COS 完成解冻。
3. 运行 `download -target=/path/to/cache` 下载索引和 chunk。
4. 运行 `link -target=/path/to/cache` 在 `target/restore` 重建文件树。

`download` 会把 chunk 放到 `target/chunks/<suffix>/<size-hash>`，`link` 会用 hardlink 复用这些 chunk，避免额外复制数据。

## 浏览和挂载

`browse` 和 `mount` 都只依赖本地索引，不读取归档对象内容：

- 目录、文件大小、权限、mtime 和符号链接目标来自索引。
- 常规文件在 FUSE 中是占位视图，适合查看元数据，不用于直接读取原始内容。
- 如需真实文件内容，请走 `restore`、`download`、`link` 恢复流程。

## 存储模型

索引结构：

- `Entries.Files`：`path -> {Size, Mode, ModTime, Hash}`
- `Entries.Dirs`：`path -> {Mode}`
- `Entries.Links`：`path -> {LinkTo}`
- `DeletedChunks`：`ChunkKey -> delete_after_unix`

chunk key：

```text
<size>-<sha1>
```

hash 计算：

- 小文件小于 `0xF000` 字节时计算全文件 SHA1。
- 大文件计算开头 `0x5000` 字节、1/3 位置 `0x5000` 字节和末尾 `0x5000` 字节的 SHA1。

这是偏性能的抽样 hash，用于个人归档去重和定位，不等价于完整内容校验。

## 开发

```bash
go test ./...
```

项目约定：

- Go 代码使用 `gofmt`。
- 业务逻辑返回 error，由 `main.go` 统一处理退出。
- 并发上传使用 `errgroup`。
- 远端对象操作集中在 `cos.go`，索引读写集中在 `index.go`。

## 注意事项

- 默认存储类型是 `DEEP_ARCHIVE`，恢复前必须先解冻。
- 单实例锁会阻止多个非浏览/挂载动作并发运行。
- 索引保存在标准存储中；chunk 按配置进入归档存储。
- 如果曾经提交过真实 `config.json`，公开仓库前应清理 git history 并轮换密钥。
