package main

import (
	"log"
	"time"
)

func fsckRemote(config Config) {
	ri := NewIndex(config)
	err := ri.LoadRemote()

	if err != nil {
		log.Fatalln("Can't load remote index: ", err)
	}

	ri.DeleteOutdatedChunks()

	cm := scanRemoteChunksMap(config)

	for fp, fi := range ri.Entries.Files {
		ch := ChunkKey{fi.Size, fi.Hash}
		_, ok := cm[ch]
		if ok {
			cm[ch] = true
		} else {
			delete(ri.Entries.Files, fp)
			log.Println("Chunk lost:", fp, chunkPath(ch))
		}
	}
	now := time.Now().Unix()
	for k, t := range ri.DeletedChunks {
		_, ok := cm[k]
		if ok {
			cm[k] = true
		} else {
			delete(ri.DeletedChunks, k)
			if t < now {
				log.Println("Deleted chunk lost:", chunkPath(k))
			}
		}
	}

	forever := time.Now().AddDate(10, 0, 0).Unix()
	for k, v := range cm {
		if !v {
			log.Println("Lost found chunk", k)
			ri.DeletedChunks[k] = forever
		}
	}

	ri.UploadRemote()
}
