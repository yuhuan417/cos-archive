package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestGetLockFilePath(t *testing.T) {
	if got := getLockFilePath(Config{WorkingDir: "/work"}); got != "/work/pid.lock" {
		t.Fatalf("unexpected working-dir lock path: %s", got)
	}
	if got := getLockFilePath(Config{TargetDir: "/target"}); got != "/target/pid.lock" {
		t.Fatalf("unexpected target-dir lock path: %s", got)
	}
	if got := getLockFilePath(Config{}); got != "pid.lock" {
		t.Fatalf("unexpected default lock path: %s", got)
	}
}

func TestRunActionUnknown(t *testing.T) {
	err := runAction(context.Background(), "bogus", Config{})
	if !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("expected ErrUnknownAction, got %v", err)
	}
}

func TestRunActionDispatchesAllHandlers(t *testing.T) {
	origBrowse := browseFilesFunc
	origRestore := restoreFilesFunc
	origDownload := downloadFilesFunc
	origLink := linkFilesFunc
	origMount := mountFilesFunc
	origBackup := backupFilesFunc
	origFsck := fsckRemoteFunc
	origVerify := verifyFilesFunc
	defer func() {
		browseFilesFunc = origBrowse
		restoreFilesFunc = origRestore
		downloadFilesFunc = origDownload
		linkFilesFunc = origLink
		mountFilesFunc = origMount
		backupFilesFunc = origBackup
		fsckRemoteFunc = origFsck
		verifyFilesFunc = origVerify
	}()

	sentinel := errors.New("sentinel")
	called := ""
	makeHandler := func(name string) func(context.Context, Config) error {
		return func(context.Context, Config) error {
			called = name
			return sentinel
		}
	}

	browseFilesFunc = makeHandler("browse")
	restoreFilesFunc = makeHandler("restore")
	downloadFilesFunc = makeHandler("download")
	linkFilesFunc = makeHandler("link")
	mountFilesFunc = makeHandler("mount")
	backupFilesFunc = makeHandler("backup")
	fsckRemoteFunc = makeHandler("fsck")
	verifyFilesFunc = makeHandler("verify")

	for _, action := range []string{"browse", "restore", "download", "link", "mount", "backup", "fsck", "verify"} {
		called = ""
		err := runAction(context.Background(), action, Config{})
		if !errors.Is(err, sentinel) {
			t.Fatalf("action %s returned %v, want sentinel", action, err)
		}
		if called != action {
			t.Fatalf("action %s dispatched to %q", action, called)
		}
	}
}

type nopCloser struct {
	closed *bool
}

func (n nopCloser) Close() error {
	if n.closed != nil {
		*n.closed = true
	}
	return nil
}

