package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type FileInfo struct {
	Size    int64
	Mode    os.FileMode
	ModTime int64
	IsDir   bool
	LinkTo  string
	Hash    string
}

type FileInfoMap = map[string]*FileInfo

type ChunkDeleteMarkMap = map[string] int64

type Index struct {
	Files     FileInfoMap
	Chunks    ChunkDeleteMarkMap
}

type ChunksMap = map[string] bool

func buildChunksMap(index Index) ChunksMap{
	chunksMap := make(ChunksMap)
	for _, fi := range index.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}

		chunksMap[chunkHash(fi)] = false
	}
	return chunksMap
}

func loadIndex(path string) Index {
	index := Index{}
	jf, err := os.Open(path)
	if err != nil {
		return index
	}
	defer jf.Close()

	jsonParser := json.NewDecoder(jf)
	if err = jsonParser.Decode(&index); err != nil {
		return index
	}

	return index
}

func processSingleFile(path string, info os.FileInfo, config *Config, index FileInfoMap, err error) error {
	if err != nil {
		return err
	}
	if info.IsDir() {
		for _, skip := range config.SkipList {
			if info.Name() == skip {
				return filepath.SkipDir
			}
		}
	}
	if !info.IsDir() && !info.Mode().IsRegular() && (info.Mode()&os.ModeSymlink == 0) {
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
	localIndex := Index{}
	localIndex.Files = make(FileInfoMap)
	localIndex.Chunks = make(ChunkDeleteMarkMap)
	for _, filePath := range config.FilePaths {
		err := filepath.Walk(filePath, func(path string, info os.FileInfo, err error) error {
			return processSingleFile(path, info, &config, localIndex.Files, err)
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
			}
		}
		if h == "" {
			h = xunleiHash(fp, fi)
		}
		fi.Hash = h
	}
	return localIndex
}

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


func backupFiles(config Config) {
	remoteIndex := loadIndex("log.old")
	localIndex := generateLocalIndex(config, remoteIndex)

	uploadFiles(config, &localIndex, &remoteIndex)

	j, _ := json.MarshalIndent(localIndex, "", "  ")
	fmt.Println("fileinfo json =", string(j))
}

func fsckRemote(config Config) {
}

func restoreFiles(config Config) {
}

func main() {
	action := flag.String("action", "backup", "a string")
	configPath := flag.String("config", "config.json", "a string")
	flag.Parse()

	config := getConfig(*configPath)
	if *action == "backup" {
		backupFiles(config)
	} else if *action == "fsck" {
		fsckRemote(config)
	} else if *action == "restore" {
		restoreFiles(config)
	}
}
