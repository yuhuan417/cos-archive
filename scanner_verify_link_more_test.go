package main

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestScanSingleFileAndGenerateLocal(t *testing.T) {
	baseDir := t.TempDir()
	keepDir := filepath.Join(baseDir, "keep")
	skipDir := filepath.Join(baseDir, "skip")
	if err := os.MkdirAll(keepDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(skipDir, 0755); err != nil {
		t.Fatal(err)
	}

	keepFile := filepath.Join(keepDir, "file.txt")
	if err := os.WriteFile(keepFile, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	modTime := time.Unix(1700000000, 0)
	if err := os.Chtimes(keepFile, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	linkPath := filepath.Join(baseDir, "link")
	if err := os.Symlink(keepFile, linkPath); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		BasePath:  baseDir,
		FilePaths: []string{"/keep", "/skip", "/link"},
		SkipList:  []string{"skip"},
	}
	index := NewIndex(cfg, nil)

	skipRootEntry, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range skipRootEntry {
		if entry.Name() == "skip" {
			if err := index.scanSingleFile(skipDir, entry); err != filepath.SkipDir {
				t.Fatalf("expected filepath.SkipDir for skip entry, got %v", err)
			}
		}
	}

	remoteIndex := NewIndex(cfg, nil)
	remoteHash := HashType{0xaa}
	remoteIndex.Entries.Files[keepFile] = &FileInfo{
		Size:    int64(len("hello world")),
		Mode:    0644,
		ModTime: modTime.Unix(),
		Hash:    remoteHash,
	}
	index.GenerateLocal(remoteIndex)

	if got := index.Entries.Files[keepFile]; got == nil || got.Hash != remoteHash {
		t.Fatalf("expected cached hash for %s, got %#v", keepFile, got)
	}
	if got := index.Entries.Dirs[keepDir]; got == nil || got.Mode != 0755 {
		t.Fatalf("expected keep dir entry, got %#v", got)
	}
	if got := index.Entries.Links[linkPath]; got == nil || string(got.LinkTo) != keepFile {
		t.Fatalf("expected symlink entry, got %#v", got)
	}
	if _, ok := index.Entries.Dirs[skipDir]; ok {
		t.Fatalf("skip dir should not be indexed: %#v", index.Entries.Dirs)
	}
}

func TestVerifySingleFileCases(t *testing.T) {
	baseDir := t.TempDir()
	dirPath := filepath.Join(baseDir, "dir")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(dirPath, "file.txt")
	content := []byte("verify me")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatal(err)
	}
	modTime := time.Unix(1700000100, 0)
	if err := os.Chtimes(filePath, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(baseDir, "link")
	if err := os.Symlink(filePath, linkPath); err != nil {
		t.Fatal(err)
	}

	hash, err := chunkHash(filePath, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}

	index := NewIndex(Config{}, nil)
	index.Entries.Dirs[dirPath] = &DirInfo{Mode: 0755}
	index.Entries.Files[filePath] = &FileInfo{
		Mode:    0644,
		ModTime: modTime.Unix(),
		Size:    int64(len(content)),
		Hash:    hash,
	}
	index.Entries.Links[linkPath] = &LinkInfo{LinkTo: Path(filePath)}

	chunks := ChunksMap{
		ChunkKey{size: int64(len(content)), hash: hash}: true,
	}

	var unmatched []string
	dirEntry, _ := os.ReadDir(baseDir)
	for _, entry := range dirEntry {
		var p string
		switch entry.Name() {
		case "dir":
			p = dirPath
		case "link":
			p = linkPath
		default:
			continue
		}
		if err := verifySingleFile(p, entry, Config{}, *index, chunks, &unmatched); err != nil {
			t.Fatalf("verifySingleFile(%s): %v", p, err)
		}
	}
	fileEntry, err := os.ReadDir(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifySingleFile(filePath, fileEntry[0], Config{}, *index, chunks, &unmatched); err != nil {
		t.Fatalf("verify regular file: %v", err)
	}
	if len(unmatched) != 0 {
		t.Fatalf("expected all entries matched, got %v", unmatched)
	}

	unmatched = nil
	if err := verifySingleFile(filePath, fileEntry[0], Config{}, *index, ChunksMap{}, &unmatched); err != nil {
		t.Fatalf("verify missing chunk: %v", err)
	}
	if len(unmatched) == 0 {
		t.Fatal("expected unmatched file when chunk is missing")
	}

	fifoPath := filepath.Join(baseDir, "named-pipe")
	if err := syscall.Mkfifo(fifoPath, 0644); err != nil {
		t.Fatal(err)
	}
	fifoEntry, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range fifoEntry {
		if entry.Name() == "named-pipe" {
			unmatched = nil
			if err := verifySingleFile(fifoPath, entry, Config{}, *index, chunks, &unmatched); err != nil {
				t.Fatalf("verify fifo: %v", err)
			}
		}
	}
}

func TestVerifySingleFileMismatchAndErrorBranches(t *testing.T) {
	baseDir := t.TempDir()
	dirPath := filepath.Join(baseDir, "dir")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(dirPath, "file.txt")
	if err := os.WriteFile(filePath, []byte("payload"), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(baseDir, "link")
	if err := os.Symlink(filePath, linkPath); err != nil {
		t.Fatal(err)
	}

	dirEntries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	var dirEntry, linkEntry fs.DirEntry
	for _, entry := range dirEntries {
		switch entry.Name() {
		case "dir":
			dirEntry = entry
		case "link":
			linkEntry = entry
		}
	}
	fileEntries, err := os.ReadDir(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	fileEntry := fileEntries[0]

	hash, err := chunkHash(filePath, 7)
	if err != nil {
		t.Fatal(err)
	}
	chunks := ChunksMap{ChunkKey{size: 7, hash: hash}: true}

	t.Run("dir missing meta", func(t *testing.T) {
		var unmatched []string
		if err := verifySingleFile(dirPath, dirEntry, Config{}, *NewIndex(Config{}, nil), chunks, &unmatched); err != nil {
			t.Fatalf("verify dir missing meta: %v", err)
		}
		if len(unmatched) != 1 || unmatched[0] != dirPath {
			t.Fatalf("unexpected unmatched dirs: %v", unmatched)
		}
	})

	t.Run("dir mismatched meta", func(t *testing.T) {
		index := NewIndex(Config{}, nil)
		index.Entries.Dirs[dirPath] = &DirInfo{Mode: 0700}
		var unmatched []string
		if err := verifySingleFile(dirPath, dirEntry, Config{}, *index, chunks, &unmatched); err != nil {
			t.Fatalf("verify dir mismatch: %v", err)
		}
		if len(unmatched) != 1 {
			t.Fatalf("expected unmatched dir, got %v", unmatched)
		}
	})

	t.Run("link missing and mismatched meta", func(t *testing.T) {
		var unmatched []string
		if err := verifySingleFile(linkPath, linkEntry, Config{}, *NewIndex(Config{}, nil), chunks, &unmatched); err != nil {
			t.Fatalf("verify link missing meta: %v", err)
		}
		if len(unmatched) != 1 {
			t.Fatalf("expected unmatched link, got %v", unmatched)
		}

		index := NewIndex(Config{}, nil)
		index.Entries.Links[linkPath] = &LinkInfo{LinkTo: "/wrong"}
		unmatched = nil
		if err := verifySingleFile(linkPath, linkEntry, Config{}, *index, chunks, &unmatched); err != nil {
			t.Fatalf("verify link mismatch: %v", err)
		}
		if len(unmatched) != 1 {
			t.Fatalf("expected mismatched link, got %v", unmatched)
		}
	})

	t.Run("file missing and mismatched meta", func(t *testing.T) {
		var unmatched []string
		if err := verifySingleFile(filePath, fileEntry, Config{}, *NewIndex(Config{}, nil), chunks, &unmatched); err != nil {
			t.Fatalf("verify file missing meta: %v", err)
		}
		if len(unmatched) == 0 {
			t.Fatal("expected unmatched file for missing metadata")
		}

		index := NewIndex(Config{}, nil)
		index.Entries.Files[filePath] = &FileInfo{
			Size:    7,
			Mode:    0600,
			ModTime: 1,
			Hash:    hash,
		}
		unmatched = nil
		if err := verifySingleFile(filePath, fileEntry, Config{}, *index, chunks, &unmatched); err != nil {
			t.Fatalf("verify file mismatch: %v", err)
		}
		if len(unmatched) == 0 {
			t.Fatal("expected mismatched file")
		}
	})

	t.Run("readlink and info errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpLink := filepath.Join(tmpDir, "link")
		if err := os.Symlink(filepath.Join(tmpDir, "target"), tmpLink); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(tmpLink); err != nil {
			t.Fatal(err)
		}
		var unmatched []string
		if err := verifySingleFile(tmpLink, entries[0], Config{}, *NewIndex(Config{}, nil), nil, &unmatched); err == nil {
			t.Fatal("expected readlink error")
		}

		tmpFile := filepath.Join(tmpDir, "file.txt")
		if err := os.WriteFile(tmpFile, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		fileEntries, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatal(err)
		}
		var stale fs.DirEntry
		for _, entry := range fileEntries {
			if entry.Name() == "file.txt" {
				stale = entry
			}
		}
		if err := os.Remove(tmpFile); err != nil {
			t.Fatal(err)
		}
		if err := verifySingleFile(tmpFile, stale, Config{}, *NewIndex(Config{}, nil), nil, &unmatched); err == nil {
			t.Fatal("expected file stat error")
		}
	})
}

func TestVerifyFilesErrorBranches(t *testing.T) {
	t.Run("invalid COS URL", func(t *testing.T) {
		err := verifyFiles(context.Background(), Config{COS: COSConfig{URL: "://bad"}})
		if !errors.Is(err, ErrConfigInvalid) {
			t.Fatalf("expected ErrConfigInvalid, got %v", err)
		}
	})

	t.Run("walk error", func(t *testing.T) {
		server := newFakeCOSServer(t)
		config := Config{
			BasePath:   t.TempDir(),
			FilePaths:  []string{"/missing"},
			WorkingDir: t.TempDir(),
			Index:      "meta.json",
			COS:        testCOSConfig(server),
		}
		index := NewIndex(config, server.newClient(t, config.COS))
		indexBytes := mustMarshalIndex(t, index)
		server.setObject("meta.json.1", indexBytes, http.Header{
			"X-Cos-Meta-Hash": []string{sha1HexBytes(indexBytes)},
		})
		if err := verifyFiles(context.Background(), config); err == nil {
			t.Fatal("expected walk error for missing base path")
		}
	})

	t.Run("unmatched files", func(t *testing.T) {
		server := newFakeCOSServer(t)
		baseDir := t.TempDir()
		workDir := t.TempDir()
		dataDir := filepath.Join(baseDir, "data")
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			t.Fatal(err)
		}
		filePath := filepath.Join(dataDir, "verify.txt")
		if err := os.WriteFile(filePath, []byte("payload"), 0644); err != nil {
			t.Fatal(err)
		}
		hash, err := chunkHash(filePath, int64(len("payload")))
		if err != nil {
			t.Fatal(err)
		}
		config := Config{
			BasePath:   baseDir,
			FilePaths:  []string{"/data"},
			WorkingDir: workDir,
			Index:      "meta.json",
			COS:        testCOSConfig(server),
		}
		index := NewIndex(config, server.newClient(t, config.COS))
		index.Entries.Dirs[dataDir] = &DirInfo{Mode: 0755}
		indexBytes := mustMarshalIndex(t, index)
		server.setObject("meta.json.1", indexBytes, http.Header{
			"X-Cos-Meta-Hash": []string{sha1HexBytes(indexBytes)},
		})
		server.setObject(path.Join(config.COS.ChunkPrefix, chunkPath(ChunkKey{size: int64(len("payload")), hash: hash})), []byte("payload"), nil)
		if err := verifyFiles(context.Background(), config); err == nil || !strings.Contains(err.Error(), "verify failed") {
			t.Fatalf("expected verify failed error, got %v", err)
		}
	})
}

func TestLinkFiles(t *testing.T) {
	targetDir := t.TempDir()
	restoreRoot := filepath.Join(targetDir, "restore")

	hash := HashType{0xaa, 0xbb}
	chunkKey := chunkPath(ChunkKey{size: 5, hash: hash})
	chunkDir := filepath.Join(targetDir, "chunks", chunkKey[len(chunkKey)-2:])
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		t.Fatal(err)
	}
	chunkPath := filepath.Join(chunkDir, chunkKey)
	if err := os.WriteFile(chunkPath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	index := NewIndex(Config{}, nil)
	index.Entries.Dirs["/nested"] = &DirInfo{Mode: 0755}
	index.Entries.Files["/nested/file.txt"] = &FileInfo{
		Size:    5,
		Mode:    0600,
		ModTime: 1700000200,
		Hash:    hash,
	}
	index.Entries.Links["/nested/link"] = &LinkInfo{LinkTo: "/tmp/target"}
	if err := index.Save(filepath.Join(targetDir, "meta.json.remote")); err != nil {
		t.Fatal(err)
	}

	if err := linkFiles(context.Background(), Config{TargetDir: targetDir, Index: "meta.json"}); err != nil {
		t.Fatalf("linkFiles: %v", err)
	}

	linkedFile := filepath.Join(restoreRoot, "/nested/file.txt")
	content, err := os.ReadFile(linkedFile)
	if err != nil {
		t.Fatalf("read linked file: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("unexpected linked content: %q", content)
	}
	fi, err := os.Stat(linkedFile)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("unexpected file mode: %o", fi.Mode().Perm())
	}
	if fi.ModTime().Unix() != 1700000200 {
		t.Fatalf("unexpected mod time: %d", fi.ModTime().Unix())
	}
	linkTarget, err := os.Readlink(filepath.Join(restoreRoot, "/nested/link"))
	if err != nil {
		t.Fatalf("read linked symlink: %v", err)
	}
	if linkTarget != "/tmp/target" {
		t.Fatalf("unexpected link target: %s", linkTarget)
	}
}
