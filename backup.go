package main

import (
	"encoding/json"
	"log"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
)

// UploadCTX struct
type UploadCTX struct {
	localPath  string
	remotePath string
}

func indexSingleFile(path string, info os.FileInfo, config Config, index FileInfoMap, err error) error {
	// log.Println("Processsing: ", path)
	if err != nil {
		log.Println("On error: ", err, " Skip: ", path)
		return err
	}
	if info.IsDir() {
		for _, skip := range config.SkipList {
			if info.Name() == skip {
				log.Println("In skiplist: ", skip, " Skip: ", path)
				return filepath.SkipDir
			}
		}
	}
	if !info.IsDir() && !info.Mode().IsRegular() && (info.Mode()&os.ModeSymlink == 0) {
		log.Println("Skip non-regular file: ", path)
		return nil
	}
	link := ""
	if info.Mode()&os.ModeSymlink != 0 {
		link, _ = os.Readlink(path)
	}
	f := FileInfo{
		Mode:    info.Mode(),
		ModTime: info.ModTime().Unix(),
		IsDir:   info.IsDir(),
		LinkTo:  link,
	}
	if info.Mode().IsRegular() {
		f.Size = info.Size()
	}
	index[path] = &f
	return nil
}

func generateLocalIndex(config Config, remoteIndex Index) Index {
	log.Println("Generating local index")
	localIndex := Index{}
	localIndex.Files = make(FileInfoMap)
	localIndex.DeletedChunks = make(ChunkDeleteMarkMap)
	for _, filePath := range config.FilePaths {
		log.Println("Walk ", filePath)
		err := filepath.Walk(filePath, func(path string, info os.FileInfo, err error) error {
			return indexSingleFile(path, info, config, localIndex.Files, err)
		})

		if err != nil {
			continue
		}
	}
	for fp, fi := range localIndex.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}
		h := ""
		if remoteFileInfo, ok := remoteIndex.Files[fp]; ok {
			if remoteFileInfo.Size == fi.Size && remoteFileInfo.ModTime == fi.ModTime && remoteFileInfo.Hash != "" {
				h = remoteFileInfo.Hash
				// log.Println("Use cached hash ", h, " for ", fp)
			}
		}
		if h == "" {
			h = chunkHash(fp, fi.Size)
			// log.Println("Caculated hash ", h, " for ", fp)
		}
		fi.Hash = h
	}
	return localIndex
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
		log.Fatalln("Upload file error:", ctx, err)
	}
}

func uploadFiles(config Config, localIndex *Index, remoteIndex *Index) {
	deleteTime := time.Now().AddDate(0, 1, 0).Unix()
	remoteChunkMap := buildChunksMap(*remoteIndex)
	remoteIndex.Files = nil
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
	for fp, fi := range localIndex.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}

		h := ChunkKey{fi.Size, fi.Hash}
		// 检查相同的chunk是否已经处理过
		lc, _ := localChunkMap[h]
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
			log.Println("Marking delete chunk:", k)
			localIndex.DeletedChunks[k] = deleteTime
		}
	}
}

func backupFiles(config Config) {
	// if needFSCK() {
	// 	fsckRemote(config)
	// }
	remoteIndex := getRemoteIndex(config)

	localIndex := generateLocalIndex(config, remoteIndex)

	uploadFiles(config, &localIndex, &remoteIndex)

	deleteOutdatedChunks(config, &localIndex)

	j, _ := json.MarshalIndent(localIndex, "", "  ")
	uploadRemoteIndex(config, j)
}
