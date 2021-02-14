package main

import (
	"log"
	"path"

	"go.uber.org/ratelimit"
)

func restoreChunk(config Config, cm ChunksMap) {
	log.Println("Restoring chunks")
	// Download chunk from map
	rl := ratelimit.New(90) // per second, hardcode.

	for k := range cm {
		rl.Take()
		p := path.Join(config.COS.ChunkPrefix, k)
		err := cosRestoreFile(config.COS, p)
		if err != nil {
			log.Println("Restore file error:", p, err)
		}
	}
}

func restoreFiles(config Config) {
	cm := scanRemoteChunksMap(config)
	restoreChunk(config, cm)
}
