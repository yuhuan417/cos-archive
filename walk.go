package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"path"
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
	fromCOS bool  // true: 云端 false: 云端加载失败，此时需要在上传时检测chunk是否已经存在，避免重复上传浪费
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

func getRemoteIndex(config Config) Index {
	old_ri_path := path.Join(config.WorkingDir, "meta.json")
	mh := metaHash(old_ri_path)
	rh := getRemoteMetaHash(config)

	ri_path := ""
	fromCOS := false
	if mh == rh {
		ri_path = old_ri_path
		fromCOS = true
	} else {
		ri_path = path.Join(config.WorkingDir, "meta.remote.json")
		fromCOS = downloadRemoteIndex(config, ri_path)
	}
	ri := loadIndex(ri_path)
	ri.fromCOS = fromCOS
	return ri
}

func backupFiles(config Config) {
	remoteIndex := getRemoteIndex(config)
	// remoteIndex := loadIndex("log.old")
	localIndex := generateLocalIndex(config, remoteIndex)

	uploadFiles(config, &localIndex, &remoteIndex)

	j, _ := json.MarshalIndent(localIndex, "", "  ")
	uploadRemoteIndex(config, j)
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
