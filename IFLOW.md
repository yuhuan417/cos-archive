# COS-Archive 项目文档

## 项目概述

COS-Archive 是一个基于 Go 语言开发的增量归档备份工具，专为 Linux 文件系统设计。该项目使用腾讯云对象存储（COS）作为后端存储，支持增量备份、文件去重、分块上传和版本管理功能。

### 主要特性

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

## 核心逻辑

### 备份流程

1. 检查云端索引哈希，如不一致则下载云端索引
2. 扫描本地目录，生成本地索引信息
3. 比较本地和云端索引，生成变更列表
4. 上传新文件内容（去重处理）
5. 上传新的索引文件
6. 更新云端主索引

### 一致性检查

1. 处理已过删除时间的文件
2. 扫描云端所有文件信息
3. 比较索引和实际文件，发现不一致
4. 生成修复操作列表
5. 更新索引信息

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