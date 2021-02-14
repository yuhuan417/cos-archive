package main

import (
	"encoding/json"
	"log"
	"path"
	"time"
)

func fsckRemote(config Config) {
	riPath := path.Join(config.WorkingDir, config.Index+".remote")
	if !downloadRemoteIndex(config, riPath) {
		log.Fatalln("Can't download remote index")
		return
	}
	ri := loadIndex(riPath)

	deleteOutdatedChunks(config, &ri)

	cm := scanRemoteChunksMap(config)

	for fp, fi := range ri.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}
		ch := chunkPath(fi.Size, fi.Hash)
		_, ok := cm[ch]
		if ok {
			cm[ch] = true
		} else {
			delete(ri.Files, fp)
		}
	}

	for k := range ri.Chunks {
		_, ok := cm[k]
		if ok {
			cm[k] = true
		} else {
			delete(ri.Chunks, k)
		}
	}

	forever := time.Now().AddDate(10, 0, 0).Unix()
	for k, v := range cm {
		if !v {
			log.Println("Lost found chunk", k)
			ri.Chunks[k] = forever
		}
	}

	j, _ := json.MarshalIndent(ri, "", "  ")
	uploadRemoteIndex(config, j)
}
