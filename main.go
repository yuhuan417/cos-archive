package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/allan-simon/go-singleinstance"
)

func main() {
	action := flag.String("action", "backup", "a string")
	configPath := flag.String("config", "config.json", "a string")
	verbose := flag.Bool("verbose", false, "enable verbose output")
	flag.Parse()

	InitLogger(*verbose)

	config, err := getConfig(*configPath)
	if err != nil {
		Fatal(err)
	}
	if err := validateConfig(*action, config); err != nil {
		Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

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

	if err := runAction(ctx, *action, config); err != nil {
		Fatal(err)
	}

	PrintDebugDetails()
}

func runAction(ctx context.Context, action string, config Config) error {
	switch action {
	case "browse":
		return browseFiles(ctx, config)
	case "restore":
		return restoreFiles(ctx, config)
	case "download":
		return downloadFiles(ctx, config)
	case "link":
		return linkFiles(ctx, config)
	case "backup":
		return backupFiles(ctx, config)
	case "fsck":
		return fsckRemote(ctx, config)
	case "verify":
		return verifyFiles(ctx, config)
	default:
		return ErrUnknownAction
	}
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
