package main

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"os"
)

func xunleiHash(path string, info FileInfo) string {
	h := sha1.New()
	size := info.Size
	f, _ := os.Open(path)
	defer f.Close()
	if size < 0xF000 {
		io.Copy(h, f)
	} else {
		b := make([]byte, 0x5000)
		io.ReadFull(f, b)
		h.Write(b)
		f.Seek(size/3, 0)
		io.ReadFull(f, b)
		h.Write(b)
		f.Seek(size-0x5000, 0)
		io.ReadFull(f, b)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}
