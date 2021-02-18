package main

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// ChunkKey in memory
type ChunkKey struct {
	size int64
	hash HashType
}

// UnmarshalText for json
func (k *ChunkKey) UnmarshalText(text []byte) error {
	*k = parseChunkKeyFromString(string(text))

	return nil
}

// MarshalText for json
func (k ChunkKey) MarshalText() ([]byte, error) {
	return []byte(chunkPath(k)), nil
}

// ChunksMap struct
type ChunksMap = map[ChunkKey]bool

func buildChunksMap(index Index) ChunksMap {
	chunksMap := make(ChunksMap)
	for _, fi := range index.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}

		chunksMap[ChunkKey{fi.Size, fi.Hash}] = false
	}
	return chunksMap
}

func scanRemoteChunksMap(config Config) ChunksMap {
	log.Println("Scan remote chunks")
	cm := make(ChunksMap)
	c := NewCOS(config.COS)
	c.ScanFiles(config.COS.ChunkPrefix, func(obj cos.Object) {
		p := filepath.Clean(config.COS.ChunkPrefix) + "/"
		k := parseChunkKeyFromString(strings.TrimPrefix(obj.Key, p))
		cm[k] = false
	})
	return cm
}

func chunkPath(k ChunkKey) string {
	return strconv.FormatInt(k.size, 10) + "-" + hex.EncodeToString([]byte(k.hash))
}

func parseChunkKeyFromString(s string) ChunkKey {
	k := ChunkKey{}
	c := strings.Split(s, "-")
	if len(c) == 2 {
		size, err := strconv.ParseInt(c[0], 10, 64)
		if err != nil {
			size = 0
		}
		k.size = size
		h, _ := hex.DecodeString(c[1])
		k.hash = HashType(h)
	}
	return k
}

func chunkHash(path string, size int64) HashType {
	// xunlei hash
	h := sha1.New()
	f, _ := os.Open(path)
	defer f.Close()
	if size < 0xF000 {
		io.Copy(h, f)
	} else {
		io.CopyN(h, f, 0x5000)
		f.Seek(size/3, 0)
		io.CopyN(h, f, 0x5000)
		f.Seek(size-0x5000, 0)
		io.CopyN(h, f, 0x5000)
	}
	return HashType(h.Sum(nil))
}
