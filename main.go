package main

import (
	"context"
	"flag"
	"io"
	"os"
	"os/signal"
	"path"
	"strconv"
	"syscall"
)

var (
	browseFilesFunc       = browseFiles
	restoreFilesFunc      = restoreFiles
	downloadFilesFunc     = downloadFiles
	linkFilesFunc         = linkFiles
	mountFilesFunc        = mountFiles
	backupFilesFunc       = backupFiles
	fsckRemoteFunc        = fsckRemote
	verifyFilesFunc       = verifyFiles
	initLoggerFunc        = InitLogger
	getConfigFunc         = getConfig
	validateConfigFunc    = validateConfig
	notifyContextFunc     = signal.NotifyContext
	mkdirAllFunc          = os.MkdirAll
	createLockFileFunc    = createLockFile
	printDebugDetailsFunc = PrintDebugDetails
)

// createLockFile takes an exclusive flock on lockPath and records the owning
// PID, so a second instance on the same state fails fast instead of racing.
// Closing the returned file releases the lock.
func createLockFile(lockPath string) (io.Closer, error) {
	f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		Fatal(err)
	}
	printDebugDetailsFunc()
}

func runMain(args []string) error {
	flags := flag.NewFlagSet("cos-archive", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	action := flags.String("action", "backup", "a string")
	configPath := flags.String("config", "config.json", "a string")
	targetDir := flags.String("target", "", "override target dir")
	mountPoint := flags.String("mountpoint", "", "override mount point")
	verbose := flags.Bool("verbose", false, "enable verbose output")
	if err := flags.Parse(args); err != nil {
		return err
	}

	initLoggerFunc(*verbose)

	config, err := getConfigFunc(*configPath)
	if err != nil {
		return err
	}
	config = applyRuntimeOverrides(config, *targetDir, *mountPoint)
	if err := validateConfigFunc(*action, config); err != nil {
		return err
	}

	ctx, cancel := notifyContextFunc(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var lockFile io.Closer
	if *action != "browse" {
		if *action != "mount" {
			lockPath := getLockFilePath(config)
			if err := mkdirAllFunc(path.Dir(lockPath), 0755); err != nil {
				return err
			}
			lockFile, err = createLockFileFunc(lockPath)
			if err != nil {
				return err
			}
			defer lockFile.Close()
		}
	}

	if err := runAction(ctx, *action, config); err != nil {
		return err
	}
	return nil
}

func runAction(ctx context.Context, action string, config Config) error {
	switch action {
	case "browse":
		return browseFilesFunc(ctx, config)
	case "restore":
		return restoreFilesFunc(ctx, config)
	case "download":
		return downloadFilesFunc(ctx, config)
	case "link":
		return linkFilesFunc(ctx, config)
	case "mount":
		return mountFilesFunc(ctx, config)
	case "backup":
		return backupFilesFunc(ctx, config)
	case "fsck":
		return fsckRemoteFunc(ctx, config)
	case "verify":
		return verifyFilesFunc(ctx, config)
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
