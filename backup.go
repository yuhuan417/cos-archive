package main

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"

	"github.com/golang/glog"
)

func processSingleFile(path string, info os.FileInfo, config *Config, index FileInfoMap, err error) error {
	glog.Info("Processsing: ", path)
	if err != nil {
		glog.Info("On error: ", err, " Skip: ", path)
		return err
	}
	if info.IsDir() {
		for _, skip := range config.SkipList {
			glog.Info("In skiplist: ", skip, " Skip: ", path)
			if info.Name() == skip {
				return filepath.SkipDir
			}
		}
	}
	if !info.IsDir() && !info.Mode().IsRegular() && (info.Mode()&os.ModeSymlink == 0) {
		glog.Info("Skip non-regular file: ", path)
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
	localIndex := generateLocalIndex(config, remoteIndex)

	uploadFiles(config, &localIndex, &remoteIndex)

	j, _ := json.MarshalIndent(localIndex, "", "  ")
	uploadRemoteIndex(config, j)
}
