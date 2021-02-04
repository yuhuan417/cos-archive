package main

import (
	"flag"
)

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
