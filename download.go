package main

import (
	"log/slog"
	"os"
	"path"
)

const maxDownloadRetries = 10

func downloadChunk(config Config, cm ChunksMap) {
	slog.Debug("Downloading chunks")
	// Download chunk from map
	c := NewCOS(config.COS)
	for key := range cm {
		k := chunkPath(key)
		cp := path.Join(config.TargetDir, "chunks", k[len(k)-2:])
		err := os.MkdirAll(cp, 0755)
		if err != nil {
			slog.Debug("Can't create chunks directory", "path", cp)
		}
		p := path.Join(config.COS.ChunkPrefix, k)
		lp := path.Join(cp, k)
		retries := 0
		for {
			if retries >= maxDownloadRetries {
				Fatal("Download exceeded max retries:", p)
			}
			slog.Debug("Downloading", "path", p)
			if fi, err := os.Stat(lp); err == nil {
				size := fi.Size()
				h, hashErr := chunkHash(lp, size)
				if hashErr == nil {
					ck := ChunkKey{size, h}
					if ck == key {
						slog.Debug("File exists. Hash matches. Good, skip.")
						break
					}
				}
			}
			err = c.DownloadFile(p, lp)
			if err != nil {
				slog.Debug("Download file error", "path", p, "error", err)
				retries++
			} else {
				break
			}
		}
	}
}

func downloadFiles(config Config) {
	if config.TargetDir == "" {
		Fatal("Empty target dir.")
	}
	index := NewIndex(config)
	err := index.LoadRemote()
	if err != nil {
		Fatal("Can't download index")
	}
	err = index.Save(path.Join(config.TargetDir, config.Index+".remote"))
	if err != nil {
		Fatal("Can't persist downloaded index:", err)
	}
	cm := scanRemoteChunksMap(config)
	downloadChunk(config, cm)
}
