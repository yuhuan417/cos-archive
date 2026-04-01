package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirectoryItemsSortAndMetadata(t *testing.T) {
	now := time.Unix(1700000000, 0)
	items := directoryItems(DirEnt{
		Dirs: DirInfoMap{
			"zeta":  {Mode: 0755},
			"alpha": {Mode: 0700},
		},
		Files: FileInfoMap{
			"b.txt": {Mode: 0644, Size: 2, ModTime: now.Unix()},
			"a.txt": {Mode: 0600, Size: 1, ModTime: now.Unix()},
		},
		Links: LinkInfoMap{
			"link-b": {LinkTo: "/tmp/b"},
			"link-a": {LinkTo: "/tmp/a"},
		},
	})

	names := []string{
		items[0].Name, items[1].Name,
		items[2].Name, items[3].Name,
		items[4].Name, items[5].Name,
	}
	want := []string{"alpha/", "zeta/", "a.txt", "b.txt", "link-a", "link-b"}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("unexpected sort order: %v", names)
		}
	}
	if items[2].Modified != now.String() {
		t.Fatalf("unexpected modified time: %q", items[2].Modified)
	}
}

func TestBuildCloudURLAndRenderers(t *testing.T) {
	cfg := Config{
		COS: COSConfig{
			URL:         "https://example.com/bucket",
			ChunkPrefix: "data/",
		},
	}
	fi := &FileInfo{Size: 5, Hash: HashType{0xaa}}
	u := buildCloudURL(cfg, fi)
	if !strings.Contains(u, "/bucket/data/5-") {
		t.Fatalf("unexpected cloud url: %s", u)
	}
	if got := buildCloudURL(Config{COS: COSConfig{URL: "://bad"}}, fi); got != "" {
		t.Fatalf("expected empty URL for invalid config, got %q", got)
	}

	tests := []struct {
		name string
		fn   func(http.ResponseWriter) error
		want string
	}{
		{
			name: "dir",
			fn: func(w http.ResponseWriter) error {
				return renderDir(DirEnt{Files: FileInfoMap{"a.txt": {Size: 1}}}, "/dir", w)
			},
			want: "Directory listing for /dir",
		},
		{
			name: "file",
			fn: func(w http.ResponseWriter) error {
				return renderFile(cfg, fi, "/dir/file.txt", w)
			},
			want: "/dir/file.txt on cloud:",
		},
		{
			name: "link",
			fn: func(w http.ResponseWriter) error {
				return renderLink(&LinkInfo{LinkTo: "/tmp/target"}, "/dir/link", w)
			},
			want: "/dir/link links to /tmp/target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if err := tt.fn(rec); err != nil {
				t.Fatalf("render error: %v", err)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Fatalf("unexpected content type: %s", got)
			}
			if !strings.Contains(rec.Body.String(), tt.want) {
				t.Fatalf("response body %q missing %q", rec.Body.String(), tt.want)
			}
		})
	}
}

func TestBrowseHandlerRoutes(t *testing.T) {
	entries := *NewDirEnt()
	entries.Dirs["/folder"] = &DirInfo{Mode: 0755}
	entries.Files["/file.txt"] = &FileInfo{Size: 12, ModTime: 1700000000}
	entries.Links["/link"] = &LinkInfo{LinkTo: "/tmp/target"}
	dm := buildDirMap(entries)

	handler := browseHandler(Config{}, entries, dm)
	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{path: "/", wantStatus: http.StatusOK, wantBody: "Directory listing for /"},
		{path: "/file.txt", wantStatus: http.StatusOK, wantBody: "/file.txt"},
		{path: "/link", wantStatus: http.StatusOK, wantBody: "/tmp/target"},
		{path: "/missing", wantStatus: http.StatusNotFound, wantBody: "404 not found"},
		{path: "/%zz", wantStatus: http.StatusBadRequest, wantBody: "bad path"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.URL.Path = tt.path
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("unexpected status: got=%d want=%d", rec.Code, tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body %q missing %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestLoadBrowseIndexAndBrowseFiles(t *testing.T) {
	targetDir := t.TempDir()
	local := NewIndex(Config{}, nil)
	local.Entries.Files["/local.txt"] = &FileInfo{Size: 1}

	remote := NewIndex(Config{}, nil)
	remote.Entries.Files["/remote.txt"] = &FileInfo{Size: 2}

	config := Config{
		TargetDir: targetDir,
		Index:     "meta.json",
		Port:      freeTCPPort(t),
	}
	if err := local.Save(filepath.Join(targetDir, "meta.json")); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadBrowseIndex(config)
	if err != nil {
		t.Fatalf("load local index: %v", err)
	}
	if _, ok := loaded.Entries.Files["/local.txt"]; !ok {
		t.Fatalf("expected local index file, got %#v", loaded.Entries.Files)
	}

	if err := remote.Save(filepath.Join(targetDir, "meta.json.remote")); err != nil {
		t.Fatal(err)
	}
	loaded, err = loadBrowseIndex(config)
	if err != nil {
		t.Fatalf("load remote index: %v", err)
	}
	if _, ok := loaded.Entries.Files["/remote.txt"]; !ok {
		t.Fatalf("expected remote index file, got %#v", loaded.Entries.Files)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- browseFiles(ctx, config)
	}()

	url := "http://127.0.0.1:" + config.Port + "/remote.txt"
	var body string
	for range 50 {
		resp, err := http.Get(url)
		if err == nil {
			b, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				t.Fatalf("read browse response: %v", readErr)
			}
			body = string(b)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(body, "/remote.txt") {
		t.Fatalf("unexpected browse response: %q", body)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("browseFiles returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("browseFiles did not stop after cancellation")
	}

	if _, err := loadBrowseIndex(Config{TargetDir: t.TempDir(), Index: "meta.json"}); err == nil {
		t.Fatal("expected loadBrowseIndex error when no index exists")
	}
}
