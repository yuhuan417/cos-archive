package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetConfigParsesAndAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.json")
	content := `{
		"WorkingDir": "/tmp/work",
		"TargetDir": "/tmp/target",
		"FilePaths": ["/backup/1"],
		"COS": {
			"URL": "https://example.com",
			"ID": "id",
			"Key": "key"
		}
	}`
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := getConfig(fp)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Threads != 4 {
		t.Fatalf("unexpected default threads: %d", cfg.Threads)
	}
	if cfg.Index != "meta.json" {
		t.Fatalf("unexpected default index: %s", cfg.Index)
	}
	if cfg.RestoreQPS != 90 {
		t.Fatalf("unexpected default restore qps: %d", cfg.RestoreQPS)
	}
	if cfg.COS.ChunkPrefix != "data/" {
		t.Fatalf("unexpected default prefix: %s", cfg.COS.ChunkPrefix)
	}
	if cfg.COS.Class != "DEEP_ARCHIVE" {
		t.Fatalf("unexpected default class: %s", cfg.COS.Class)
	}
	if cfg.COS.Retries != 1 {
		t.Fatalf("unexpected default retries: %d", cfg.COS.Retries)
	}
}

func TestValidateConfig(t *testing.T) {
	valid := Config{
		FilePaths:  []string{"/backup"},
		Threads:    1,
		WorkingDir: "/tmp/work",
		TargetDir:  "/tmp/target",
		Port:       "8080",
		COS: COSConfig{
			URL: "https://example.com",
			ID:  "id",
			Key: "key",
		},
	}

	tests := []struct {
		name    string
		action  string
		cfg     Config
		wantErr bool
	}{
		{name: "backup valid", action: "backup", cfg: valid},
		{name: "backup missing working dir", action: "backup", cfg: Config{FilePaths: valid.FilePaths, Threads: 1, COS: valid.COS}, wantErr: true},
		{name: "browse valid", action: "browse", cfg: valid},
		{name: "browse missing port", action: "browse", cfg: Config{TargetDir: valid.TargetDir}, wantErr: true},
		{name: "link missing target dir", action: "link", cfg: Config{}, wantErr: true},
		{name: "unknown action", action: "bogus", cfg: valid, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.action, tt.cfg)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
