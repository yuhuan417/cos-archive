package main

import (
	"log"
	"os"
	"path"
)

func downloadChunk(config Config, cm ChunksMap) {
	log.Println("Downloading chunks")
	// Download chunk from map
	c := NewCOS(config.COS)
	for key := range cm {
		k := chunkPath(key)
		cp := path.Join(config.TargetDir, "chunks", k[len(k)-2:])
		err := os.MkdirAll(cp, 0755)
		if err != nil {
			log.Println("Can't create chunks directory: ", cp)
		}
		p := path.Join(config.COS.ChunkPrefix, k)
		lp := path.Join(cp, k)
		for {
			log.Println("Downloading:", p)
			if fi, err := os.Stat(lp); err == nil {
				size := fi.Size()
				h := chunkHash(lp, size)
				ck := ChunkKey{size, h}

				if ck == key {
					log.Println("File exists. Hash matches. Good, skip.")
					break
				}
			}
			err = c.DownloadFile(p, lp)
			if err != nil {
				log.Println("Download file error:", p, err)
			} else {
				break
			}
		}
	}
}

func downloadFiles(config Config) {
	if config.TargetDir == "" {
		log.Fatalln("Empty target dir.")
	}
	riPath := path.Join(config.TargetDir, config.Index+".remote")
	if !downloadRemoteIndex(config, riPath) {
		log.Fatalln("Can't download index")
	}
	cm := scanRemoteChunksMap(config)
	downloadChunk(config, cm)
}
