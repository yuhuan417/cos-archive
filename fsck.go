package main

import (
	"log/slog"
	"time"
)

func fsckRemote(config Config) {
	ri := NewIndex(config)
	err := ri.LoadRemote()

	if err != nil {
		Fatal("Can't load remote index: ", err)
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
			slog.Debug("Chunk lost", "fp", fp, "path", chunkPath(ch))
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
				slog.Debug("Deleted chunk lost", "path", chunkPath(k))
			}
		}
	}

	forever := time.Now().AddDate(10, 0, 0).Unix()
	for k, v := range cm {
		if !v {
			slog.Debug("Lost found chunk", "chunk", k)
			ri.DeletedChunks[k] = forever
		}
	}

	ri.UploadRemote()
}
