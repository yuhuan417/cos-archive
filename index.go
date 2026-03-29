package main

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
	"github.com/ugorji/go/codec"
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
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// Load ...
func (index *Index) Load(path string) error {
	jf, err := os.Open(path)
	if err != nil {
		slog.Debug("Load index error", "path", path, "error", err)
		return err
	}
	defer jf.Close()

	jh := codec.JsonHandle{}
	jh.ReaderBufferSize = 8192

	var dec *codec.Decoder = codec.NewDecoder(jf, &jh)
	err = dec.Decode(index)

	if err != nil {
		slog.Debug("Load index error", "path", path, "error", err)
		return err
	}

	return err
}

func (index *Index) downloadRemote(remote string, path string) error {
	c := NewCOS(index.config.COS)
	slog.Debug("Download remote index", "remote", remote, "path", path)
	err := c.DownloadFile(remote, path)
	if err != nil {
		slog.Debug("Download index error", "error", err)
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
	slog.Debug("Uploading index")
	w, err := os.CreateTemp(index.config.WorkingDir, index.config.Index+".*.new")
	if err != nil {
		Fatal("Write index error:", err)
	}
	defer w.Close()
	jh := codec.JsonHandle{Indent: 2}
	h := new(codec.JsonHandle)
	h.WriterBufferSize = 8192
	var enc *codec.Encoder = codec.NewEncoder(w, &jh)
	err = enc.Encode(index)
	if err != nil {
		Fatal("Encode index json error:", err)
	}
	tmpfp := w.Name()

	lh := index.metaHash(tmpfp)
	latestIndex, err := index.getLatestIndex()
	if err != nil {
		slog.Debug("Not found remote index.")
	}
	rh := index.getRemoteHash(latestIndex)
	if lh != rh {
		hh := http.Header{}
		hh.Add("x-cos-meta-hash", lh)
		c := NewCOS(index.config.COS)
		newName := index.config.Index + "." + strconv.FormatInt(time.Now().Unix(), 10)
		slog.Debug("Upload index to", "name", newName)
		err := c.UploadFile(newName, tmpfp, "STANDARD", hh)
		if err != nil {
			Fatal("Upload index fail", "error", err)
		}
	} else {
		slog.Debug("Hash matched, old index is good enough")
	}
	fp := path.Join(index.config.WorkingDir, index.config.Index)
	err = os.Rename(tmpfp, fp)
	if err != nil {
		Fatal("Index rename error: ", err)
	}
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
		slog.Debug("Using latest index", "index", latestIndex)
	} else {
		err = errors.New("NO REMOTE IDEX")
	}
	return latestIndex, err
}

// LoadRemote ...
func (index *Index) LoadRemote() error {
	slog.Debug("Loading remote index")
	oldPath := path.Join(index.config.WorkingDir, index.config.Index)
	mh := index.metaHash(oldPath)

	latestIndex, err := index.getLatestIndex()
	if err != nil {
		slog.Debug("Not found remote index.")
	}

	rh := index.getRemoteHash(latestIndex)
	riPath := ""

	if mh == rh {
		slog.Debug("Hash match, using local old index.")
		riPath = oldPath
	} else {
		riPath = path.Join(index.config.WorkingDir, index.config.Index+".remote")
		os.Remove(riPath)
		err := index.downloadRemote(latestIndex, riPath)
		if err != nil {
			slog.Debug("Can't download remote index", "error", err)
		}
		slog.Debug("Download remote index to", "path", riPath)
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
			var lmt int64 = 0
			h := c.GetHeader(path.Join(index.config.COS.ChunkPrefix, chunkPath(fp)))
			if h != nil {
				lm := h.Get("Last-Modified")
				layout := "Mon, 2 Jan 2006 15:04:05 MST"

				t, err := time.Parse(layout, lm)
				if err == nil {
					lmt = t.AddDate(0, 0, 180).Unix()
				}
			}
			if lmt > t {
				slog.Debug("Fix time", "path", fp, "from", t, "to", lmt)
				index.DeletedChunks[fp] = lmt
			}
			if now > lmt {
				slog.Debug("Deleting remote chunk", "path", fp)
				err := c.DeleteFile(path.Join(index.config.COS.ChunkPrefix, chunkPath(fp)))
				if err != nil {
					continue
				}
				delete(index.DeletedChunks, fp)
				cnt = cnt + 1
			}
		}
	}
	slog.Info("Deleting outdated remote chunk", "count", cnt)
}

func (index *Index) scanSingleFile(path string, de fs.DirEntry) error {
	// slog.Info("Processsing: ", path)
	if de.IsDir() {
		for _, skip := range index.config.SkipList {
			if de.Name() == skip {
				// slog.Info("In skiplist: ", skip, " Skip: ", path)
				return filepath.SkipDir
			}
		}
		fi, err := de.Info()
		if err != nil {
			slog.Debug("Can't stat dir", "path", path)
			return nil
		}
		d := DirInfo{
			Mode: fi.Mode().Perm(),
		}
		index.Entries.Dirs[path] = &d
		return nil
	}
	if de.Type()&fs.ModeSymlink != 0 {
		link, _ := os.Readlink(path)
		l := LinkInfo{
			LinkTo: link,
		}
		index.Entries.Links[path] = &l
		return nil
	}
	if de.Type().IsRegular() {
		fi, err := de.Info()
		if err != nil {
			slog.Debug("Can't stat file", "path", path)
			return nil
		}
		f := FileInfo{
			Mode:    fi.Mode().Perm(),
			ModTime: fi.ModTime().Unix(),
			Size:    fi.Size(),
		}
		index.Entries.Files[path] = &f
		return nil
	}
	slog.Debug("Skip non-regular file", "path", path)
	return nil
}

// GenerateLocal ...
func (index *Index) GenerateLocal(remoteIndex *Index) {
	slog.Debug("Generating local index")
	for _, filePath := range index.config.FilePaths {
		slog.Debug("Walk", "path", filePath)

		err := filepath.WalkDir(path.Join(index.config.BasePath, filePath), func(path string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return index.scanSingleFile(path, de)
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
				// slog.Info("Use cached hash ", h, " for ", fp)
			}
		}
		if !cachedHash {
			var err error
			h, err = chunkHash(fp, fi.Size)
			if err != nil {
				slog.Error("Hash calculation failed", "path", fp, "error", err)
				delete(index.Entries.Files, fp)
				continue
			}
			// slog.Info("Caculated hash ", h, " for ", fp)
		}
		fi.Hash = h
	}
}
