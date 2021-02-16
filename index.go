package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// HashType ...
type HashType string

// FileInfo struct
type FileInfo struct {
	Size    int64
	Mode    os.FileMode
	ModTime int64
	IsDir   bool
	LinkTo  string
	Hash    HashType
}

// UnmarshalText for json
func (h *HashType) UnmarshalText(text []byte) error {
	b, _ := hex.DecodeString(string(text))
	*h = HashType(b)

	return nil
}

// MarshalText for json
func (h HashType) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString([]byte(h))), nil
}

// FileInfoMap struct
type FileInfoMap = map[string]*FileInfo

// ChunkDeleteMarkMap struct
type ChunkDeleteMarkMap = map[ChunkKey]int64

// Index struct
type Index struct {
	Files         FileInfoMap
	DeletedChunks ChunkDeleteMarkMap
	config        Config
}

// NewIndex ...
func NewIndex(config Config) *Index {
	index := Index{}
	index.Files = make(FileInfoMap)
	index.DeletedChunks = make(ChunkDeleteMarkMap)
	index.config = config
	return &index
}

func (index *Index) metaHash(path string) string {
	h := sha1.New()
	f, _ := os.Open(path)
	defer f.Close()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// Load ...
func (index *Index) Load(path string) error {
	jf, err := os.Open(path)
	if err != nil {
		log.Println("Load index error: ", path, err)
		return err
	}
	defer jf.Close()

	jsonParser := json.NewDecoder(jf)
	if err = jsonParser.Decode(&index); err != nil {
		log.Println("Load index error: ", path, err)
		return err
	}

	return err
}

func (index *Index) downloadRemote(remote string, path string) error {
	c := NewCOS(index.config.COS)
	log.Println("Download remote index :", remote, path)
	err := c.DownloadFile(remote, path)
	if err != nil {
		log.Println("Download index error:", err)
	}
	return err
}

func (index *Index) getRemoteHash(p string) string {
	c := NewCOS(index.config.COS)
	h := c.GetHeader(p)

	if h == nil {
		return ""
	}
	return h.Get("x-cos-meta-hash")
}

// UploadRemote ...
func (index *Index) UploadRemote() {
	log.Println("Uploading index")
	content, _ := json.MarshalIndent(index, "", "  ")
	tmpfp := path.Join(index.config.WorkingDir, index.config.Index+".new")
	ioutil.WriteFile(tmpfp, content, 0666)
	lh := index.metaHash(tmpfp)
	hh := http.Header{}
	hh.Add("x-cos-meta-hash", lh)
	c := NewCOS(index.config.COS)
	err := c.UploadFile(index.config.Index+"."+strconv.FormatInt(time.Now().Unix(), 10), tmpfp, "STANDARD", hh)
	if err != nil {
		log.Fatalln("Upload index fail:", err)
	}
	fp := path.Join(index.config.WorkingDir, index.config.Index)
	os.Rename(tmpfp, fp)
}

// LoadRemote ...
func (index *Index) LoadRemote() error {
	log.Println("Loading remote index")
	oldPath := path.Join(index.config.WorkingDir, index.config.Index)
	mh := index.metaHash(oldPath)

	c := NewCOS(index.config.COS)

	indexList := []string{}
	c.ScanFiles(index.config.Index, func(obj cos.Object) {
		indexList = append(indexList, obj.Key)
	})
	sort.Slice(indexList, func(i, j int) bool {
		p := index.config.Index + "."
		numA, _ := strconv.ParseInt(strings.TrimPrefix(indexList[i], p), 10, 64)
		numB, _ := strconv.ParseInt(strings.TrimPrefix(indexList[j], p), 10, 64)
		return numB < numA
	})
	if len(indexList) > 30 {
		for i := 30; i < len(indexList); i++ {
			c.DeleteFile(indexList[i])
		}
	}

	latestIndex := ""
	if len(indexList) > 0 {
		latestIndex = indexList[0]
		log.Println("Using latest index: ", latestIndex)
	} else {
		return errors.New("No remote index")
	}
	rh := index.getRemoteHash(latestIndex)
	riPath := ""

	if mh == rh {
		log.Println("Hash match, using local old index.")
		riPath = oldPath
	} else {
		riPath = path.Join(index.config.WorkingDir, index.config.Index+".remote")
		os.Remove(riPath)
		err := index.downloadRemote(latestIndex, riPath)
		if err != nil {
			log.Println("Can't download remote index", err)
		}
		log.Println("Download remote index to: ", riPath)
	}
	return index.Load(riPath)
}

// DeleteOutdatedChunks ...
func (index *Index) DeleteOutdatedChunks() {
	now := time.Now().Unix()
	c := NewCOS(index.config.COS)
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

func (index *Index) scanSingleFile(path string, info os.FileInfo, err error) error {
	// log.Println("Processsing: ", path)
	if err != nil {
		log.Println("On error: ", err, " Skip: ", path)
		return err
	}
	if info.IsDir() {
		for _, skip := range index.config.SkipList {
			if info.Name() == skip {
				// log.Println("In skiplist: ", skip, " Skip: ", path)
				return filepath.SkipDir
			}
		}
	}
	if !info.IsDir() && !info.Mode().IsRegular() && (info.Mode()&os.ModeSymlink == 0) {
		log.Println("Skip non-regular file: ", path)
		return nil
	}
	link := ""
	if info.Mode()&os.ModeSymlink != 0 {
		link, _ = os.Readlink(path)
	}
	f := FileInfo{
		Mode:    info.Mode(),
		ModTime: info.ModTime().Unix(),
		IsDir:   info.IsDir(),
		LinkTo:  link,
	}
	if info.Mode().IsRegular() {
		f.Size = info.Size()
	}
	index.Files[path] = &f
	return nil
}

// GenerateLocal ...
func (index *Index) GenerateLocal(remoteIndex *Index) {
	log.Println("Generating local index")
	for _, filePath := range index.config.FilePaths {
		log.Println("Walk ", filePath)
		err := filepath.Walk(filePath, func(path string, info os.FileInfo, err error) error {
			return index.scanSingleFile(path, info, err)
		})

		if err != nil {
			continue
		}
	}
	for fp, fi := range index.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}
		var h HashType = ""
		if remoteFileInfo, ok := remoteIndex.Files[fp]; ok {
			if remoteFileInfo.Size == fi.Size && remoteFileInfo.ModTime == fi.ModTime && remoteFileInfo.Hash != "" {
				h = remoteFileInfo.Hash
				// log.Println("Use cached hash ", h, " for ", fp)
			}
		}
		if h == "" {
			h = chunkHash(fp, fi.Size)
			// log.Println("Caculated hash ", h, " for ", fp)
		}
		fi.Hash = h
	}
	return
}
