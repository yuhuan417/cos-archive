package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
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

func downloadRemoteIndex(config Config, path string) bool {
	log.Println("Download remote index to ", path)
	err := cosDownloadFile(config.COS, config.Index, path)
	if err != nil {
		log.Println("Download index error:", err)
		return false
	}
	return true
}

func getRemoteMetaHash(config Config) string {
	h := cosGetHeader(config.COS, config.Index)

	if h == nil {
		return ""
	}
	return h.Get("x-cos-meta-hash")
}

func uploadRemoteIndex(config Config, content []byte) {
	log.Println("Uploading index")

	rh := getRemoteMetaHash(config)
	tmpfp := path.Join(config.WorkingDir, config.Index+".new")
	ioutil.WriteFile(tmpfp, content, 0666)
	lh := metaHash(tmpfp)
	if rh != lh {
		hh := http.Header{}
		hh.Add("x-cos-meta-hash", lh)
		err := cosUploadFile(config.COS, config.Index, tmpfp, "STANDARD", hh)
		if err != nil {
			log.Fatalln("Upload index fail:", err)
		}
	}
	fp := path.Join(config.WorkingDir, config.Index)
	os.Rename(tmpfp, fp)
}
