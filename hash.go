package main

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"os"
	"strconv"
)

func metaHash(path string) string {
	h := sha1.New()
	f, _ := os.Open(path)
	defer f.Close()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

func xunleiHash(path string, info *FileInfo) string {
	h := sha1.New()
	size := info.Size
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

func chunkHash(fi *FileInfo) string {
	return strconv.FormatInt(fi.Size, 10) + "-" + fi.Hash
}
