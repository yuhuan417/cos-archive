package main

import (
	"log"
	"os"
	"path"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
)

// UploadCTX struct
type UploadCTX struct {
	localPath  string
	remotePath string
}

func uploadPayload(c *COS, config Config, ctx UploadCTX) {
	rp := path.Join(config.COS.ChunkPrefix, ctx.remotePath)

	header := c.GetHeader(rp)
	if header != nil {
		log.Println("File already exists:", ctx, rp)
		return
	}

	err := c.UploadFile(rp, ctx.localPath, config.COS.Class, nil)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("Local file not exist:", ctx, err)
		} else {
			log.Fatalln("Upload file error:", ctx, err)
		}
	}
	log.Println("Uploaded:", ctx.localPath)
}

func uploadFiles(config Config, localIndex *Index, remoteIndex *Index) {
	deleteTime := time.Now().AddDate(0, 6, 0).Unix()
	remoteChunkMap := buildChunksMap(*remoteIndex)
	remoteIndex.Entries.Files = nil
	remoteIndex.Entries.Dirs = nil
	remoteIndex.Entries.Links = nil
	localChunkMap := buildChunksMap(*localIndex)

	localIndex.DeletedChunks = make(ChunkDeleteMarkMap)
	for k, v := range remoteIndex.DeletedChunks {
		if v > deleteTime {
			v = deleteTime
		}
		localIndex.DeletedChunks[k] = v
	}

	wg := &sync.WaitGroup{}
	ch := make(chan UploadCTX, config.Threads)

	cnt := 0
	uploadSize := int64(0)
	totalSize := int64(0)

	uploadFile := func(id int) {
		defer wg.Done()
		c := NewCOS(config.COS)
		for ctx := range ch {
			uploadPayload(c, config, ctx)
		}
	}
	wg.Add(config.Threads)
	for i := 0; i < config.Threads; i++ {
		go uploadFile(i)
	}
	for fp, fi := range localIndex.Entries.Files {
		h := ChunkKey{fi.Size, fi.Hash}
		// 检查相同的chunk是否已经处理过
		lc := localChunkMap[h]
		if lc {
			continue
		}

		totalSize = totalSize + fi.Size
		localChunkMap[h] = true
		_, ok := remoteChunkMap[h]
		if ok {
			remoteChunkMap[h] = true
		} else {
			// 需要上传
			ctx := new(UploadCTX)
			ctx.localPath = fp
			ctx.remotePath = chunkPath(h)
			cnt = cnt + 1
			uploadSize = uploadSize + fi.Size
			ch <- *ctx
		}
		_, ok = localIndex.DeletedChunks[h]
		if ok {
			delete(localIndex.DeletedChunks, h)
		}
	}

	close(ch)
	wg.Wait()

	log.Println("Uploaded ", cnt, " files, ", humanize.IBytes(uint64(uploadSize)))
	log.Println("Total chunk size: ", humanize.IBytes(uint64(totalSize)))
	for k, v := range remoteChunkMap {
		if !v {
			log.Println("Marking delete chunk:", chunkPath(k))
			localIndex.DeletedChunks[k] = deleteTime
		}
	}
	localIndex.DeleteOutdatedChunks()
}

func backupFiles(config Config) {
	// if needFSCK() {
	// 	fsckRemote(config)
	// }
	remoteIndex := NewIndex(config)
	remoteIndex.LoadRemote()

	localIndex := NewIndex(config)
	localIndex.GenerateLocal(remoteIndex)

	uploadFiles(config, localIndex, remoteIndex)
	localIndex.UploadRemote()
}
