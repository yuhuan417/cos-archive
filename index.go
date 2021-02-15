package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
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

// Index struct
type Index struct {
	Files         FileInfoMap
	DeletedChunks ChunkDeleteMarkMap
}

func metaHash(path string) string {
	h := sha1.New()
	f, _ := os.Open(path)
	defer f.Close()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

func loadIndex(path string) Index {
	index := Index{}
	index.Files = make(FileInfoMap)
	index.DeletedChunks = make(ChunkDeleteMarkMap)
	jf, err := os.Open(path)
	if err != nil {
		return index
	}
	defer jf.Close()

	jsonParser := json.NewDecoder(jf)
	if err = jsonParser.Decode(&index); err != nil {
		log.Println("Load index error: ", path, err)
		return index
	}

	return index
}

func downloadRemoteIndex(config Config, path string) bool {
	c := NewCOS(config.COS)
	log.Println("Download remote index to ", path)
	err := c.DownloadFile(config.Index, path)
	if err != nil {
		log.Println("Download index error:", err)
		return false
	}
	return true
}

func getRemoteMetaHash(config Config) string {
	c := NewCOS(config.COS)
	h := c.GetHeader(config.Index)

	if h == nil {
		return ""
	}
	// log.Println("remote index header:", h.Get("Last-Modified"))
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
		c := NewCOS(config.COS)
		err := c.UploadFile(config.Index, tmpfp, "STANDARD", hh)
		if err != nil {
			log.Fatalln("Upload index fail:", err)
		}
	}
	fp := path.Join(config.WorkingDir, config.Index)
	os.Rename(tmpfp, fp)
}

func getRemoteIndex(config Config) Index {
	log.Println("Loading remote index")
	oldPath := path.Join(config.WorkingDir, config.Index)
	mh := metaHash(oldPath)
	rh := getRemoteMetaHash(config)
	riPath := ""

	if mh == rh {
		log.Println("Hash match, using local old index.")
		riPath = oldPath
	} else {
		riPath = path.Join(config.WorkingDir, config.Index+".remote")
		downloadRemoteIndex(config, riPath)
		log.Println("Download remote index to: ", riPath)
	}
	ri := loadIndex(riPath)
	return ri
}
