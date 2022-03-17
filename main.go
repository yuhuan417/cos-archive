package main

import (
	"flag"
	"log"
	"os"
	"path"
	"time"

	"github.com/allan-simon/go-singleinstance"
)

func main() {
	action := flag.String("action", "backup", "a string")
	configPath := flag.String("config", "config.json", "a string")
	flag.Parse()

	config := getConfig(*configPath)

	if *action == "browse" {
		log.Println("Action browse")
		browseFiles(config)
		return
	}

	lockFile, err := singleinstance.CreateLockFile(path.Join(config.WorkingDir, "pid.lock"))
	if err != nil {
		log.Fatalln("An instance already exists")
	}
	defer lockFile.Close()

	if *action == "restore" {
		log.Println("Action: restore")
		restoreFiles(config)
	} else if *action == "download" {
		log.Println("Action: download")
		downloadFiles(config)
	} else if *action == "link" {
		log.Println("Action: link")
		linkFiles(config)
	} else {
		os.MkdirAll(path.Join(config.WorkingDir, "backup"), os.ModePerm)

		// /share/CACHEDEV1_DATA/Public/@Recently-Snapshot/GMT+08_2022-03-11_0100
		now := time.Now()
		targetPath := "/share/CACHEDEV1_DATA/Public/@Recently-Snapshot/GMT+08_" + now.Format("2006-01-02") + "_0100"
		if _, err := os.Lstat(targetPath); err != nil {
			log.Println("Target doesn't exist: ", err)
		} else {
			symlinkPath := path.Join(config.WorkingDir, "/Recently-Snapshot")
			if _, err := os.Lstat(symlinkPath); err == nil {
				os.Remove(symlinkPath)
			}
			if err := os.Symlink(targetPath, symlinkPath); err == nil {
				if *action == "backup" {
					log.Println("Action: backup")
					backupFiles(config)
				} else if *action == "fsck" {
					log.Println("Action: fsck")
					fsckRemote(config)
				} else if *action == "verify" {
					log.Println("Action: verify")
					verifyFiles(config)
				}
			}
		}
	}
}
