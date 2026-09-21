package main

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// badName is a filename Linux accepts but JSON cannot represent: the exact
// bytes of the mojibake immich resource that broke the real archive.
// \xc8\xa1 is valid UTF-8 (ȡ) and \xe3\xb8\xa6 too (㸦); the \xa6, \xa9 and
// \xc0\xa6 around them are not.
const badName = "\xc8\xa1+\xa6\xe3\xb8\xa6\xa9\xc0\xa6"

func writeBadNameFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, badName)
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanSingleFileSkipsNonUTF8Name(t *testing.T) {
	baseDir := t.TempDir()
	goodFile := filepath.Join(baseDir, "good.txt")
	if err := os.WriteFile(goodFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	badFile := writeBadNameFile(t, baseDir)

	index := NewIndex(Config{BasePath: baseDir}, nil)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := index.scanSingleFile(filepath.Join(baseDir, entry.Name()), entry); err != nil {
			t.Fatalf("scan %q: %v", entry.Name(), err)
		}
	}

	if _, ok := index.Entries.Files[goodFile]; !ok {
		t.Fatalf("expected the valid file to be indexed, got %#v", index.Entries.Files)
	}
	if _, ok := index.Entries.Files[badFile]; ok {
		t.Fatal("expected the non-UTF-8 file to be left out of the index")
	}
	if len(index.skippedLocal) != 1 || index.skippedLocal[0] != badFile {
		t.Fatalf("expected the skipped path to be recorded, got %#v", index.skippedLocal)
	}
}

func TestScanSingleFileSkipsNonUTF8SymlinkTarget(t *testing.T) {
	baseDir := t.TempDir()
	linkPath := filepath.Join(baseDir, "link")
	if err := os.Symlink("/target\xe9", linkPath); err != nil {
		t.Fatal(err)
	}

	index := NewIndex(Config{BasePath: baseDir}, nil)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := index.scanSingleFile(filepath.Join(baseDir, entry.Name()), entry); err != nil {
			t.Fatalf("scan %q: %v", entry.Name(), err)
		}
	}

	if _, ok := index.Entries.Links[linkPath]; ok {
		t.Fatal("expected a symlink with a non-UTF-8 target to be left out of the index")
	}
	if len(index.skippedLocal) != 1 {
		t.Fatalf("expected the skipped link to be recorded, got %#v", index.skippedLocal)
	}
}

func TestScanSingleFileSkipsNonUTF8Dir(t *testing.T) {
	baseDir := t.TempDir()
	badDir := filepath.Join(baseDir, "dir\xe9")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "inner.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	index := NewIndex(Config{BasePath: baseDir}, nil)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		got := index.scanSingleFile(filepath.Join(baseDir, entry.Name()), entry)
		if entry.IsDir() && got != filepath.SkipDir {
			t.Fatalf("expected SkipDir for a non-UTF-8 directory, got %v", got)
		}
	}

	if len(index.Entries.Files) != 0 || len(index.Entries.Dirs) != 0 {
		t.Fatalf("expected nothing under the bad dir to be indexed, got %#v", index.Entries)
	}
}

func TestGenerateLocalReportsSkippedNames(t *testing.T) {
	baseDir := t.TempDir()
	goodFile := filepath.Join(baseDir, "good.txt")
	if err := os.WriteFile(goodFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	writeBadNameFile(t, baseDir)

	index := NewIndex(Config{BasePath: baseDir, FilePaths: []string{"/"}}, nil)

	var logBuf strings.Builder
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(&SimpleHandler{writer: &logBuf}))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	index.GenerateLocal(NewIndex(Config{}, nil))

	if _, ok := index.Entries.Files[goodFile]; !ok {
		t.Fatal("expected the valid file to survive the scan")
	}
	for _, want := range []string{
		"not valid UTF-8",
		`\xa6`, // escaped form of the offending bytes, so the file can be found again
		"count=1",
	} {
		if !strings.Contains(logBuf.String(), want) {
			t.Fatalf("expected log to contain %q, got:\n%s", want, logBuf.String())
		}
	}
}

func TestSaveKeepsExistingIndexWhenMarshalFails(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "meta.json")

	good := NewIndex(Config{}, nil)
	good.Entries.Files["/ok"] = &FileInfo{Size: 1}
	if err := good.Save(fp); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}

	bad := NewIndex(Config{}, nil)
	bad.Entries.Files["/ok"] = &FileInfo{Size: 1}
	bad.Entries.Files["/bad\xe9"] = &FileInfo{Size: 1}

	err = bad.Save(fp)
	if err == nil {
		t.Fatal("expected Save to fail on a non-UTF-8 name")
	}
	if !strings.Contains(err.Error(), `\xe9`) {
		t.Fatalf("expected the error to name the offending path, got %v", err)
	}

	after, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("existing index is gone: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a failed Save modified the existing index")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "meta.json" {
		t.Fatalf("expected only meta.json to remain, got %v", dirNames(entries))
	}
}

func TestUploadRemoteRemovesTempFileWhenMarshalFails(t *testing.T) {
	dir := t.TempDir()
	index := NewIndex(Config{WorkingDir: dir, Index: "meta.json"}, nil)
	index.Entries.Files["/bad\xe9"] = &FileInfo{Size: 1}

	err := index.UploadRemote(context.Background())
	if err == nil {
		t.Fatal("expected UploadRemote to fail on a non-UTF-8 name")
	}
	if !strings.Contains(err.Error(), `\xe9`) {
		t.Fatalf("expected the error to name the offending path, got %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no leftover temp files, got %v", dirNames(entries))
	}
}

func TestUnencodableNamesCoversEveryStringField(t *testing.T) {
	index := NewIndex(Config{}, nil)
	index.Entries.Files["/file\xe9"] = &FileInfo{Size: 1}
	index.Entries.Dirs["/dir\xe9"] = &DirInfo{Mode: 0755}
	index.Entries.Links["/link"] = &LinkInfo{LinkTo: "/target\xe9"}

	got := index.unencodableNames()
	if len(got) != 3 {
		t.Fatalf("expected file, dir and link target to be reported, got %#v", got)
	}
	joined := strings.Join(got, "|")
	for _, want := range []string{"file ", "dir ", "link target "} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in %#v", want, got)
		}
	}
}

func TestWrapMarshalErrorWithoutBadNames(t *testing.T) {
	index := NewIndex(Config{}, nil)
	index.Entries.Files["/ok"] = &FileInfo{Size: 1}
	sentinel := fs.ErrInvalid
	if got := index.wrapMarshalError(sentinel); got != sentinel {
		t.Fatalf("expected the original error to pass through, got %v", got)
	}
}

func dirNames(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
