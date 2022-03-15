package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/allan-simon/go-singleinstance"
)

func runCMD(cmd string, check bool) {
	fmt.Println(cmd)
	c := exec.Command("bash", "-c", cmd)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	err := c.Run()

	if err != nil && check {
		log.Println("runCMD failed: ", cmd)
	}
}

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
		runCMD("mkdir -p "+path.Join(config.WorkingDir, "backup"), false)

		// /share/CACHEDEV1_DATA/Public/@Recently-Snapshot/GMT+08_2022-03-11_0100
		now := time.Now()
		s := "/share/CACHEDEV1_DATA/Public/@Recently-Snapshot/GMT+08_" + now.Format("2006-01-02") + "_0100"
		runCMD("rm -f "+path.Join(config.WorkingDir, "/Recently-Snapshot"), false)
		runCMD("ln -s -f "+s+" "+path.Join(config.WorkingDir, "/Recently-Snapshot"), false)

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
