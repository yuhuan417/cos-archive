package main

import (
	"encoding/json"
	"log"
	"time"
)

func fsckRemote(config Config) {
	ri := getRemoteIndex(config)

	deleteOutdatedChunks(config, &ri)

	cm := scanRemoteChunksMap(config)

	for fp, fi := range ri.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}
		ch := ChunkKey{fi.Size, fi.Hash}
		_, ok := cm[ch]
		if ok {
			cm[ch] = true
		} else {
			delete(ri.Files, fp)
		}
	}

	for k := range ri.DeletedChunks {
		_, ok := cm[k]
		if ok {
			cm[k] = true
		} else {
			delete(ri.DeletedChunks, k)
		}
	}

	forever := time.Now().AddDate(10, 0, 0).Unix()
	for k, v := range cm {
		if !v {
			log.Println("Lost found chunk", k)
			ri.DeletedChunks[k] = forever
		}
	}

	j, _ := json.MarshalIndent(ri, "", "  ")
	uploadRemoteIndex(config, j)
}
