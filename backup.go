package main

import (
	"encoding/json"
	"log"
	"os"
	"path"
	"path/filepath"

)

func processSingleFile(path string, info os.FileInfo, config Config, index FileInfoMap, err error) error {
	log.Println("Processsing: ", path)
	if err != nil {
		log.Println("On error: ", err, " Skip: ", path)
		return err
	}
	if info.IsDir() {
		for _, skip := range config.SkipList {
			log.Println("In skiplist: ", skip, " Skip: ", path)
			if info.Name() == skip {
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
	localIndex.Chunks = make(ChunkDeleteMarkMap)
	for _, filePath := range config.FilePaths {
		log.Println("Walk ", filePath)
		err := filepath.Walk(filePath, func(path string, info os.FileInfo, err error) error {
			return processSingleFile(path, info, config, localIndex.Files, err)
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
				log.Println("Use cached hash ", h, " for ", fp)
			}
		}
		if h == "" {
			h = xunleiHash(fp, fi)
			log.Println("Caculated hash ", h, " for ", fp)
		}
		fi.Hash = h
	}
	return localIndex
}

func getRemoteIndex(config Config) Index {
	log.Println("Loading remote index")
	old_ri_path := path.Join(config.WorkingDir, config.Index)
	mh := metaHash(old_ri_path)
	rh := getRemoteMetaHash(config)

	ri_path := ""
	fromCOS := false
	if mh == rh {
		log.Println("Hash match, using local old index.")
		ri_path = old_ri_path
		fromCOS = true
	} else {
		ri_path = path.Join(config.WorkingDir, config.Index + ".remote")
		fromCOS = downloadRemoteIndex(config, ri_path)
		log.Println("Download remote index: ", fromCOS)
	}
	ri := loadIndex(ri_path)
	ri.fromCOS = fromCOS
	return ri
}

func backupFiles(config Config) {
	remoteIndex := getRemoteIndex(config)
	localIndex := generateLocalIndex(config, remoteIndex)

	uploadFiles(config, &localIndex, &remoteIndex)

	j, _ := json.MarshalIndent(localIndex, "", "  ")
	uploadRemoteIndex(config, j)
}
