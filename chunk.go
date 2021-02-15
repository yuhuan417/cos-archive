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
	"time"
)

// ChunkKey in memory
type ChunkKey struct {
	size int64
	hash string
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

// ChunkDeleteMarkMap struct
type ChunkDeleteMarkMap = map[ChunkKey]int64

func deleteOutdatedChunks(config Config, index *Index) {
	now := time.Now().Unix()
	c := NewCOS(config.COS)
	cnt := 0
	for fp, t := range index.DeletedChunks {
		if now > t {
			// log.Println("Deleting remote chunk: ", fp)
			err := c.DeleteFile(chunkPath(fp))
			if err != nil {
				continue
			}
			delete(index.DeletedChunks, fp)
			cnt = cnt + 1
		}
	}
	log.Println("Deleting outdated remote chunk: ", cnt)
}

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
	c.ScanFiles(func(key string) {
		p := filepath.Clean(config.COS.ChunkPrefix) + "/"
		k := parseChunkKeyFromString(strings.TrimPrefix(key, p))
		cm[k] = false
	})
	return cm
}

// func chunkPath(size int64, hash string) string {
// 	return strconv.FormatInt(size, 10) + "-" + hash
// }

func chunkPath(k ChunkKey) string {
	return strconv.FormatInt(k.size, 10) + "-" + k.hash
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
		k.hash = c[1]
	}
	return k
}

func chunkHash(path string, size int64) string {
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
	return hex.EncodeToString(h.Sum(nil))
}