func TestRunMainBranches(t *testing.T) {
	origInitLogger := initLoggerFunc
	origGetConfig := getConfigFunc
	origValidateConfig := validateConfigFunc
	origNotifyContext := notifyContextFunc
	origMkdirAll := mkdirAllFunc
	origCreateLockFile := createLockFileFunc
	origRunAction := backupFilesFunc
	defer func() {
		initLoggerFunc = origInitLogger
		getConfigFunc = origGetConfig
		validateConfigFunc = origValidateConfig
		notifyContextFunc = origNotifyContext
		mkdirAllFunc = origMkdirAll
		createLockFileFunc = origCreateLockFile
		backupFilesFunc = origRunAction
	}()

	initLoggerFunc = func(bool) {}
	notifyContextFunc = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(parent)
	}

	t.Run("success with lock file and overrides", func(t *testing.T) {
		cfg := Config{WorkingDir: "/work", TargetDir: "/cfg-target"}
		getConfigFunc = func(string) (Config, error) { return cfg, nil }
		validateConfigFunc = func(string, Config) error { return nil }

		var mkdirPath string
		mkdirAllFunc = func(path string, perm os.FileMode) error {
			mkdirPath = path
			return nil
		}
		var closed bool
		var lockPath string
		createLockFileFunc = func(path string) (io.Closer, error) {
			lockPath = path
			return nopCloser{closed: &closed}, nil
		}

		called := false
		backupFilesFunc = func(ctx context.Context, got Config) error {
			called = true
			if got.TargetDir != "/override" {
				t.Fatalf("expected target override, got %q", got.TargetDir)
			}
			return nil
		}

		if err := runMain([]string{"-action=backup", "-target=/override"}); err != nil {
			t.Fatalf("runMain success: %v", err)
		}
		if !called {
			t.Fatal("expected backup handler to be called")
		}
		if mkdirPath != "/work" || lockPath != "/work/pid.lock" {
			t.Fatalf("unexpected lock setup mkdir=%q lock=%q", mkdirPath, lockPath)
		}
		if !closed {
			t.Fatal("expected lock file closer to run")
		}
	})

	t.Run("browse skips lock", func(t *testing.T) {
		getConfigFunc = func(string) (Config, error) { return Config{}, nil }
		validateConfigFunc = func(string, Config) error { return nil }
		mkdirAllFunc = func(string, os.FileMode) error {
			t.Fatal("mkdirAll should not be called for browse")
			return nil
		}
		createLockFileFunc = func(string) (io.Closer, error) {
			t.Fatal("createLockFile should not be called for browse")
			return nil, nil
		}
		browseCalled := false
		origBrowse := browseFilesFunc
		defer func() { browseFilesFunc = origBrowse }()
		browseFilesFunc = func(context.Context, Config) error {
			browseCalled = true
			return nil
		}
		if err := runMain([]string{"-action=browse"}); err != nil {
			t.Fatalf("browse runMain: %v", err)
		}
		if !browseCalled {
			t.Fatal("expected browse handler to be called")
		}
	})

	t.Run("parse error", func(t *testing.T) {
		if err := runMain([]string{"-unknown-flag"}); err == nil {
			t.Fatal("expected flag parse error")
		}
	})

	t.Run("get config error", func(t *testing.T) {
		sentinel := errors.New("bad config")
		getConfigFunc = func(string) (Config, error) { return Config{}, sentinel }
		if err := runMain(nil); !errors.Is(err, sentinel) {
			t.Fatalf("expected getConfig error, got %v", err)
		}
	})

	t.Run("validate error", func(t *testing.T) {
		sentinel := errors.New("bad validate")
		getConfigFunc = func(string) (Config, error) { return Config{}, nil }
		validateConfigFunc = func(string, Config) error { return sentinel }
		if err := runMain(nil); !errors.Is(err, sentinel) {
			t.Fatalf("expected validate error, got %v", err)
		}
	})

	t.Run("mkdir error", func(t *testing.T) {
		sentinel := errors.New("mkdir fail")
		getConfigFunc = func(string) (Config, error) { return Config{WorkingDir: "/work"}, nil }
		validateConfigFunc = func(string, Config) error { return nil }
		mkdirAllFunc = func(string, os.FileMode) error { return sentinel }
		if err := runMain([]string{"-action=backup"}); !errors.Is(err, sentinel) {
			t.Fatalf("expected mkdir error, got %v", err)
		}
	})

	t.Run("lock error", func(t *testing.T) {
		sentinel := errors.New("lock fail")
		getConfigFunc = func(string) (Config, error) { return Config{WorkingDir: "/work"}, nil }
		validateConfigFunc = func(string, Config) error { return nil }
		mkdirAllFunc = func(string, os.FileMode) error { return nil }
		createLockFileFunc = func(string) (io.Closer, error) { return nil, sentinel }
		if err := runMain([]string{"-action=backup"}); !errors.Is(err, sentinel) {
			t.Fatalf("expected lock error, got %v", err)
		}
	})

	t.Run("runAction error", func(t *testing.T) {
		sentinel := errors.New("run fail")
		getConfigFunc = func(string) (Config, error) { return Config{WorkingDir: "/work"}, nil }
		validateConfigFunc = func(string, Config) error { return nil }
		mkdirAllFunc = func(string, os.FileMode) error { return nil }
		createLockFileFunc = func(string) (io.Closer, error) { return nopCloser{}, nil }
		backupFilesFunc = func(context.Context, Config) error { return sentinel }
		if err := runMain([]string{"-action=backup"}); !errors.Is(err, sentinel) {
			t.Fatalf("expected runAction error, got %v", err)
		}
	})
}

func TestMainCallsPrintDebugDetailsOnSuccess(t *testing.T) {
	origArgs := os.Args
	origInitLogger := initLoggerFunc
	origGetConfig := getConfigFunc
	origValidateConfig := validateConfigFunc
	origNotifyContext := notifyContextFunc
	origMkdirAll := mkdirAllFunc
	origCreateLockFile := createLockFileFunc
	origBackup := backupFilesFunc
	origPrint := printDebugDetailsFunc
	defer func() {
		os.Args = origArgs
		initLoggerFunc = origInitLogger
		getConfigFunc = origGetConfig
		validateConfigFunc = origValidateConfig
		notifyContextFunc = origNotifyContext
		mkdirAllFunc = origMkdirAll
		createLockFileFunc = origCreateLockFile
		backupFilesFunc = origBackup
		printDebugDetailsFunc = origPrint
	}()

	os.Args = []string{"cos-archive", "-action=backup"}
	initLoggerFunc = func(bool) {}
	getConfigFunc = func(string) (Config, error) { return Config{WorkingDir: "/work"}, nil }
	validateConfigFunc = func(string, Config) error { return nil }
	notifyContextFunc = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(parent)
	}
	mkdirAllFunc = func(string, os.FileMode) error { return nil }
	createLockFileFunc = func(string) (io.Closer, error) { return nopCloser{}, nil }
	backupFilesFunc = func(context.Context, Config) error { return nil }

	printed := false
	printDebugDetailsFunc = func() { printed = true }

	main()

	if !printed {
		t.Fatal("expected main to print debug details on success")
	}
}

func TestMainExitsOnError(t *testing.T) {
	if os.Getenv("TEST_MAIN_EXIT_HELPER") == "1" {
		initLoggerFunc = InitLogger
		getConfigFunc = func(string) (Config, error) { return Config{}, errors.New("boom") }
		printDebugDetailsFunc = func() {}
		os.Args = []string{"cos-archive"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMainExitsOnError$")
	cmd.Env = append(os.Environ(), "TEST_MAIN_EXIT_HELPER=1")
	out, err := cmd.CombinedOutput()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, err=%v output=%s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("expected fatal output from main error path")
	}
}
