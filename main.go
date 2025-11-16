package main

import (
	"flag"
	"log/slog"
	"path"

	"github.com/allan-simon/go-singleinstance"
)

func main() {
	action := flag.String("action", "backup", "a string")
	configPath := flag.String("config", "config.json", "a string")
	verbose := flag.Bool("verbose", false, "enable verbose output")
	flag.Parse()

	// 初始化日志系统
	InitLogger(*verbose)

	config := getConfig(*configPath)

	if *action == "browse" {
		slog.Info("Action browse")
		browseFiles(config)
		return
	}

	lockFile, err := singleinstance.CreateLockFile(path.Join(config.WorkingDir, "pid.lock"))
	if err != nil {
		Fatal("An instance already exists")
	}
	defer lockFile.Close()

	if *action == "restore" {
		slog.Info("Action: restore")
		restoreFiles(config)
	} else if *action == "download" {
		slog.Info("Action: download")
		downloadFiles(config)
	} else if *action == "link" {
		slog.Info("Action: link")
		linkFiles(config)
	} else if *action == "backup" {
		slog.Info("Action: backup")
		backupFiles(config)
	} else if *action == "fsck" {
		slog.Info("Action: fsck")
		fsckRemote(config)
	} else if *action == "verify" {
		slog.Info("Action: verify")
		verifyFiles(config)
	}
	
	// 在程序结束时输出调试信息（非 verbose 模式）
	PrintDebugDetails()
}
