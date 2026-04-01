package main

import (
	"crypto/sha1"
	"os"
	"path/filepath"
	"testing"
)

func TestChunkKeyRoundTrip(t *testing.T) {
	original := ChunkKey{
		size: 12345,
		hash: HashType{0x01, 0x02, 0x03, 0x04, 0x05},
	}
	text, err := original.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var decoded ChunkKey
	if err := decoded.UnmarshalText(text); err != nil {
		t.Fatal(err)
	}
	if original != decoded {
		t.Fatalf("roundtrip failed: %#v != %#v", original, decoded)
	}
}

func TestChunkHashSmallFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "small.bin")
	data := []byte("hello world")
	if err := os.WriteFile(fp, data, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := chunkHash(fp, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(data)
	var want HashType
	copy(want[:], sum[:])
	if got != want {
		t.Fatalf("unexpected hash: got=%x want=%x", got, want)
	}
}

func TestChunkHashLargeFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "large.bin")
	data := make([]byte, 0x20000)
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := os.WriteFile(fp, data, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := chunkHash(fp, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

	h := sha1.New()
	_, _ = h.Write(data[:0x5000])
	mid := len(data) / 3
	_, _ = h.Write(data[mid : mid+0x5000])
	_, _ = h.Write(data[len(data)-0x5000:])
	sum := h.Sum(nil)
	var want HashType
	copy(want[:], sum)
	if got != want {
		t.Fatalf("unexpected hash: got=%x want=%x", got, want)
	}
}

func TestChunkHashMissingFile(t *testing.T) {
	if _, err := chunkHash(filepath.Join(t.TempDir(), "missing.bin"), 1); err == nil {
		t.Fatal("expected missing file error")
	}
}
