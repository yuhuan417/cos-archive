package main

import (
	"bytes"
	"encoding/hex"
	"os"
	"strconv"
)

// HashType ...
type HashType [20]byte

// DirInfo ...
type DirInfo struct {
	Mode os.FileMode
}

// LinkInfo ...
type LinkInfo struct {
	LinkTo string
}

// FileInfo struct
type FileInfo struct {
	Size    int64
	Mode    os.FileMode
	ModTime int64
	Hash    HashType
}

// FileInfoMap struct
type FileInfoMap = map[string]*FileInfo

// DirInfoMap struct
type DirInfoMap = map[string]*DirInfo

// LinkInfoMap struct
type LinkInfoMap = map[string]*LinkInfo

// ChunkDeleteMarkMap struct
type ChunkDeleteMarkMap = map[ChunkKey]int64

// DirEnt ...
type DirEnt struct {
	Files FileInfoMap
	Dirs  DirInfoMap
	Links LinkInfoMap
}

// ChunkKey in memory
type ChunkKey struct {
	size int64
	hash HashType // sha1
}

// ChunksMap struct
type ChunksMap = map[ChunkKey]bool

// UnmarshalText from json
func (h *HashType) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*h = HashType{}
		return nil
	}
	th := h[:]
	_, err := hex.Decode(th, text)
	return err
}

// MarshalText to json
func (h HashType) MarshalText() ([]byte, error) {
	empty := HashType{}
	if h == empty {
		return make([]byte, 0), nil
	}
	b := make([]byte, hex.EncodedLen(len(h)))
	hex.Encode(b, h[:])
	return b, nil
}

func parseInt64FromBytes(b []byte) int64 {
	r := int64(0)
	for _, i := range b {
		r = r*10 + int64(i-'0')
	}
	return r
}

// UnmarshalText decode ChunkKey from json
func (k *ChunkKey) UnmarshalText(text []byte) error {
	*k = ChunkKey{}
	c := bytes.Split(text, []byte("-"))
	if len(c) == 2 {
		k.size = parseInt64FromBytes(c[0])
		err := k.hash.UnmarshalText(c[1])
		if err != nil {
			return err
		}
	}
	return nil
}

// MarshalText encode ChunkKey to json
func (k ChunkKey) MarshalText() ([]byte, error) {
	b := []byte("")
	b = strconv.AppendInt(b, k.size, 10)
	b = append(b, '-')
	h, _ := k.hash.MarshalText()
	b = append(b, h...)
	return b, nil
}

// NewDirEnt ...
func NewDirEnt() *DirEnt {
	d := DirEnt{}
	d.Files = make(FileInfoMap)
	d.Dirs = make(DirInfoMap)
	d.Links = make(LinkInfoMap)
	return &d
}
