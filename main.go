package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"path"

	"github.com/allan-simon/go-singleinstance"
)

func runCMD(cmd string, check bool) {
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
		runCMD("mkdir -p /backup", false)
		runCMD("umount /backup", false)
		runCMD("lvremove -f /dev/vg3/backup", false)
		runCMD("lvcreate -L 60G -s -n backup /dev/vg3/volume_2", true)
		runCMD("mount /dev/vg3/backup /backup", true)

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
		runCMD("umount /backup", false)
		runCMD("lvremove -f /dev/vg3/backup", false)
	}
}
