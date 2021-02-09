package main

import (
	"log"
	"path"
)

func restoreFiles(config Config) {
	riPath := path.Join(config.WorkingDir, config.Index+".remote")
	if !downloadRemoteIndex(config, riPath) {
		log.Fatalln("Can't download remote index")
		return
	}
	ri := loadIndex(riPath)
	remoteChunkMap := buildChunksMap(ri)
	restoreChunk(config, remoteChunkMap)
}
