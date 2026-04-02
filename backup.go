package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"time"

	"github.com/dustin/go-humanize"
	"golang.org/x/sync/errgroup"
)

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
	return localIndex.DeleteOutdatedChunks(ctx)
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
