package main

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

func fsckRemote(ctx context.Context, config Config) error {
	c, err := NewCOS(config.COS)
	if err != nil {
		return err
	}
	ri := NewIndex(config, c)
	if err := ri.LoadRemote(ctx); err != nil {
		return err
	}

	if err := ri.DeleteOutdatedChunks(ctx); err != nil {
		slog.Debug("Delete outdated chunk errors", "error", err)
	}

	cm, err := scanRemoteChunksMap(ctx, c, config)
	if err != nil {
		return err
	}

	var errs error
	for fp, fi := range ri.Entries.Files {
		ch := ChunkKey{fi.Size, fi.Hash}
		if _, ok := cm[ch]; ok {
			cm[ch] = true
		} else {
			delete(ri.Entries.Files, fp)
			slog.Debug("Chunk lost", "fp", fp, "path", chunkPath(ch))
			errs = errors.Join(errs, ErrChunkLost)
		}
	}
	now := time.Now().Unix()
	for k, t := range ri.DeletedChunks {
		if _, ok := cm[k]; ok {
			cm[k] = true
		} else {
			delete(ri.DeletedChunks, k)
			if t < now {
				slog.Debug("Deleted chunk lost", "path", chunkPath(k))
			}
		}
	}

	forever := time.Now().AddDate(10, 0, 0).Unix()
	for k, v := range cm {
		if !v {
			slog.Debug("Lost found chunk", "chunk", k)
			ri.DeletedChunks[k] = forever
		}
	}

	if err := ri.UploadRemote(ctx); err != nil {
		return err
	}
	return errs
}
