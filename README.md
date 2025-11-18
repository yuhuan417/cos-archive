# COS-Archive 增量归档备份工具

COS-Archive 是一个基于 Go 语言开发的增量归档备份工具，专为 Linux 文件系统设计。该项目使用腾讯云对象存储（COS）作为后端存储，支持增量备份、文件去重、分块上传和版本管理功能。

## 主要特性

- **增量备份**：只备份变更的文件，节省存储空间和传输时间
- **文件去重**：基于内容哈希的去重机制，相同文件只上传一次
- **分块上传**：支持大文件自动分块上传
- **版本管理**：类似 Time Machine 的多版本保存及恢复功能
- **云端延迟删除**：支持云端自动延迟删除机制
- **元数据保护**：保持文件权限、时间戳、属主等元信息
- **符号链接支持**：保持符号链接结构
- **中断恢复**：本地任务随时中断不影响数据最终一致性

## 技术栈

- **语言**：Go 1.14
- **主要依赖**：
  - `github.com/tencentyun/cos-go-sdk-v5`：腾讯云 COS SDK
  - `github.com/dustin/go-humanize`：文件大小人性化显示
  - `go.uber.org/ratelimit`：速率限制
  - `github.com/allan-simon/go-singleinstance`：单实例锁

## 项目结构

```
cos-archive/
├── main.go          # 程序入口点
├── config.go        # 配置结构定义
├── backup.go        # 备份逻辑实现
├── restore.go       # 恢复逻辑实现
├── download.go      # 下载逻辑实现
├── browse.go        # 浏览功能实现
├── link.go          # 链接处理逻辑
├── fsck.go          # 一致性检查
├── verify.go        # 验证功能
├── index.go         # 索引管理
├── chunk.go         # 分块处理
├── cos.go           # COS 接口封装
├── config.json      # 配置文件示例
└── go.mod           # Go 模块定义
```

## 构建和运行

### 构建项目

```bash
go build -o cos-archive
```

### 运行项目

```bash
./cos-archive -action=<操作类型> -config=<配置文件路径>
```

### 支持的操作类型

- `backup`：执行增量备份（默认操作）
- `restore`：恢复文件
- `download`：下载文件
- `browse`：浏览云端文件
- `link`：处理符号链接
- `fsck`：执行一致性检查
- `verify`：验证文件完整性

## 配置说明

配置文件使用 JSON 格式，示例 `config.json`：

```json
{
    "FilePaths": [
        "/backup/1",
        "/backup/2"
    ],
    "Threads": 4,
    "Port": "3389",
    "COS": {
        "URL": "https://example-1250000000.cos.ap-region.myqcloud.com",
        "ID": "your-cos-id",
        "Key": "your-cos-key",
        "Prefix": "data/"
    }
}
```

### 配置参数说明

- `FilePaths`：需要备份的文件路径列表
- `Threads`：上传线程数（默认 4）
- `Port`：服务端口（用于浏览功能）
- `COS.URL`：腾讯云 COS 访问地址
- `COS.ID`：腾讯云访问密钥 ID
- `COS.Key`：腾讯云访问密钥
- `COS.Prefix`：云端存储前缀（默认 "data/"）

## 存储设计

### ChunkKey 数据结构

| 字段名 | 类型   | 说明      |
| ------ | ------ | ---------------- |
| size   | int64  | 文件大小 |
| hash   | HashType | SHA1哈希值（20字节） |

### FileInfo 数据结构

| 字段名   | 类型       | 说明          |
| ------- | --------- | -------------------- |
| Size    | int64     | 文件大小                 |
| Mode    | os.FileMode | 文件权限                 |
| ModTime | int64     | 文件修改时间（Unix时间戳）                |
| Hash    | HashType  | 文件哈希值，SHA1算法 |

### DirInfo 数据结构

| 字段名 | 类型          | 说明          |
| ----- | ------------ | -------------------- |
| Mode  | os.FileMode  | 目录权限                 |

### LinkInfo 数据结构

| 字段名   | 类型   | 说明          |
| ----- | ----- | -------------------- |
| LinkTo | string | 符号链接目标路径 |

### Index 数据结构

| 字段名           | 类型                | 说明          |
| --------------- | ------------------ | -------------------- |
| Entries         | DirEnt             | 目录条目集合 |
| DeletedChunks   | ChunkDeleteMarkMap | 已删除块标记映射 |
| config          | Config             | 配置信息 |

### DirEnt 数据结构

| 字段名  | 类型         | 说明          |
| ------ | ----------- | -------------------- |
| Files  | FileInfoMap | 文件信息映射 |
| Dirs   | DirInfoMap  | 目录信息映射 |
| Links  | LinkInfoMap | 链接信息映射 |



## 核心逻辑

### 备份流程 (backup.go)

1. 加载远程索引到 `remoteIndex`
2. 生成本地索引到 `localIndex`，复用远程索引中未变更文件的哈希值
3. 构建本地和远程的块映射表 `localChunkMap` 和 `remoteChunkMap`
4. 并发上传缺失的文件块到云端存储
5. 标记远程存在但本地不存在的块为待删除（7天后执行）
6. 上传新的索引文件到云端

### 一致性检查 (fsck.go)

1. 加载远程索引
2. 删除已过删除时间的文件块
3. 扫描云端所有实际文件块，构建云端块映射表
4. 比较索引和实际文件，发现不一致：
   - 索引中有但云端没有的文件：从索引中删除
   - 云端有但索引中没有的文件：标记为 lost_found（10年后删除）
5. 上传更新后的索引

### 哈希算法 (chunk.go)

使用迅雷哈希算法计算文件哈希：
- 小文件（<0xF000字节）：直接计算整个文件的SHA1
- 大文件：计算文件开头0x5000字节、1/3处0x5000字节和末尾0x5000字节的SHA1

### 索引管理 (index.go)

- 索引文件以 JSON 格式存储，使用 codec 库进行序列化
- 支持本地缓存和远程索引的哈希比较，避免不必要的下载
- 索引文件命名：`meta.json.{timestamp}`，保留最近30个版本
- 删除的文件块会延迟删除，默认延迟时间为存储最短计费时长（180天）

## 操作对比表

### 文件块上传操作

| 本地块映射中有 | 远程块映射中有 | 操作 |
| --- | --- | --- |
| 是 | 否 | 加入上传队列 |
| 是 | 是 | 跳过（已存在） |
| 否 | 是 | 标记为待删除（7天后） |

### 一致性校验操作

| 索引中是否有 | 云端实际是否有 | 操作 |
| --- | --- | --- |
| 是 | 是 | 无操作 |
| 是 | 否 | 从索引中删除该条目 |
| 否 | 是 | 标记为 lost_found（10年后删除） |

## 开发约定

- 使用 Go 标准格式化工具
- 错误处理使用 log.Fatal 输出严重错误
- 并发处理使用 sync.WaitGroup
- 文件路径使用 path.Join 进行跨平台兼容
- 配置项提供合理的默认值

## 注意事项

- 程序使用单实例锁，防止同时运行多个实例
- 云端存储默认使用 DEEP_ARCHIVE 存储类别
- 删除的文件会延迟 7 天后实际删除
- 首次运行会进行全量备份
- 索引文件存储在云端，不进行归档级别存储



