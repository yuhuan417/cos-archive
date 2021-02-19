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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/karrick/godirwalk"
	"github.com/tencentyun/cos-go-sdk-v5"
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

// UnmarshalText from json
func (h *HashType) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		h = &HashType{}
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

// Index struct
type Index struct {
	Entries       DirEnt
	DeletedChunks ChunkDeleteMarkMap
	config        Config
}

// NewIndex ...
func NewIndex(config Config) *Index {
	index := Index{}
	index.Entries.Files = make(FileInfoMap)
	index.Entries.Dirs = make(DirInfoMap)
	index.Entries.Links = make(LinkInfoMap)
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
	w, err := ioutil.TempFile(index.config.WorkingDir, index.config.Index+".*.new")
	if err != nil {
		log.Fatal("Write index error:", err)
	}
	je := json.NewEncoder(w)
	je.SetIndent("", "  ")
	err = je.Encode(index)
	if err != nil {
		log.Fatal("Encode index json error:", err)
	}
	tmpfp := w.Name()
	w.Close()

	lh := index.metaHash(tmpfp)
	latestIndex, err := index.getLatestIndex()
	if err != nil {
		log.Println("Not found remote index.")
	}
	rh := index.getRemoteHash(latestIndex)
	if lh != rh {
		hh := http.Header{}
		hh.Add("x-cos-meta-hash", lh)
		c := NewCOS(index.config.COS)
		err := c.UploadFile(index.config.Index+"."+strconv.FormatInt(time.Now().Unix(), 10), tmpfp, "STANDARD", hh)
		if err != nil {
			log.Fatalln("Upload index fail:", err)
		}
	} else {
		log.Println("Hash matched, old index is good enough")
	}
	fp := path.Join(index.config.WorkingDir, index.config.Index)
	os.Rename(tmpfp, fp)
}

func (index *Index) getLatestIndex() (string, error) {
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
	var err error
	err = nil
	if len(indexList) > 0 {
		latestIndex = indexList[0]
		log.Println("Using latest index: ", latestIndex)
	} else {
		err = errors.New("No remote index")
	}
	return latestIndex, err
}

// LoadRemote ...
func (index *Index) LoadRemote() error {
	log.Println("Loading remote index")
	oldPath := path.Join(index.config.WorkingDir, index.config.Index)
	mh := index.metaHash(oldPath)

	latestIndex, err := index.getLatestIndex()
	if err != nil {
		log.Println("Not found remote index.")
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

func (index *Index) scanSingleFile(path string, de *godirwalk.Dirent) error {
	// log.Println("Processsing: ", path)
	if de.IsDir() {
		for _, skip := range index.config.SkipList {
			if de.Name() == skip {
				// log.Println("In skiplist: ", skip, " Skip: ", path)
				return godirwalk.SkipThis
			}
		}
		d := DirInfo{
			Mode: de.ModeType(),
		}
		index.Entries.Dirs[path] = &d
		return nil
	}
	if de.IsSymlink() {
		link, _ := os.Readlink(path)
		l := LinkInfo{
			LinkTo: link,
		}
		index.Entries.Links[path] = &l
		return nil
	}
	if de.IsRegular() {
		fi, err := os.Stat(path)
		if err != nil {
			log.Println("Can't stat file:", path)
			return err
		}
		f := FileInfo{
			Mode:    fi.Mode(),
			ModTime: fi.ModTime().Unix(),
			Size:    fi.Size(),
		}
		index.Entries.Files[path] = &f
		return nil
	}
	log.Println("Skip non-regular file: ", path)
	return nil
}

// GenerateLocal ...
func (index *Index) GenerateLocal(remoteIndex *Index) {
	log.Println("Generating local index")
	for _, filePath := range index.config.FilePaths {
		log.Println("Walk ", filePath)

		err := godirwalk.Walk(filePath, &godirwalk.Options{
			Callback: func(path string, de *godirwalk.Dirent) error {
				return index.scanSingleFile(path, de)
			},
			Unsorted: true, // (optional) set true for faster yet non-deterministic enumeration (see godoc)
		})

		if err != nil {
			continue
		}
	}
	for fp, fi := range index.Entries.Files {
		var h HashType
		cachedHash := false
		if remoteFileInfo, ok := remoteIndex.Entries.Files[fp]; ok {
			if remoteFileInfo.Size == fi.Size && remoteFileInfo.ModTime == fi.ModTime {
				h = remoteFileInfo.Hash
				cachedHash = true
				// log.Println("Use cached hash ", h, " for ", fp)
			}
		}
		if !cachedHash {
			h = chunkHash(fp, fi.Size)
			// log.Println("Caculated hash ", h, " for ", fp)
		}
		fi.Hash = h
	}
	return
}
