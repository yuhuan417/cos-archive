package main

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
)

func classifyRestoreError(err error) error {
	if err == nil {
		return nil
	}
	if cos.IsNotFoundError(err) {
		slog.Debug("Restore target does not exist")
		return nil
	}
	if e, ok := cos.IsCOSError(err); ok {
		if e.Code == "RestoreAlreadyInProgress" {
			return ErrRestorePending
		}
		slog.Error("Restore error", "code", e.Code, "message", e.Message, "resource", e.Resource, "requestId", e.RequestID)
		return err
	}
	slog.Error("Restore error", "error", err)
	return err
}

func restoreChunk(ctx context.Context, c *COS, config Config, cm ChunksMap) error {
	slog.Debug("Restoring chunks")
	qps := config.RestoreQPS
	if qps <= 0 {
		qps = 90
	}
	// Restores run one at a time, so pacing is just a tick: the loop
	// releases a request every 1/qps and waits on the context in between.
	interval := time.Second / time.Duration(qps)
	if interval <= 0 {
		interval = time.Nanosecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var errs error
	for k := range cm {
		select {
		case <-ctx.Done():
			return errors.Join(errs, ctx.Err())
		case <-ticker.C:
		}
		p := path.Join(config.COS.ChunkPrefix, chunkPath(k))
		err := classifyRestoreError(c.RestoreFile(ctx, p))
		if errors.Is(err, ErrRestorePending) {
			slog.Debug("Restore already in progress", "path", p)
			continue
		}
		if err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

func restoreFiles(ctx context.Context, config Config) error {
	c, err := NewCOS(config.COS)
	if err != nil {
		return err
	}
	cm, err := scanRemoteChunksMap(ctx, c, config)
	if err != nil {
		return err
	}
	return restoreChunk(ctx, c, config, cm)
}
