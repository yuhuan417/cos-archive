package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"slices"
	"time"

	"github.com/dustin/go-humanize"
	"golang.org/x/sync/errgroup"
)

const uploadAnalysisThreshold = 300 << 20 // 300 MiB

// UploadCTX struct
type UploadCTX struct {
	localPath  string
	remotePath string
	size       int64
}

func uploadPayload(ctx context.Context, c *COS, config Config, uploadCtx UploadCTX) error {
	rp := path.Join(config.COS.ChunkPrefix, uploadCtx.remotePath)

	header, err := c.GetHeader(ctx, rp)
	if err != nil {
		return err
	}
	if header != nil {
		slog.Debug("File already exists", "ctx", uploadCtx, "remotePath", rp)
		return nil
	}

	err = c.UploadFile(ctx, rp, uploadCtx.localPath, config.COS.Class, nil)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("Local file does not exist", "ctx", uploadCtx, "error", err)
			return nil
		}
		return fmt.Errorf("upload %s: %w", uploadCtx.localPath, err)
	}
	slog.Debug("Uploaded file", "localPath", uploadCtx.localPath)
	return nil
}

func uploadFiles(ctx context.Context, c *COS, config Config, localIndex *Index, remoteIndex *Index) error {
	deleteTime := time.Now().AddDate(0, 0, 7).Unix()
	remoteChunkMap := buildChunksMap(*remoteIndex)
	remoteIndex.Entries.Files = nil
	remoteIndex.Entries.Dirs = nil
	remoteIndex.Entries.Links = nil
	localChunkMap := buildChunksMap(*localIndex)

	localIndex.DeletedChunks = make(ChunkDeleteMarkMap)
	for k, v := range remoteIndex.DeletedChunks {
		if v > time.Now().AddDate(3, 0, 0).Unix() {
			v = deleteTime
		}
		localIndex.DeletedChunks[k] = v
	}

	cnt := 0
	uploadSize := int64(0)
	totalSize := int64(0)
	tasks := make([]UploadCTX, 0)

	for fp, fi := range localIndex.Entries.Files {
		h := ChunkKey{fi.Size, fi.Hash}
		if localChunkMap[h] {
			continue
		}

		totalSize += fi.Size
		localChunkMap[h] = true
		if _, ok := remoteChunkMap[h]; ok {
			remoteChunkMap[h] = true
		} else {
			uploadCtx := UploadCTX{
				localPath:  fp,
				remotePath: chunkPath(h),
				size:       fi.Size,
			}
			cnt++
			uploadSize += fi.Size
			tasks = append(tasks, uploadCtx)
		}
		if _, ok := localIndex.DeletedChunks[h]; ok {
			delete(localIndex.DeletedChunks, h)
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(config.Threads)
	for _, uploadCtx := range tasks {
		uploadCtx := uploadCtx
		g.Go(func() error {
			if err := uploadPayload(gctx, c, config, uploadCtx); err != nil {
				return err
			}
			return nil
		})
	}
	err := g.Wait()
	if err != nil {
		return err
	}

	slog.Info("Uploaded files", "count", cnt, "size", humanize.IBytes(uint64(uploadSize)))
	slog.Info("Total chunk size", "size", humanize.IBytes(uint64(totalSize)))

	for k, v := range remoteChunkMap {
		if !v {
			slog.Debug("Marking delete chunk", "path", chunkPath(k))
			localIndex.DeletedChunks[k] = deleteTime
		}
	}
	delErr := localIndex.DeleteOutdatedChunks(ctx)

	if uploadSize > uploadAnalysisThreshold {
		slices.SortFunc(tasks, func(a, b UploadCTX) int {
			if b.size > a.size {
				return 1
			}
			if b.size < a.size {
				return -1
			}
			return 0
		})

		typeSizeBuckets := [4]int64{0, 0, 0, 0} // 100MiB+, 10-100MiB, 1-10MiB, 0-1MiB
		typeCntBuckets := [4]int{0, 0, 0, 0}
		for _, t := range tasks {
			switch {
			case t.size >= 100<<20:
				typeSizeBuckets[0] += t.size
				typeCntBuckets[0]++
			case t.size >= 10<<20:
				typeSizeBuckets[1] += t.size
				typeCntBuckets[1]++
			case t.size >= 1<<20:
				typeSizeBuckets[2] += t.size
				typeCntBuckets[2]++
			default:
				typeSizeBuckets[3] += t.size
				typeCntBuckets[3]++
			}
		}

		slog.Info("Upload size analysis",
			"100MiB+", fmt.Sprintf("%d files (%s)", typeCntBuckets[0], humanize.IBytes(uint64(typeSizeBuckets[0]))),
			"10-100MiB", fmt.Sprintf("%d files (%s)", typeCntBuckets[1], humanize.IBytes(uint64(typeSizeBuckets[1]))),
			"1-10MiB", fmt.Sprintf("%d files (%s)", typeCntBuckets[2], humanize.IBytes(uint64(typeSizeBuckets[2]))),
			"0-1MiB", fmt.Sprintf("%d files (%s)", typeCntBuckets[3], humanize.IBytes(uint64(typeSizeBuckets[3]))),
		)

		topN := min(10, len(tasks))
		topFiles := make([]string, topN)
		for i := range topN {
			topFiles[i] = fmt.Sprintf("%s (%s)", tasks[i].localPath, humanize.IBytes(uint64(tasks[i].size)))
		}
		slog.Info("Upload top files", "files", topFiles)
	}

	return delErr
}

func backupFiles(ctx context.Context, config Config) error {
	c, err := NewCOS(config.COS)
	if err != nil {
		return err
	}
	remoteIndex := NewIndex(config, c)
	err = remoteIndex.LoadRemote(ctx)
	if err != nil && !errors.Is(err, ErrIndexNotFound) {
		return fmt.Errorf("load remote index: %w", err)
	}
	if errors.Is(err, ErrIndexNotFound) {
		slog.Info("Remote index not found, starting fresh backup")
	}

	localIndex := NewIndex(config, c)
	localIndex.GenerateLocal(remoteIndex)

	if err := uploadFiles(ctx, c, config, localIndex, remoteIndex); err != nil {
		return err
	}
	return localIndex.UploadRemote(ctx)
}
