package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path"
	"time"
)

func fsckRemote(config Config) {
	ri_path := path.Join(config.WorkingDir, config.COS.Index + ".remote")
	if !downloadRemoteIndex(config, ri_path) {
		return
	}
	ri := loadIndex(ri_path)

	deleteOutdatedChunks(config, &ri)

	cm := scanRemoteChunksMap(config)

	for fp, fi := range ri.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}
		ch := chunkHash(fi)
		_, ok := cm[ch]
		if ok {
			cm[ch] = true
		} else {
			delete(ri.Files, fp)
		}
	}

	for k, _ := range ri.Chunks {
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
	fmt.Println(string(j))
	uploadRemoteIndex(config, j)
}
