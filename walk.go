package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	// "time"
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

type ChunkInfo struct {
	DeleteTime int64
}

type ChunkInfoMap = map[string]*ChunkInfo

type Index struct {
	Files     FileInfoMap
	Chunks    ChunkInfoMap
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
	fmt.Println("loading: ", path)
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

func generateLocalIndex(config Config, oldIndex Index) Index {
	fmt.Println("generate local index: ")
	localIndex := Index{}
	localIndex.Files = make(FileInfoMap)
	localIndex.Chunks = make(ChunkInfoMap)
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
		if oldFileInfo, ok := oldIndex.Files[fp]; ok {
			if oldFileInfo.Size == fi.Size && oldFileInfo.ModTime == fi.ModTime && oldFileInfo.Hash != "" {
				h = oldFileInfo.Hash
			}
		}
		if h == "" {
			h = xunleiHash(fp, fi)
		}
		fi.Hash = h
	}
	return localIndex
}

func backupFiles(config Config) {
	oldIndex := loadIndex("log.old")
	chunkSet := buildChunksMap(oldIndex)
	_ = chunkSet
	localIndex := generateLocalIndex(config, oldIndex)

	j, _ := json.MarshalIndent(localIndex, "", "  ")
	fmt.Println("fileinfo json =", string(j))
}

func fullSync(config Config) {
}

func restoreFiles(config Config) {
}

func main() {
	action := flag.String("action", "", "a string")
	configPath := flag.String("config", "foo", "a string")
	flag.Parse()

	config := getConfig(*configPath)
	if *action == "backup" {
		backupFiles(config)
	} else if *action == "fullsync" {
		fullSync(config)
	} else if *action == "restore" {
		restoreFiles(config)
	}
}
