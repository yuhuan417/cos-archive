package main

import "testing"

func TestHashTypeEmptyAndInvalidInput(t *testing.T) {
	var h HashType
	if err := h.UnmarshalText(nil); err != nil {
		t.Fatalf("unexpected empty unmarshal error: %v", err)
	}
	if got, err := h.MarshalText(); err != nil || len(got) != 0 {
		t.Fatalf("unexpected empty marshal result: %q, %v", got, err)
	}

	if err := h.UnmarshalText([]byte("zz")); err == nil {
		t.Fatal("expected invalid hex error")
	}
}

func TestParseInt64FromBytesAndChunkKeyFallbacks(t *testing.T) {
	if got := parseInt64FromBytes([]byte("123456")); got != 123456 {
		t.Fatalf("unexpected parsed value: %d", got)
	}

	var key ChunkKey
	if err := key.UnmarshalText([]byte("badinput")); err != nil {
		t.Fatalf("unexpected error for invalid chunk key: %v", err)
	}
	if key != (ChunkKey{}) {
		t.Fatalf("expected zero chunk key, got %#v", key)
	}

	encoded, err := (ChunkKey{}).MarshalText()
	if err != nil {
		t.Fatalf("marshal zero chunk key: %v", err)
	}
	if string(encoded) != "0-" {
		t.Fatalf("unexpected zero chunk key encoding: %q", encoded)
	}
}

func TestNewDirEntInitializesMaps(t *testing.T) {
	de := NewDirEnt()
	de.Files["/file"] = &FileInfo{}
	de.Dirs["/dir"] = &DirInfo{}
	de.Links["/link"] = &LinkInfo{}

	if len(de.Files) != 1 || len(de.Dirs) != 1 || len(de.Links) != 1 {
		t.Fatalf("unexpected map lengths: %#v", de)
	}
}
