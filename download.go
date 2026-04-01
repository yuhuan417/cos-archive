package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"sync/atomic"
)

const maxDownloadRetries = 10

func downloadChunk(ctx context.Context, c *COS, config Config, cm ChunksMap) error {
	slog.Debug("Downloading chunks")
	var processed atomic.Int64
	var processedBytes atomic.Int64
	totalItems := int64(len(cm))
	totalBytes := int64(0)
	for key := range cm {
		totalBytes += key.size
	}
	stopProgress := startProgressLogger(ctx, "Download progress", totalItems, totalBytes, &processed, &processedBytes)
	defer stopProgress()

	for key := range cm {
		k := chunkPath(key)
		cp := path.Join(config.TargetDir, "chunks", k[len(k)-2:])
		if err := os.MkdirAll(cp, 0755); err != nil {
			return err
		}
		p := path.Join(config.COS.ChunkPrefix, k)
		lp := path.Join(cp, k)
		retries := 0
		for {
			if retries >= maxDownloadRetries {
				return fmt.Errorf("%w: %s", ErrMaxRetries, p)
			}
			slog.Debug("Downloading", "path", p)
			if fi, err := os.Stat(lp); err == nil {
				size := fi.Size()
				h, hashErr := chunkHash(lp, size)
				if hashErr == nil {
					ck := ChunkKey{size, h}
					if ck == key {
						slog.Debug("File exists. Hash matches. Good, skip.")
						processed.Add(1)
						processedBytes.Add(key.size)
						break
					}
				}
			}
			err := c.DownloadFile(ctx, p, lp)
			if err != nil {
				slog.Debug("Download file error", "path", p, "error", err)
				retries++
				continue
			}
			processed.Add(1)
			processedBytes.Add(key.size)
			break
		}
	}
	return nil
}

func downloadFiles(ctx context.Context, config Config) error {
	c, err := NewCOS(config.COS)
	if err != nil {
		return err
	}
	index := NewIndex(config, c)
	err = index.LoadRemote(ctx)
	if err != nil {
		if errors.Is(err, ErrIndexNotFound) {
			return fmt.Errorf("download index: %w", err)
		}
		return err
	}
	if err := index.Save(path.Join(config.TargetDir, config.Index+".remote")); err != nil {
		return fmt.Errorf("persist downloaded index: %w", err)
	}
	cm, err := scanRemoteChunksMap(ctx, c, config)
	if err != nil {
		return err
	}
	return downloadChunk(ctx, c, config, cm)
}
