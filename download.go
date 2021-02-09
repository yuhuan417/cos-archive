package main

import (
	"log"
	"path"
)

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
