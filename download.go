package main

import (
	"log"
	"os"
	"path"
)

func downloadChunk(config Config, cm ChunksMap) {
	log.Println("Downloading chunks")
	// Download chunk from map

	for k := range cm {
		cp := path.Join(config.TargetDir, "chunks")
		err := os.MkdirAll(cp, 0755)
		if err != nil {
			log.Println("Can't create chunks directory: ", cp)
		}
		p := path.Join(config.COS.ChunkPrefix, k)
		lp := path.Join(cp, k[len(k)-2:], k)
		for {
			log.Println("Downloading:", p)
			if fi, err := os.Stat(lp); err == nil {
				h := chunkHash(lp, fi.Size())
				if chunkPath(fi.Size(), h) == k {
					log.Println("File exists. Hash matches. Good, skip.")
					continue
				}
			}
			err = cosDownloadFile(config.COS, p, lp)
			if err != nil {
				log.Println("Download file error:", p, err)
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
