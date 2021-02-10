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

func xunleiHash(path string, size int64) string {
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

func chunkHash(size int64, hash string) string {
	return strconv.FormatInt(size, 10) + "-" + hash
}
