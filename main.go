package main

import (
	"flag"
	"log"
)

func main() {
	action := flag.String("action", "backup", "a string")
	configPath := flag.String("config", "config.json", "a string")
	flag.Parse()

	config := getConfig(*configPath)
	if *action == "backup" {
		log.Println("Action: backup")
		backupFiles(config)
	} else if *action == "fsck" {
		log.Println("Action: fsck")
		fsckRemote(config)
	} else if *action == "verify" {
		log.Println("Action: verify")
		verifyFiles(config)
	} else if *action == "restore" {
		log.Println("Action: restore")
		restoreFiles(config)
	}
}
