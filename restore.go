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
		slog.Info("WARN: Resource is not existed")
	} else if e, ok := cos.IsCOSError(err); ok {
		if e.Code == "RestoreAlreadyInProgress" {
			return
		}
		slog.Info("ERROR: Code: %v\n", e.Code)
		slog.Info("ERROR: Message: %v\n", e.Message)
		slog.Info("ERROR: Resource: %v\n", e.Resource)
		slog.Info("ERROR: RequestId: %v\n", e.RequestID)
		// ERROR
	} else {
		slog.Info("ERROR: %v\n", err)
		// ERROR
	}
}

func restoreChunk(config Config, cm ChunksMap) {
	slog.Info("Restoring chunks")
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
