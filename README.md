# 增量归档备份

## 设计目标

- 面向 Linux 文件系统
- 支持增量备份
- 本地任务随时中断不影响本地数据和云端数据最终一致性
- 本地数据完全丢失不影响云端数据一致性
- 支持云端自动延迟删除
- 在备份时不需要从云端获取数据
- 在时间范围内支持云端多版本保存及恢复功能(Time machine-like)
- 支持压缩存储
- 支持分块上传
- 支持多种归档备份后端（aws glacier, Tencent cloud cas）
- 支持加密备份(optional)
- 保持符号链接
- 保持文件权限
- 保持文件时间
- 保持文件属主、组
- 支持以上配置文件的修改，已有备份不需要重传
- 文件内容与文件属性分离，重复文件只上传一次

## 接口分析

为了支持多种归档备份后端，需要对归档后端接口进行分析，提取最小可用接口并进行抽象。

### AWS

- 假设所有数据存于用户指定的 Vault
- Upload 接口可以上传一个文件（支持 multiple-part upload），并且返回 archive ID
- Upload 需要计算 tree-sha256
- Download和Delete都以archive ID进行操作

### Tencent cloud cas

- 相当类似于AWS，不再赘述

## 存储设计

### Archive

| name             | type   | Description      |
| ---------------- | ------ | ---------------- |
| hash             | string | PK sha256+size   |
| archive_id       | string |                  |
| upload_time      | time   | 上传完成的时间，为0表示尚未上传 |
| delete_mark_time | time   | 删除标记的时间          |

### FileInfo

| name        | type    | Description          |
| ----------- | ------- | -------------------- |
| path        | string  | PK 文件路径              |
| symbol_link | string  | 符号链接目标路径，为""表示不是符号链接 |
| permission  | uint32  | 文件权限                 |
| size        | int64   | 文件大小                 |
| ctime       | int64   | ctime                |
| mtime       | int64   | mtime                |
| atime       | int64   | atime                |
| uid         | int     | uid                  |
| gid         | int     | gid                  |
| hash        | string  | sha256+size，为空表示尚未计算 |
| directory   | boolean | 是否是目录                |



## 逻辑设计

对于目标目录进行遍历，对于每一个文件条目：

- 若文件条目不存在，添加条目，并且计算 hash，写入 Archive 表等待上传
- 若文件条目已存在，若文件大小和修改时间有其一不一致，计算hash，写入 Archive 表等待上传，更新条目信息

保存最近 N 个版本的 FileInfo 表，成功完成后扫描 Archive 表和所有 FileInfo 表，对于不出现于任何 FileInfo 表中的条目，表示应该删除，在 Archive 表中设置删除标记时间

成功完成后扫描 Archive 表

- 对于未上传的文件进行上传，并设置 archive_id 值
- 对于 delete_mark_time 大于 upload_time + 存储最短计费时长的，进行物理删除，并删除表项
- 对于不出现于任何 FileInfo 表中的条目，表示应该删除，在 Archive 表中设置删除标记时间

完成后将 Archive 表和多版本 FileInfo 表打包上传存储。