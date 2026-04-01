package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestIndexSaveLoadRoundTrip(t *testing.T) {
	idx := NewIndex(Config{}, nil)
	hash := HashType{0xaa, 0xbb, 0xcc}
	idx.Entries.Files["/data/file.txt"] = &FileInfo{
		Size:    12,
		Mode:    0644,
		ModTime: 123,
		Hash:    hash,
	}
	idx.Entries.Dirs["/data"] = &DirInfo{Mode: 0755}
	idx.Entries.Links["/data/link"] = &LinkInfo{LinkTo: "/elsewhere"}
	idx.DeletedChunks[ChunkKey{size: 12, hash: hash}] = 456

	fp := filepath.Join(t.TempDir(), "meta.json")
	if err := idx.Save(fp); err != nil {
		t.Fatal(err)
	}

	loaded := NewIndex(Config{}, nil)
	if err := loaded.Load(fp); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(idx.Entries, loaded.Entries) {
		t.Fatalf("entries mismatch: %#v != %#v", idx.Entries, loaded.Entries)
	}
	if !reflect.DeepEqual(idx.DeletedChunks, loaded.DeletedChunks) {
		t.Fatalf("deleted chunks mismatch: %#v != %#v", idx.DeletedChunks, loaded.DeletedChunks)
	}
}
