package main

import (
	"fmt"
	"sync"
	"time"
)

type UploadCTX struct {
	path string
	hash string
}

func uploadPayload(ctx UploadCTX) {
	fmt.Println("Upload " + ctx.path + " as " + ctx.hash)
}


func uploadFiles(config Config, localIndex *Index, remoteIndex *Index) {
	deleteTime := time.Now().AddDate(0, 1, 0).Unix()
	remoteChunkMap := buildChunksMap(*remoteIndex)
	localChunkMap := buildChunksMap(*localIndex)

	localIndex.Chunks = make(ChunkDeleteMarkMap)
	for k, v := range remoteIndex.Chunks {
		localIndex.Chunks[k] = v
	}
	wg := &sync.WaitGroup{}
	ch := make(chan UploadCTX, config.Threads)

	uploadFile := func(id int) {
		defer wg.Done()
		for ctx := range ch {
			uploadPayload(ctx)
		}
	}
	wg.Add(config.Threads)
	for i := 0; i < config.Threads; i++ {
		go uploadFile(i)
	}
	for fp, fi := range localIndex.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}

		h := chunkHash(fi)
		// 检查相同的chunk是否已经处理过
		lc, _ := localChunkMap[h]
		if lc {
			continue
		}

		localChunkMap[h] = true
		_, ok := remoteChunkMap[h]
		if ok {
			remoteChunkMap[h] = true
			delete(localIndex.Chunks, h)
		} else {
			// 需要上传
			ctx := new(UploadCTX)
			ctx.path = fp
			ctx.hash = h
			ch <- *ctx
		}
	}

	close(ch)
	wg.Wait()

	for k, v := range remoteChunkMap {
		if !v {
			localIndex.Chunks[k] = deleteTime
		}
	}
}
