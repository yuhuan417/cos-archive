package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type FileInfo struct {
	Name    string
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
	IsDir   bool
	LinkTo	string
}

type FileInfoMap = map[string] FileInfo

func processSingleFile(path string, info os.FileInfo, config Config, index FileInfoMap, err error) error {
	fmt.Println("visited: ", path)
	if (err != nil) {
		fmt.Println("err", err)
		return err
	}
	for _, skip := range config.SkipList {
		if info.Name() == skip {
			return nil
		}
	}
	if (!info.IsDir() && !info.Mode().IsRegular() && (info.Mode() & os.ModeSymlink == 0)) {
		fmt.Println("skip: ", path)
		return nil
	}
	link := ""
	if (info.Mode() & os.ModeSymlink != 0) {
		link, _ = os.Readlink(path)
		fmt.Println(path, "link to:", link)
	}
	f := FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
		LinkTo:	link,
	}
	index[path] = f
	return nil
}

func backupFiles(config Config) {
	localIndex := make(FileInfoMap)
	for _, filePath := range config.FilePaths {
		err := filepath.Walk(filePath, func(path string, info os.FileInfo, err error) error {
			return processSingleFile(path, info, config, localIndex, err)
		})

		if err != nil {
			fmt.Printf("error walking the path %q: %v\n", ".", err)
			continue
		}
	}
	j, _ := json.Marshal(localIndex)
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
	fmt.Println(config)
	if *action == "backup" {
		backupFiles(config)
	} else if *action == "fullsync" {
		fullSync(config)
	} else if *action == "restore" {
		restoreFiles(config)
	}
}
