# COS Archive

[中文](README.md)

COS Archive is a file backup tool designed for Tencent Cloud COS archive storage. It keeps frequently accessed metadata separate from cold data blocks: indexes are always stored in the STANDARD class, while large data chunks can be uploaded to low-cost archive classes such as `DEEP_ARCHIVE`. Routine backup, browsing, verification, and cleanup rely on the index, bucket listing, and self-describing object paths as much as possible, so cold objects do not need to be downloaded or restored for ordinary maintenance.

It is intended for long-lived cold data such as photos, personal archives, and historical backups: writes are infrequent, restores can tolerate an explicit restore step, and day-to-day maintenance should avoid touching archive objects.

## Archive Storage Optimizations

Archive storage is cheap, but frequent `HEAD`, `GET`, or object-content reads are a bad fit. COS Archive is built to minimize those operations:

- **Hot index, cold data**: `meta.json.{timestamp}` index files are always uploaded as `STANDARD`. They contain directory entries, file metadata, symlinks, chunk size/hash, and deletion marks. Large chunks are uploaded with the configured storage class, usually `DEEP_ARCHIVE`.
- **Self-describing chunk paths**: chunk object names are `size-sha1`, for example `12345-...`. Many checks and statistics can recover size/hash from the path without reading object metadata.
- **LIST instead of per-object probes**: `fsck`, `verify`, `restore`, and `download` scan the chunk prefix and build a remote chunk map, instead of issuing `HEAD` for every chunk.
- **No cold data access for browsing**: `browse` and `mount` read only the local index. The FUSE mount is useful for `ls`, `find`, `stat`, and `readlink`, and does not fetch archived file contents.
- **Delayed deletion for minimum storage duration**: unreferenced chunks are first marked in `DeletedChunks` with a 7-day grace period. Actual deletion also checks the object `Last-Modified` timestamp to avoid deleting before the 180-day minimum billing period. Cleanup logs include both object count and freed size.
- **Explicit staged restore**: run `restore` to issue archive restore requests, `download` after COS finishes restoring, and `link` to rebuild the local file tree.

There are still a few deliberate metadata operations: upload checks `HEAD` before writing a chunk, outdated deletion reads `Last-Modified` for minimum-duration protection, and remote indexes use `x-cos-meta-hash` to decide whether a local cached index can be reused. These operations are narrow and do not happen during ordinary local browsing or index-only operations.

## Features

- Incremental backup: unchanged files reuse hashes from the remote index; only new chunks are uploaded.
- Content deduplication: files with the same size/hash share one chunk.
- Archive restore: batch COS restore requests with QPS limiting.
- Versioned indexes: indexes are named `meta.json.{timestamp}` and the latest 30 versions are retained by default.
- Delayed deletion: unreferenced remote chunks are marked before actual deletion.
- Local browsing: inspect an index through a web UI or a read-only FUSE mount.
- Linked restore: after downloading chunks, rebuild the file tree with hard links while preserving mode and mtime.
- Consistency check: compare remote LIST results with the index and repair missing or orphaned chunk records.

## Build

Go 1.27 or newer is required.

```bash
go build -o cos-archive
```

## Configuration

Start from the example:

```bash
cp config.example.json config.json
```

Example:

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

Parameters:

| Parameter | Description |
| --- | --- |
| `BasePath` | Root path used when scanning backup paths. |
| `FilePaths` | Paths to back up. They are joined with `BasePath`. |
| `WorkingDir` | Working directory for lock files and remote index cache. |
| `Threads` | Upload concurrency. Default: `4`. |
| `RestoreQPS` | Rate limit for archive restore requests. Default: `90`. |
| `Port` | Web server port for `browse`. |
| `COS.URL` | COS bucket URL. |
| `COS.ID` | COS SecretId. |
| `COS.Key` | COS SecretKey. |
| `COS.Prefix` | Chunk object prefix. Default: `data/`. |
| `COS.Class` | Storage class for uploaded chunks. Default: `DEEP_ARCHIVE`. |
| `COS.Retries` | Upload retries for `ServiceUnavailable`. |

`config.json` is ignored by `.gitignore`. Do not commit real bucket endpoints or credentials.

## Usage

General form:

```bash
./cos-archive -action=<action> -config=config.json [-target=<dir>] [-mountpoint=<dir>] [-verbose]
```

Actions:

| action | Description |
| --- | --- |
| `backup` | Default action. Scan local files, upload new chunks, and write a new index. |
| `restore` | Issue COS archive restore requests for remote chunks. |
| `download` | Download the remote index and chunks into a local target directory. |
| `link` | Rebuild the file tree in `target/restore` from downloaded chunks. |
| `browse` | Read a local index and start a web browser view. |
| `mount` | Mount a local index as a read-only FUSE filesystem. |
| `fsck` | Compare remote chunk listing with the index and repair missing/orphaned records. |
| `verify` | Verify local files, remote index, and remote chunk listing. |

Common commands:

```bash
# Incremental backup
./cos-archive -action=backup -config=config.json

# Request archive restore
./cos-archive -action=restore -config=config.json

# Download restored data to a local cache
./cos-archive -action=download -config=config.json -target=/path/to/cache

# Rebuild the file tree
./cos-archive -action=link -config=config.json -target=/path/to/cache

# Browse the cached index
./cos-archive -action=browse -config=config.json -target=/path/to/cache

# Mount the index view
./cos-archive -action=mount -config=config.json -target=/path/to/cache -mountpoint=/path/to/mount
```

## Restore Flow

Archive objects cannot be downloaded immediately. A full restore usually has four steps:

1. Run `restore` to issue restore requests.
2. Wait for COS to finish restoring the objects.
3. Run `download -target=/path/to/cache` to download the index and chunks.
4. Run `link -target=/path/to/cache` to rebuild the file tree under `target/restore`.

`download` stores chunks under `target/chunks/<suffix>/<size-hash>`. `link` hard-links those chunks into the restored tree to avoid copying data again.

## Browse And Mount

`browse` and `mount` rely only on the local index and do not read archive object contents:

- Directory entries, file sizes, modes, mtime, and symlink targets come from the index.
- Regular files in the FUSE mount are placeholder views for metadata inspection, not direct access to original contents.
- To read real file contents, use the `restore`, `download`, and `link` flow.

## Storage Model

Index structure:

- `Entries.Files`: `path -> {Size, Mode, ModTime, Hash}`
- `Entries.Dirs`: `path -> {Mode}`
- `Entries.Links`: `path -> {LinkTo}`
- `DeletedChunks`: `ChunkKey -> delete_after_unix`

Chunk key:

```text
<size>-<sha1>
```

Hashing:

- Small files below `0xF000` bytes use full-file SHA1.
- Large files hash three samples: first `0x5000` bytes, `0x5000` bytes around one-third of the file, and last `0x5000` bytes.

This is a performance-oriented sampled hash for personal archive deduplication and location. It is not a full-content verification hash.

## Development

```bash
go test ./...
```

Project conventions:

- Format Go code with `gofmt`.
- Return errors from business logic and let `main.go` handle program exit.
- Use `errgroup` for concurrent uploads.
- Keep remote object operations in `cos.go` and index persistence in `index.go`.

## Notes

- The default storage class is `DEEP_ARCHIVE`; restore before downloading.
- A single-instance lock prevents multiple non-browse/non-mount actions from running concurrently.
- Indexes are stored in STANDARD; chunks use the configured archive class.
- If a real `config.json` was ever committed, clean git history and rotate credentials before publishing.
