package main

import (
	"log/slog"
	"path"

	"github.com/tencentyun/cos-go-sdk-v5"
	"go.uber.org/ratelimit"
)

func logRestoreStatus(err error) {
	if err == nil {
		return
	}
	if cos.IsNotFoundError(err) {
		// WARN
		slog.Debug("WARN: Resource does not exist")
	} else if e, ok := cos.IsCOSError(err); ok {
		if e.Code == "RestoreAlreadyInProgress" {
			return
		}
		slog.Info("ERROR", "code", e.Code)
		slog.Info("ERROR", "message", e.Message)
		slog.Info("ERROR", "resource", e.Resource)
		slog.Info("ERROR", "requestId", e.RequestID)
		// ERROR
	} else {
		slog.Info("ERROR", "error", err)
		// ERROR
	}
}

func restoreChunk(config Config, cm ChunksMap) {
	slog.Debug("Restoring chunks")
	// Download chunk from map
	rl := ratelimit.New(90) // per second, hardcode.
	c := NewCOS(config.COS)

	for k := range cm {
		rl.Take()
		p := path.Join(config.COS.ChunkPrefix, chunkPath(k))
		err := c.RestoreFile(p)
		logRestoreStatus(err)
	}
}

func restoreFiles(config Config) {
	cm := scanRemoteChunksMap(config)
	restoreChunk(config, cm)
}
