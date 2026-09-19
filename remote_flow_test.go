package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

func testCOSConfig(server *fakeCOSServer) COSConfig {
	return COSConfig{
		URL:         server.srv.URL,
		ID:          "id",
		Key:         "key",
		ChunkPrefix: "data/",
		Class:       "STANDARD",
		Retries:     1,
	}
}

func sha1HexBytes(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

func keysWithPrefix(keys []string, prefix string) []string {
	var filtered []string
	for _, key := range keys {
		if strings.HasPrefix(key, prefix) {
			filtered = append(filtered, key)
		}
	}
	sort.Strings(filtered)
	return filtered
}

func TestChunkAndCOSOperations(t *testing.T) {
	index := NewIndex(Config{}, nil)
	hash := HashType{0x01, 0x02}
	index.Entries.Files["/a"] = &FileInfo{Size: 5, Hash: hash}
	index.Entries.Files["/b"] = &FileInfo{Size: 5, Hash: hash}
	cm := buildChunksMap(*index)
	if len(cm) != 1 {
		t.Fatalf("expected deduped chunk map, got %d entries", len(cm))
	}

	parsed := parseChunkKeyFromString("5-" + strings.Repeat("00", 20))
	if parsed.size != 5 {
		t.Fatalf("unexpected parsed chunk size: %d", parsed.size)
	}
	if parsed := parseChunkKeyFromString("broken"); parsed != (ChunkKey{}) {
		t.Fatalf("expected zero chunk key for invalid input, got %#v", parsed)
	}

	if _, err := NewCOS(COSConfig{URL: "://bad"}); !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("expected ErrConfigInvalid for bad URL, got %v", err)
	}

	server := newFakeCOSServer(t)
	server.setListPageSize(1)
	server.setObject("data/existing.txt", []byte("hello"), http.Header{"X-Cos-Meta-Hash": []string{"meta-1"}})
	server.setObject("data/one", []byte("1"), nil)
	server.setObject("data/two", []byte("2"), nil)

	client := server.newClient(t, testCOSConfig(server))
	ctx := context.Background()

	localPath := filepath.Join(t.TempDir(), "download.txt")
	if err := client.DownloadFile(ctx, "data/existing.txt", localPath); err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if got, err := os.ReadFile(localPath); err != nil || string(got) != "hello" {
		t.Fatalf("unexpected downloaded content %q err=%v", got, err)
	}

	uploadSrc := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(uploadSrc, []byte("payload"), 0644); err != nil {
		t.Fatal(err)
	}
	server.addFailure(http.MethodPut, "data/retry.txt", fakeResponseSpec{
		status:  http.StatusServiceUnavailable,
		code:    "ServiceUnavailable",
		message: "try again",
	})
	header := http.Header{}
	header.Set("x-cos-meta-hash", "abc123")
	if err := client.UploadFile(ctx, "data/retry.txt", uploadSrc, "STANDARD", header); err != nil {
		t.Fatalf("UploadFile retry: %v", err)
	}
	if server.requestCount(http.MethodPut, "data/retry.txt") != 2 {
		t.Fatalf("expected retry upload count 2, got %d", server.requestCount(http.MethodPut, "data/retry.txt"))
	}
	if string(server.objectBody("data/retry.txt")) != "payload" {
		t.Fatalf("unexpected uploaded payload: %q", server.objectBody("data/retry.txt"))
	}
	if got := server.objectHeader("data/retry.txt").Get("X-Cos-Meta-Hash"); got != "abc123" {
		t.Fatalf("unexpected stored metadata header: %q", got)
	}

	for i := 0; i < 3; i++ {
		server.addFailure(http.MethodPut, "data/fail.txt", fakeResponseSpec{
			status:  http.StatusInternalServerError,
			code:    "InternalError",
			message: "boom",
		})
	}
	if err := client.UploadFile(ctx, "data/fail.txt", uploadSrc, "STANDARD", nil); err == nil {
		t.Fatal("expected UploadFile error for non-retryable failure")
	}

	h, err := client.GetHeader(ctx, "data/existing.txt")
	if err != nil || h.Get("X-Cos-Meta-Hash") != "meta-1" {
		t.Fatalf("unexpected header: %v %v", h, err)
	}
	h, err = client.GetHeader(ctx, "data/missing.txt")
	if err != nil || h != nil {
		t.Fatalf("expected nil header for missing object, got header=%v err=%v", h, err)
	}

	var keys []string
	if err := client.ScanFiles(ctx, "data/", func(obj cos.Object) {
		keys = append(keys, obj.Key)
	}); err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	sort.Strings(keys)
	if len(keys) < 4 || keys[0] != "data/existing.txt" {
		t.Fatalf("unexpected scanned keys: %v", keys)
	}

	if err := client.RestoreFile(ctx, "data/existing.txt"); err != nil {
		t.Fatalf("RestoreFile success: %v", err)
	}
	server.addFailure(http.MethodPost, "data/missing.txt?restore", fakeResponseSpec{
		status:  http.StatusNotFound,
		code:    "NoSuchKey",
		message: "missing",
	})
	if err := client.RestoreFile(ctx, "data/missing.txt"); !cos.IsNotFoundError(err) {
		t.Fatalf("expected not found restore error, got %v", err)
	}

	if err := client.DeleteFile(ctx, "data/one"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if server.objectExists("data/one") {
		t.Fatal("expected deleted object to be removed")
	}
}

func TestIndexRemoteOperations(t *testing.T) {
	t.Run("requireCOS and metaHash", func(t *testing.T) {
		config := Config{WorkingDir: t.TempDir(), Index: "meta.json"}
		index := NewIndex(config, nil)
		if _, err := index.requireCOS(); err == nil {
			t.Fatal("expected requireCOS error when client is nil")
		}

		fp := filepath.Join(config.WorkingDir, "content.txt")
		if err := os.WriteFile(fp, []byte("abc"), 0644); err != nil {
			t.Fatal(err)
		}
		if got := index.metaHash(fp); got != sha1HexBytes([]byte("abc")) {
			t.Fatalf("unexpected meta hash: %s", got)
		}
		if got := index.metaHash(filepath.Join(config.WorkingDir, "missing.txt")); got != "" {
			t.Fatalf("expected empty hash for missing file, got %q", got)
		}

		bad := filepath.Join(config.WorkingDir, "bad.json")
		if err := os.WriteFile(bad, []byte("{"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := index.Load(bad); err == nil {
			t.Fatal("expected load error for invalid json")
		}
	})

	t.Run("downloadRemote and getRemoteHash errors", func(t *testing.T) {
		server := newFakeCOSServer(t)
		config := Config{
			WorkingDir: t.TempDir(),
			Index:      "meta.json",
			COS:        testCOSConfig(server),
		}
		client := server.newClient(t, config.COS)
		index := NewIndex(config, client)

		server.setPersistentFailure(http.MethodGet, "meta.json.1", fakeResponseSpec{
			status:  http.StatusInternalServerError,
			code:    "InternalError",
			message: "download failed",
		})
		if err := index.downloadRemote(context.Background(), "meta.json.1", filepath.Join(config.WorkingDir, "meta.json.remote")); err == nil {
			t.Fatal("expected downloadRemote error")
		}

		server.setPersistentFailure(http.MethodHead, "meta.json.1", fakeResponseSpec{
			status:  http.StatusInternalServerError,
			code:    "InternalError",
			message: "head failed",
		})
		if _, err := index.getRemoteHash(context.Background(), "meta.json.1"); err == nil {
			t.Fatal("expected getRemoteHash error")
		}
	})

	t.Run("upload and load remote", func(t *testing.T) {
		server := newFakeCOSServer(t)
		config := Config{
			WorkingDir: t.TempDir(),
			Index:      "meta.json",
			COS:        testCOSConfig(server),
		}
		client := server.newClient(t, config.COS)
		ctx := context.Background()

		index := NewIndex(config, client)
		index.Entries.Files["/local.txt"] = &FileInfo{Size: 3, Hash: HashType{0xaa}}
		if err := index.UploadRemote(ctx); err != nil {
			t.Fatalf("UploadRemote: %v", err)
		}

		uploaded := keysWithPrefix(server.objectKeys(), "meta.json.")
		if len(uploaded) != 1 {
			t.Fatalf("expected one uploaded index, got %v", uploaded)
		}
		if _, err := os.Stat(filepath.Join(config.WorkingDir, "meta.json")); err != nil {
			t.Fatalf("expected local meta.json after upload, got %v", err)
		}
		if server.objectHeader(uploaded[0]).Get("X-Cos-Meta-Hash") == "" {
			t.Fatalf("expected uploaded index hash metadata, got %v", server.objectHeader(uploaded[0]))
		}

		if err := index.UploadRemote(ctx); err != nil {
			t.Fatalf("UploadRemote hash match: %v", err)
		}
		if got := keysWithPrefix(server.objectKeys(), "meta.json."); len(got) != 1 {
			t.Fatalf("expected no extra index upload on hash match, got %v", got)
		}

		loaded := NewIndex(config, client)
		if err := loaded.LoadRemote(ctx); err != nil {
			t.Fatalf("LoadRemote cached: %v", err)
		}
		if _, ok := loaded.Entries.Files["/local.txt"]; !ok {
			t.Fatalf("expected cached local entry, got %#v", loaded.Entries.Files)
		}
		if server.requestCount(http.MethodGet, uploaded[0]) != 0 {
			t.Fatalf("expected cached load to avoid object download, got %d GETs", server.requestCount(http.MethodGet, uploaded[0]))
		}

		remote := NewIndex(config, client)
		remote.Entries.Files["/remote.txt"] = &FileInfo{Size: 7, Hash: HashType{0xbb}}
		remoteBytes := mustMarshalIndex(t, remote)
		server.setObject("meta.json.9999999999", remoteBytes, http.Header{
			"X-Cos-Meta-Hash": []string{sha1HexBytes(remoteBytes)},
		})

		fresh := NewIndex(config, client)
		if err := fresh.LoadRemote(ctx); err != nil {
			t.Fatalf("LoadRemote download: %v", err)
		}
		if _, ok := fresh.Entries.Files["/remote.txt"]; !ok {
			t.Fatalf("expected downloaded remote entry, got %#v", fresh.Entries.Files)
		}
		if _, err := os.Stat(filepath.Join(config.WorkingDir, "meta.json.remote")); err != nil {
			t.Fatalf("expected downloaded remote index file, got %v", err)
		}
	})

	t.Run("get latest index cleanup", func(t *testing.T) {
		server := newFakeCOSServer(t)
		config := Config{
			WorkingDir: t.TempDir(),
			Index:      "meta.json",
			COS:        testCOSConfig(server),
		}
		client := server.newClient(t, config.COS)
		index := NewIndex(config, client)
		for i := 1; i <= 35; i++ {
			server.setObject("meta.json."+strconv.Itoa(i), []byte("x"), nil)
		}
		latest, err := index.getLatestIndex(context.Background())
		if err != nil {
			t.Fatalf("getLatestIndex: %v", err)
		}
		if latest != "meta.json.35" {
			t.Fatalf("unexpected latest index: %s", latest)
		}
		if server.objectExists("meta.json.1") || server.objectExists("meta.json.5") {
			t.Fatalf("expected oldest indexes to be deleted, got %v", server.objectKeys())
		}
	})

	t.Run("delete outdated chunks", func(t *testing.T) {
		server := newFakeCOSServer(t)
		config := Config{
			WorkingDir: t.TempDir(),
			Index:      "meta.json",
			COS:        testCOSConfig(server),
		}
		client := server.newClient(t, config.COS)
		index := NewIndex(config, client)

		oldChunk := ChunkKey{size: 1, hash: HashType{0x01}}
		keepChunk := ChunkKey{size: 2, hash: HashType{0x02}}
		failChunk := ChunkKey{size: 3, hash: HashType{0x03}}
		oldPath := path.Join(config.COS.ChunkPrefix, chunkPath(oldChunk))
		keepPath := path.Join(config.COS.ChunkPrefix, chunkPath(keepChunk))
		failPath := path.Join(config.COS.ChunkPrefix, chunkPath(failChunk))

		server.setObject(oldPath, []byte("old"), http.Header{
			"Last-Modified": []string{time.Unix(1000, 0).UTC().Format("Mon, 2 Jan 2006 15:04:05 MST")},
		})
		server.setObject(keepPath, []byte("keep"), http.Header{
			"Last-Modified": []string{time.Now().UTC().Format("Mon, 2 Jan 2006 15:04:05 MST")},
		})
		server.setPersistentFailure(http.MethodHead, failPath, fakeResponseSpec{
			status:  http.StatusInternalServerError,
			code:    "InternalError",
			message: "head failed",
		})

		now := time.Now().Unix()
		index.DeletedChunks[oldChunk] = now - 10
		index.DeletedChunks[keepChunk] = now - 10
		index.DeletedChunks[failChunk] = now - 10

		var logBuf strings.Builder
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(&SimpleHandler{writer: &logBuf}))
		t.Cleanup(func() {
			slog.SetDefault(oldLogger)
		})

		err := index.DeleteOutdatedChunks(context.Background())
		if err == nil {
			t.Fatal("expected joined error for failed HEAD request")
		}
		if got := logBuf.String(); !strings.Contains(got, "Deleting outdated remote chunk count=1 size=1 B") {
			t.Fatalf("expected deletion log with freed size, got %q", got)
		}
		if _, ok := index.DeletedChunks[oldChunk]; ok {
			t.Fatalf("expected old chunk deletion mark removed, got %#v", index.DeletedChunks)
		}
		if !server.objectExists(keepPath) {
			t.Fatal("expected keep chunk to remain on server")
		}
		if got := index.DeletedChunks[keepChunk]; got <= now {
			t.Fatalf("expected keep chunk deadline to be extended, got %d", got)
		}
	})
}

func TestBackupDownloadRestoreVerifyAndFsckFlows(t *testing.T) {
	t.Run("upload payload and upload files", func(t *testing.T) {
		server := newFakeCOSServer(t)
		config := Config{
			Threads: 2,
			COS:     testCOSConfig(server),
		}
		client := server.newClient(t, config.COS)
		ctx := context.Background()

		existingFile := filepath.Join(t.TempDir(), "existing.txt")
		if err := os.WriteFile(existingFile, []byte("new"), 0644); err != nil {
			t.Fatal(err)
		}
		server.setObject("data/existing", []byte("old"), nil)
		if err := uploadPayload(ctx, client, config, UploadCTX{
			localPath:  existingFile,
			remotePath: "existing",
			size:       3,
		}); err != nil {
			t.Fatalf("uploadPayload existing: %v", err)
		}
		if string(server.objectBody("data/existing")) != "old" {
			t.Fatalf("existing object should not be overwritten, got %q", server.objectBody("data/existing"))
		}

		missingLocal := filepath.Join(t.TempDir(), "missing.txt")
		if err := uploadPayload(ctx, client, config, UploadCTX{
			localPath:  missingLocal,
			remotePath: "missing",
			size:       0,
		}); err != nil {
			t.Fatalf("uploadPayload missing local should be ignored, got %v", err)
		}

		newFile := filepath.Join(t.TempDir(), "upload.txt")
		if err := os.WriteFile(newFile, []byte("fresh"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := uploadPayload(ctx, client, config, UploadCTX{
			localPath:  newFile,
			remotePath: "new",
			size:       5,
		}); err != nil {
			t.Fatalf("uploadPayload upload: %v", err)
		}
		if string(server.objectBody("data/new")) != "fresh" {
			t.Fatalf("unexpected uploaded object body: %q", server.objectBody("data/new"))
		}

		localIndex := NewIndex(config, client)
		remoteIndex := NewIndex(config, client)

		file1 := filepath.Join(t.TempDir(), "file1.txt")
		file2 := filepath.Join(t.TempDir(), "file2.txt")
		if err := os.WriteFile(file1, []byte("11111"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file2, []byte("22222"), 0644); err != nil {
			t.Fatal(err)
		}
		chunk1 := ChunkKey{size: 5, hash: HashType{0x11}}
		chunk2 := ChunkKey{size: 5, hash: HashType{0x22}}
		oldChunk := ChunkKey{size: 4, hash: HashType{0x33}}
		localIndex.Entries.Files[file1] = &FileInfo{Size: 5, Hash: chunk1.hash}
		localIndex.Entries.Files[file2] = &FileInfo{Size: 5, Hash: chunk2.hash}
		remoteIndex.Entries.Files["/remote-keep"] = &FileInfo{Size: 5, Hash: chunk2.hash}
		remoteIndex.Entries.Files["/remote-old"] = &FileInfo{Size: 4, Hash: oldChunk.hash}
		remoteIndex.DeletedChunks[oldChunk] = time.Now().AddDate(4, 0, 0).Unix()

		if err := uploadFiles(ctx, client, config, localIndex, remoteIndex); err != nil {
			t.Fatalf("uploadFiles: %v", err)
		}
		if !server.objectExists(path.Join(config.COS.ChunkPrefix, chunkPath(chunk1))) {
			t.Fatalf("expected uploaded chunk %s", chunkPath(chunk1))
		}
		if server.objectExists(path.Join(config.COS.ChunkPrefix, chunkPath(chunk2))) {
			t.Fatalf("expected existing remote chunk to be skipped")
		}
		if got := localIndex.DeletedChunks[oldChunk]; got <= time.Now().Unix() {
			t.Fatalf("expected old chunk delete deadline in future, got %d", got)
		}
	})

	t.Run("backup files", func(t *testing.T) {
		server := newFakeCOSServer(t)
		baseDir := t.TempDir()
		dataDir := filepath.Join(baseDir, "data")
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			t.Fatal(err)
		}
		filePath := filepath.Join(dataDir, "backup.txt")
		if err := os.WriteFile(filePath, []byte("backup-data"), 0644); err != nil {
			t.Fatal(err)
		}

		config := Config{
			BasePath:   baseDir,
			FilePaths:  []string{"/data"},
			Threads:    2,
			WorkingDir: t.TempDir(),
			Index:      "meta.json",
			COS:        testCOSConfig(server),
		}
		if err := backupFiles(context.Background(), config); err != nil {
			t.Fatalf("backupFiles: %v", err)
		}

		keys := server.objectKeys()
		if len(keysWithPrefix(keys, "meta.json.")) == 0 {
			t.Fatalf("expected uploaded remote index, got keys %v", keys)
		}
		if len(keysWithPrefix(keys, "data/")) == 0 {
			t.Fatalf("expected uploaded chunk objects, got keys %v", keys)
		}
	})

	t.Run("download chunks and files", func(t *testing.T) {
		server := newFakeCOSServer(t)
		config := Config{
			TargetDir:  t.TempDir(),
			WorkingDir: t.TempDir(),
			Index:      "meta.json",
			COS:        testCOSConfig(server),
		}
		client := server.newClient(t, config.COS)

		existingBody := []byte("existing-data")
		existingHash, err := chunkHash(writeTempFile(t, existingBody), int64(len(existingBody)))
		if err != nil {
			t.Fatal(err)
		}
		existingKey := ChunkKey{size: int64(len(existingBody)), hash: existingHash}
		existingChunkPath := chunkPath(existingKey)
		existingLocalDir := filepath.Join(config.TargetDir, "chunks", existingChunkPath[len(existingChunkPath)-2:])
		if err := os.MkdirAll(existingLocalDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(existingLocalDir, existingChunkPath), existingBody, 0644); err != nil {
			t.Fatal(err)
		}

		remoteBody := []byte("remote-data")
		remoteHash, err := chunkHash(writeTempFile(t, remoteBody), int64(len(remoteBody)))
		if err != nil {
			t.Fatal(err)
		}
		remoteKey := ChunkKey{size: int64(len(remoteBody)), hash: remoteHash}
		remoteChunkPath := chunkPath(remoteKey)
		server.setObject(path.Join(config.COS.ChunkPrefix, remoteChunkPath), remoteBody, nil)
		server.addFailure(http.MethodGet, path.Join(config.COS.ChunkPrefix, remoteChunkPath), fakeResponseSpec{
			status:  http.StatusInternalServerError,
			code:    "InternalError",
			message: "retry me",
		})

		if err := downloadChunk(context.Background(), client, config, ChunksMap{
			existingKey: false,
			remoteKey:   false,
		}); err != nil {
			t.Fatalf("downloadChunk: %v", err)
		}
		if server.requestCount(http.MethodGet, path.Join(config.COS.ChunkPrefix, existingChunkPath)) != 0 {
			t.Fatal("expected existing valid chunk to skip download")
		}
		if server.requestCount(http.MethodGet, path.Join(config.COS.ChunkPrefix, remoteChunkPath)) != 2 {
			t.Fatalf("expected retried download count 2, got %d", server.requestCount(http.MethodGet, path.Join(config.COS.ChunkPrefix, remoteChunkPath)))
		}
		if got, err := os.ReadFile(filepath.Join(config.TargetDir, "chunks", remoteChunkPath[len(remoteChunkPath)-2:], remoteChunkPath)); err != nil || string(got) != string(remoteBody) {
			t.Fatalf("unexpected downloaded chunk %q err=%v", got, err)
		}

		retryServer := newFakeCOSServer(t)
		retryConfig := Config{TargetDir: t.TempDir(), COS: testCOSConfig(retryServer)}
		retryClient := retryServer.newClient(t, retryConfig.COS)
		retryKey := ChunkKey{size: 1, hash: HashType{0x44}}
		retryPath := path.Join(retryConfig.COS.ChunkPrefix, chunkPath(retryKey))
		for i := 0; i < maxDownloadRetries; i++ {
			retryServer.addFailure(http.MethodGet, retryPath, fakeResponseSpec{
				status:  http.StatusInternalServerError,
				code:    "InternalError",
				message: "still failing",
			})
		}
		if err := downloadChunk(context.Background(), retryClient, retryConfig, ChunksMap{retryKey: false}); !errors.Is(err, ErrMaxRetries) {
			t.Fatalf("expected ErrMaxRetries, got %v", err)
		}

		index := NewIndex(config, client)
		index.Entries.Files["/downloaded.txt"] = &FileInfo{Size: remoteKey.size, Hash: remoteKey.hash}
		indexBytes := mustMarshalIndex(t, index)
		server.setObject("meta.json.1", indexBytes, http.Header{
			"X-Cos-Meta-Hash": []string{sha1HexBytes(indexBytes)},
		})
		if err := downloadFiles(context.Background(), config); err != nil {
			t.Fatalf("downloadFiles: %v", err)
		}
		if _, err := os.Stat(filepath.Join(config.TargetDir, "meta.json.remote")); err != nil {
			t.Fatalf("expected downloaded remote index file, got %v", err)
		}

		t.Run("invalid url and missing remote index", func(t *testing.T) {
			if err := downloadFiles(context.Background(), Config{COS: COSConfig{URL: "://bad"}}); !errors.Is(err, ErrConfigInvalid) {
				t.Fatalf("expected ErrConfigInvalid, got %v", err)
			}

			emptyServer := newFakeCOSServer(t)
			err := downloadFiles(context.Background(), Config{
				TargetDir:  t.TempDir(),
				WorkingDir: t.TempDir(),
				Index:      "meta.json",
				COS:        testCOSConfig(emptyServer),
			})
			if err == nil || !strings.Contains(err.Error(), "download index") {
				t.Fatalf("expected wrapped download index error, got %v", err)
			}
		})

		t.Run("persist and scan errors", func(t *testing.T) {
			server := newFakeCOSServer(t)
			workDir := t.TempDir()
			targetBase := t.TempDir()
			targetAsFile := filepath.Join(targetBase, "target-file")
			if err := os.WriteFile(targetAsFile, []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
			cfg := Config{
				TargetDir:  targetAsFile,
				WorkingDir: workDir,
				Index:      "meta.json",
				COS:        testCOSConfig(server),
			}
			index := NewIndex(cfg, server.newClient(t, cfg.COS))
			indexBytes := mustMarshalIndex(t, index)
			server.setObject("meta.json.1", indexBytes, http.Header{
				"X-Cos-Meta-Hash": []string{sha1HexBytes(indexBytes)},
			})
			if err := downloadFiles(context.Background(), cfg); err == nil {
				t.Fatal("expected persist downloaded index error")
			}

			scanFailServer := newFakeCOSServer(t)
			cfg = Config{
				TargetDir:  t.TempDir(),
				WorkingDir: t.TempDir(),
				Index:      "meta.json",
				COS:        testCOSConfig(scanFailServer),
			}
			index = NewIndex(cfg, scanFailServer.newClient(t, cfg.COS))
			indexBytes = mustMarshalIndex(t, index)
			scanFailServer.setObject("meta.json.1", indexBytes, http.Header{
				"X-Cos-Meta-Hash": []string{sha1HexBytes(indexBytes)},
			})
			scanFailServer.setPersistentFailure(http.MethodGet, "?max-keys=1000&prefix=data%2F", fakeResponseSpec{
				status:  http.StatusInternalServerError,
				code:    "InternalError",
				message: "list fail",
			})
			if err := downloadFiles(context.Background(), cfg); err == nil {
				t.Fatal("expected scanRemoteChunksMap error")
			}
		})
	})

	t.Run("restore flows", func(t *testing.T) {
		if err := classifyRestoreError(nil); err != nil {
			t.Fatalf("expected nil restore error, got %v", err)
		}
		rawErr := errors.New("raw")
		if got := classifyRestoreError(rawErr); !errors.Is(got, rawErr) {
			t.Fatalf("expected generic error passthrough, got %v", got)
		}

		server := newFakeCOSServer(t)
		config := Config{
			RestoreQPS: 1000,
			COS:        testCOSConfig(server),
		}
		client := server.newClient(t, config.COS)

		successChunk := ChunkKey{size: 1, hash: HashType{0x51}}
		pendingChunk := ChunkKey{size: 1, hash: HashType{0x52}}
		missingChunk := ChunkKey{size: 1, hash: HashType{0x53}}
		failChunk := ChunkKey{size: 1, hash: HashType{0x54}}
		server.addFailure(http.MethodPost, path.Join(config.COS.ChunkPrefix, chunkPath(pendingChunk))+"?restore", fakeResponseSpec{
			status:  http.StatusConflict,
			code:    "RestoreAlreadyInProgress",
			message: "pending",
		})
		server.addFailure(http.MethodPost, path.Join(config.COS.ChunkPrefix, chunkPath(missingChunk))+"?restore", fakeResponseSpec{
			status:  http.StatusNotFound,
			code:    "NoSuchKey",
			message: "missing",
		})
		server.setPersistentFailure(http.MethodPost, path.Join(config.COS.ChunkPrefix, chunkPath(failChunk))+"?restore", fakeResponseSpec{
			status:  http.StatusInternalServerError,
			code:    "InternalError",
			message: "boom",
		})

		err := restoreChunk(context.Background(), client, config, ChunksMap{
			successChunk: false,
			pendingChunk: false,
			missingChunk: false,
			failChunk:    false,
		})
		if err == nil {
			t.Fatal("expected restoreChunk error for internal failure")
		}

		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		if err := restoreChunk(cancelled, client, config, ChunksMap{successChunk: false}); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}

		restoreServer := newFakeCOSServer(t)
		restoreConfig := Config{RestoreQPS: 1000, COS: testCOSConfig(restoreServer)}
		restoreClient := restoreServer.newClient(t, restoreConfig.COS)
		restoreKey := ChunkKey{size: 2, hash: HashType{0x60}}
		restoreServer.setObject(path.Join(restoreConfig.COS.ChunkPrefix, chunkPath(restoreKey)), []byte("x"), nil)
		if err := restoreFiles(context.Background(), Config{RestoreQPS: 1000, COS: restoreConfig.COS}); err != nil {
			t.Fatalf("restoreFiles: %v", err)
		}
		_ = restoreClient
	})

	t.Run("verify and fsck", func(t *testing.T) {
		server := newFakeCOSServer(t)
		baseDir := t.TempDir()
		workDir := t.TempDir()
		dataDir := filepath.Join(baseDir, "data")
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			t.Fatal(err)
		}
		filePath := filepath.Join(dataDir, "verify.txt")
		body := []byte("verify-data")
		if err := os.WriteFile(filePath, body, 0644); err != nil {
			t.Fatal(err)
		}
		modTime := time.Unix(1700000300, 0)
		if err := os.Chtimes(filePath, modTime, modTime); err != nil {
			t.Fatal(err)
		}
		hash, err := chunkHash(filePath, int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		chunk := ChunkKey{size: int64(len(body)), hash: hash}

		config := Config{
			BasePath:   baseDir,
			FilePaths:  []string{"/data"},
			WorkingDir: workDir,
			Index:      "meta.json",
			COS:        testCOSConfig(server),
		}

		verifyIndex := NewIndex(config, server.newClient(t, config.COS))
		verifyIndex.Entries.Dirs[dataDir] = &DirInfo{Mode: 0755}
		verifyIndex.Entries.Files[filePath] = &FileInfo{
			Size:    int64(len(body)),
			Mode:    0644,
			ModTime: modTime.Unix(),
			Hash:    hash,
		}
		indexBytes := mustMarshalIndex(t, verifyIndex)
		server.setObject("meta.json.1", indexBytes, http.Header{
			"X-Cos-Meta-Hash": []string{sha1HexBytes(indexBytes)},
		})
		server.setObject(path.Join(config.COS.ChunkPrefix, chunkPath(chunk)), body, nil)

		if err := verifyFiles(context.Background(), config); err != nil {
			t.Fatalf("verifyFiles: %v", err)
		}

		fsckServer := newFakeCOSServer(t)
		fsckConfig := Config{
			WorkingDir: t.TempDir(),
			Index:      "meta.json",
			COS:        testCOSConfig(fsckServer),
		}
		fsckIndex := NewIndex(fsckConfig, fsckServer.newClient(t, fsckConfig.COS))
		missingChunk := ChunkKey{size: 9, hash: HashType{0x70}}
		strayChunk := ChunkKey{size: 10, hash: HashType{0x71}}
		fsckIndex.Entries.Files["/missing.txt"] = &FileInfo{Size: missingChunk.size, Hash: missingChunk.hash}
		fsckBytes := mustMarshalIndex(t, fsckIndex)
		fsckServer.setObject("meta.json.1", fsckBytes, http.Header{
			"X-Cos-Meta-Hash": []string{sha1HexBytes(fsckBytes)},
		})
		fsckServer.setObject(path.Join(fsckConfig.COS.ChunkPrefix, chunkPath(strayChunk)), []byte("stray"), nil)

		err = fsckRemote(context.Background(), fsckConfig)
		if !errors.Is(err, ErrChunkLost) {
			t.Fatalf("expected ErrChunkLost, got %v", err)
		}

		updated := NewIndex(fsckConfig, nil)
		if err := updated.Load(filepath.Join(fsckConfig.WorkingDir, "meta.json")); err != nil {
			t.Fatalf("load updated fsck index: %v", err)
		}
		if len(updated.Entries.Files) != 0 {
			t.Fatalf("expected missing file removed from fsck index, got %#v", updated.Entries.Files)
		}
		if _, ok := updated.DeletedChunks[strayChunk]; !ok {
			t.Fatalf("expected stray chunk marked deleted, got %#v", updated.DeletedChunks)
		}
	})
}

func writeTempFile(t *testing.T, body []byte) string {
	t.Helper()
	fp := filepath.Join(t.TempDir(), "tmp.bin")
	if err := os.WriteFile(fp, body, 0644); err != nil {
		t.Fatal(err)
	}
	return fp
}
