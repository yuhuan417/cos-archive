package main

import (
	"flag"
	"log/slog"
	"os"
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
	validateConfig(*action, config)

	if *action != "browse" {
		lockPath := getLockFilePath(config)
		if err := os.MkdirAll(path.Dir(lockPath), 0755); err != nil {
			Fatal("Create lock dir error:", err)
		}
		lockFile, err := singleinstance.CreateLockFile(lockPath)
		if err != nil {
			Fatal("An instance already exists")
		}
		defer lockFile.Close()
	}

	switch *action {
	case "browse":
		slog.Info("Action: browse")
		browseFiles(config)
	case "restore":
		slog.Info("Action: restore")
		restoreFiles(config)
	case "download":
		slog.Info("Action: download")
		downloadFiles(config)
	case "link":
		slog.Info("Action: link")
		linkFiles(config)
	case "backup":
		slog.Info("Action: backup")
		backupFiles(config)
	case "fsck":
		slog.Info("Action: fsck")
		fsckRemote(config)
	case "verify":
		slog.Info("Action: verify")
		verifyFiles(config)
	default:
		Fatal("Unknown action:", *action)
	}

	// 在程序结束时输出调试信息（非 verbose 模式）
	PrintDebugDetails()
}

func getLockFilePath(config Config) string {
	if config.WorkingDir != "" {
		return path.Join(config.WorkingDir, "pid.lock")
	}
	if config.TargetDir != "" {
		return path.Join(config.TargetDir, "pid.lock")
	}
	return "pid.lock"
}
