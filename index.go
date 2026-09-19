package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/tencentyun/cos-go-sdk-v5"
)

// indexJSONOpts keeps the on-disk index byte-stable across runs. The file's
// sha1 is compared against the remote copy to decide whether an upload is
// needed, so deterministic map ordering is load-bearing, not cosmetic.
var indexJSONOpts = jsonv2.JoinOptions(
	jsonv2.Deterministic(true),
	jsontext.WithIndent("  "),
)

// Index struct
type Index struct {
	Entries       DirEnt
	DeletedChunks ChunkDeleteMarkMap
	config        Config
	cos           *COS
}

// NewIndex ...
func NewIndex(config Config, c *COS) *Index {
	index := Index{}
	index.Entries.Files = make(FileInfoMap)
	index.Entries.Dirs = make(DirInfoMap)
	index.Entries.Links = make(LinkInfoMap)
	index.DeletedChunks = make(ChunkDeleteMarkMap)
	index.config = config
	index.cos = c
	return &index
}

func (index *Index) requireCOS() (*COS, error) {
	if index.cos == nil {
		return nil, errors.New("cos client required")
	}
	return index.cos, nil
}

func (index *Index) metaHash(path string) string {
	h := sha1.New()
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
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

	if err := jsonv2.UnmarshalRead(jf, index); err != nil {
		slog.Debug("Load index error", "path", path, "error", err)
		return err
	}

	return nil
}

// Save writes index to a local file.
func (index *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	w, err := os.Create(path)
	if err != nil {
		return err
	}
	defer w.Close()

	if err := jsonv2.MarshalWrite(w, index, indexJSONOpts); err != nil {
		return err
	}
	return nil
}

func (index *Index) downloadRemote(ctx context.Context, remote string, localPath string) error {
	c, err := index.requireCOS()
	if err != nil {
		return err
	}
	slog.Debug("Download remote index", "remote", remote, "path", localPath)
	if err := c.DownloadFile(ctx, remote, localPath); err != nil {
		slog.Debug("Download index error", "error", err)
		return err
	}
	return nil
}

func (index *Index) getRemoteHash(ctx context.Context, p string) (string, error) {
	c, err := index.requireCOS()
	if err != nil {
		return "", err
	}
	h, err := c.GetHeader(ctx, p)
	if err != nil {
		return "", err
	}
	if h == nil {
		return "", nil
	}
	return h.Get("x-cos-meta-hash"), nil
}

// UploadRemote ...
func (index *Index) UploadRemote(ctx context.Context) error {
	slog.Debug("Uploading index")
	if err := os.MkdirAll(index.config.WorkingDir, 0755); err != nil {
		return err
	}
	w, err := os.CreateTemp(index.config.WorkingDir, index.config.Index+".*.new")
	if err != nil {
		return err
	}
	defer w.Close()

	if err := jsonv2.MarshalWrite(w, index, indexJSONOpts); err != nil {
		return err
	}
	tmpfp := w.Name()

	lh := index.metaHash(tmpfp)
	latestIndex := ""
	rh := ""
	latestIndex, err = index.getLatestIndex(ctx)
	if err != nil && !errors.Is(err, ErrIndexNotFound) {
		return err
	}
	if latestIndex != "" {
		rh, err = index.getRemoteHash(ctx, latestIndex)
		if err != nil {
			return err
		}
	}
	if lh != rh {
		hh := http.Header{}
		hh.Add("x-cos-meta-hash", lh)
		c, err := index.requireCOS()
		if err != nil {
			return err
		}
		newName := index.config.Index + "." + strconv.FormatInt(time.Now().Unix(), 10)
		slog.Debug("Upload index to", "name", newName)
		if err := c.UploadFile(ctx, newName, tmpfp, "STANDARD", hh); err != nil {
			return err
		}
	} else {
		slog.Debug("Hash matched, old index is good enough")
	}
	fp := path.Join(index.config.WorkingDir, index.config.Index)
	if err := os.Rename(tmpfp, fp); err != nil {
		return err
	}
	return nil
}

func (index *Index) getLatestIndex(ctx context.Context) (string, error) {
	c, err := index.requireCOS()
	if err != nil {
		return "", err
	}

	indexList := []string{}
	err = c.ScanFiles(ctx, index.config.Index, func(obj cos.Object) {
		indexList = append(indexList, obj.Key)
	})
	if err != nil {
		return "", err
	}
	sort.Slice(indexList, func(i, j int) bool {
		prefix := index.config.Index + "."
		numA, _ := strconv.ParseInt(strings.TrimPrefix(indexList[i], prefix), 10, 64)
		numB, _ := strconv.ParseInt(strings.TrimPrefix(indexList[j], prefix), 10, 64)
		return numB < numA
	})
	if len(indexList) > 30 {
		for i := 30; i < len(indexList); i++ {
			if err := c.DeleteFile(ctx, indexList[i]); err != nil {
				slog.Debug("Delete outdated index error", "index", indexList[i], "error", err)
			}
		}
	}

	if len(indexList) == 0 {
		return "", ErrIndexNotFound
	}
	latestIndex := indexList[0]
	slog.Debug("Using latest index", "index", latestIndex)
	return latestIndex, nil
}

// LoadRemote ...
func (index *Index) LoadRemote(ctx context.Context) error {
	slog.Debug("Loading remote index")
	oldPath := path.Join(index.config.WorkingDir, index.config.Index)
	mh := index.metaHash(oldPath)

	latestIndex, err := index.getLatestIndex(ctx)
	if err != nil {
		return err
	}

	rh, err := index.getRemoteHash(ctx, latestIndex)
	if err != nil {
		return err
	}
	if mh != "" && mh == rh {
		slog.Debug("Hash match, using local old index.")
		return index.Load(oldPath)
	}

	riPath := path.Join(index.config.WorkingDir, index.config.Index+".remote")
	if err := os.MkdirAll(index.config.WorkingDir, 0755); err != nil {
		return err
	}
	if err := os.Remove(riPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := index.downloadRemote(ctx, latestIndex, riPath); err != nil {
		return err
	}
	slog.Debug("Download remote index to", "path", riPath)
	return index.Load(riPath)
}

// DeleteOutdatedChunks ...
func (index *Index) DeleteOutdatedChunks(ctx context.Context) error {
	now := time.Now().Unix()
	c, err := index.requireCOS()
	if err != nil {
		return err
	}
	cnt := 0
	freedSize := int64(0)
	var errs error
	for fp, t := range index.DeletedChunks {
		if now <= t {
			continue
		}
		var lmt int64
		h, err := c.GetHeader(ctx, path.Join(index.config.COS.ChunkPrefix, chunkPath(fp)))
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		if h != nil {
			lm := h.Get("Last-Modified")
			layout := "Mon, 2 Jan 2006 15:04:05 MST"

			modTime, err := time.Parse(layout, lm)
			if err == nil {
				lmt = modTime.AddDate(0, 0, 180).Unix()
			}
		}
		if lmt > t {
			slog.Debug("Fix time", "path", fp, "from", t, "to", lmt)
			index.DeletedChunks[fp] = lmt
		}
		if now > lmt {
			slog.Debug("Deleting remote chunk", "path", fp)
			if err := c.DeleteFile(ctx, path.Join(index.config.COS.ChunkPrefix, chunkPath(fp))); err != nil {
				errs = errors.Join(errs, err)
				continue
			}
			delete(index.DeletedChunks, fp)
			cnt++
			freedSize += fp.size
		}
	}
	slog.Info("Deleting outdated remote chunk", "count", cnt, "size", humanize.IBytes(uint64(freedSize)))
	return errs
}
