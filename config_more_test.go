package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGetConfigErrors(t *testing.T) {
	if _, err := getConfig(filepath.Join(t.TempDir(), "missing.json")); !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("expected ErrConfigInvalid for missing file, got %v", err)
	}

	fp := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(fp, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := getConfig(fp); !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("expected ErrConfigInvalid for bad json, got %v", err)
	}
}

func TestValidateConfigAdditionalBranches(t *testing.T) {
	valid := Config{
		BasePath:   "/base",
		FilePaths:  []string{"/data"},
		Threads:    2,
		WorkingDir: "/work",
		TargetDir:  "/target",
		MountPoint: "/mnt",
		Port:       "8080",
		RestoreQPS: 10,
		COS:        COSConfig{URL: "https://example.com", ID: "id", Key: "key"},
	}

	tests := []struct {
		name    string
		action  string
		config  Config
		wantErr error
	}{
		{name: "restore valid", action: "restore", config: valid},
		{name: "restore missing cos", action: "restore", config: Config{}, wantErr: ErrConfigInvalid},
		{name: "download valid", action: "download", config: valid},
		{name: "download missing target", action: "download", config: Config{WorkingDir: "/work", COS: valid.COS}, wantErr: ErrConfigInvalid},
		{name: "mount valid", action: "mount", config: valid},
		{name: "mount missing mount point", action: "mount", config: Config{TargetDir: "/target"}, wantErr: ErrConfigInvalid},
		{name: "fsck valid", action: "fsck", config: valid},
		{name: "fsck missing workdir", action: "fsck", config: Config{COS: valid.COS}, wantErr: ErrConfigInvalid},
		{name: "verify valid", action: "verify", config: valid},
		{name: "verify missing file paths", action: "verify", config: Config{WorkingDir: "/work", COS: valid.COS}, wantErr: ErrConfigInvalid},
		{name: "empty action", action: "", config: valid, wantErr: ErrUnknownAction},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.action, tt.config)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidateHelperFunctions(t *testing.T) {
	if err := validateThreads(Config{Threads: 1}); err != nil {
		t.Fatalf("expected valid thread count, got %v", err)
	}
	if err := validateThreads(Config{Threads: -1}); !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("expected ErrConfigInvalid for invalid threads, got %v", err)
	}

	if err := validateCOSConfig(Config{COS: COSConfig{URL: "u", ID: "id", Key: "key"}}); err != nil {
		t.Fatalf("expected valid cos config, got %v", err)
	}
	if err := validateCOSConfig(Config{COS: COSConfig{URL: "u", ID: "id"}}); !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("expected ErrConfigInvalid for missing key, got %v", err)
	}
}
