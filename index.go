package main

import (
	"encoding/json"
	"os"
)

// FileInfo struct
type FileInfo struct {
	Size    int64
	Mode    os.FileMode
	ModTime int64
	IsDir   bool
	LinkTo  string
	Hash    string
}

// FileInfoMap struct
type FileInfoMap = map[string]*FileInfo

// ChunkDeleteMarkMap struct
type ChunkDeleteMarkMap = map[string]int64

// Index struct
type Index struct {
	Files  FileInfoMap
	Chunks ChunkDeleteMarkMap
}

func loadIndex(path string) Index {
	index := Index{}
	index.Files = make(FileInfoMap)
	index.Chunks = make(ChunkDeleteMarkMap)
	jf, err := os.Open(path)
	if err != nil {
		return index
	}
	defer jf.Close()

	jsonParser := json.NewDecoder(jf)
	if err = jsonParser.Decode(&index); err != nil {
		return index
	}

	return index
}
