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
}

func processSingleFile(path string, info os.FileInfo, subDirToSkip string, err error) error {
	if err != nil {
		fmt.Printf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
		return err
	}
	if info.IsDir() && info.Name() == subDirToSkip {
		fmt.Printf("skipping a dir without errors: %+v \n", info.Name())
		return filepath.SkipDir
	}
	fmt.Printf("visited file or dir: %q\n", path)
	f := FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}
	j, _ := json.Marshal(f)
	fmt.Println("fileinfo json =", string(j))
	return nil
}

func backupFiles(config Config) {
	subDirToSkip := ".git"
	for _, filePath := range config.FilePaths {
		err := filepath.Walk(filePath, func(path string, info os.FileInfo, err error) error {
			return processSingleFile(path, info, subDirToSkip, err)
		})

		if err != nil {
			fmt.Printf("error walking the path %q: %v\n", ".", err)
			return
		}
	}
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
