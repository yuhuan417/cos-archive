package main

import (
	"log"
	"path/filepath"
	"strings"
	"time"
)

// ChunksMap struct
type ChunksMap = map[string]bool

func deleteOutdatedChunks(config Config, index *Index) {
	now := time.Now().Unix()

	for fp, t := range index.Chunks {
		if now > t {
			// log.Println("Deleting remote chunk: ", fp)
			err := cosDeleteFile(config.COS, fp)
			if err != nil {
				continue
			}
			delete(index.Chunks, fp)
		}
	}
}

func buildChunksMap(index Index) ChunksMap {
	chunksMap := make(ChunksMap)
	for _, fi := range index.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}

		chunksMap[chunkHash(fi.Size, fi.Hash)] = false
	}
	return chunksMap
}

func scanRemoteChunksMap(config Config) ChunksMap {
	log.Println("Scan remote chunks")
	cm := make(ChunksMap)

	cosScanFiles(config.COS, config.COS.ChunkPrefix, func(key string) {
		p := filepath.Clean(config.COS.ChunkPrefix) + "/"
		cm[strings.TrimPrefix(key, p)] = false
	})
	return cm
}
