package main

import (
	"context"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
	"go.uber.org/ratelimit"
)

// ChunksMap struct
type ChunksMap = map[string]bool

// UploadCTX struct
type UploadCTX struct {
	path string
	hash string
}

func buildChunksMap(index Index) ChunksMap {
	chunksMap := make(ChunksMap)
	for _, fi := range index.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}

		chunksMap[chunkHash(fi.Size, fi.Hash)] = false
	}
	return chunksMap
}

func uploadPayload(config Config, notCheckBeforeUpload bool, ctx UploadCTX) {
	// log.Println("Uploading:", ctx)
	c := cosInit(config)

	rp := path.Join(config.COS.ChunkPrefix, ctx.hash)
	if !notCheckBeforeUpload {
		_, err := c.Object.Head(context.Background(), rp, nil)
		if err == nil {
			log.Println("File already exists:", ctx)
			return
		}
	}
	opt := &cos.MultiUploadOptions{
		ThreadPoolSize: 2,
		OptIni: &cos.InitiateMultipartUploadOptions{
			ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
				XCosStorageClass: "DEEP_ARCHIVE",
			},
		},
	}
	_, _, err := c.Object.Upload(
		context.Background(), rp, ctx.path, opt)
	if err != nil {
		log.Fatalln("Upload file error:", ctx, err)
	}
}

func downloadRemoteIndex(config Config, path string) bool {
	log.Println("Download remote index to ", path)
	c := cosInit(config)

	_, err := c.Object.GetToFile(context.Background(), config.Index, path, nil)
	if err != nil {
		log.Println("Download index error:", err)
		return false
	}
	return true
}

func uploadRemoteIndex(config Config, content []byte) {
	log.Println("Uploading index")
	c := cosInit(config)

	rh := getRemoteMetaHash(config)
	tmpfp := path.Join(config.WorkingDir, config.Index+".new")
	ioutil.WriteFile(tmpfp, content, 0666)
	lh := metaHash(tmpfp)
	if rh != lh {
		hh := http.Header{}
		hh.Add("x-cos-meta-hash", lh)
		opt := &cos.MultiUploadOptions{
			OptIni: &cos.InitiateMultipartUploadOptions{
				ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
					XCosMetaXXX: &hh,
				},
			},
		}
		_, _, err := c.Object.Upload(
			context.Background(), config.Index, tmpfp, opt)
		if err != nil {
			log.Fatalln("Upload index fail:", err)
		}
	}
	fp := path.Join(config.WorkingDir, config.Index)
	os.Rename(tmpfp, fp)
}

func getRemoteMetaHash(config Config) string {
	c := cosInit(config)
	resp, err := c.Object.Head(context.Background(), config.Index, nil)
	if err != nil {
		return ""
	}
	return resp.Header.Get("x-cos-meta-hash")
}

func deleteOutdatedChunks(config Config, index *Index) {
	now := time.Now().Unix()
	c := cosInit(config)

	for fp, t := range index.Chunks {
		if now > t {
			// log.Println("Deleting remote chunk: ", fp)
			_, err := c.Object.Delete(context.Background(), fp)
			if err != nil {
				continue
			}
			delete(index.Chunks, fp)
		}
	}
}

func scanRemoteChunksMap(config Config) ChunksMap {
	log.Println("Scan remote chunks")
	cm := make(ChunksMap)

	c := cosInit(config)

	opt := &cos.BucketGetOptions{
		Prefix:  config.COS.ChunkPrefix,
		MaxKeys: 1000,
	}
	for {
		v, _, err := c.Bucket.Get(context.Background(), opt)
		if err != nil {
			log.Fatalln("Scan error:", err)
		}
		prefix := filepath.Clean(config.COS.ChunkPrefix) + "/"
		for _, c := range v.Contents {
			s := c.Key
			cm[strings.TrimPrefix(s, prefix)] = false
		}
		if !v.IsTruncated {
			break
		}
		opt = &cos.BucketGetOptions{
			Prefix:  config.COS.ChunkPrefix,
			MaxKeys: 1000,
			Marker:  v.NextMarker,
		}
	}
	return cm
}

func restoreChunk(config Config, cm ChunksMap) {
	log.Println("Restoring chunks")
	// Download chunk from map
	rl := ratelimit.New(90) // per second, hardcode.

	c := cosInit(config)

	for k := range cm {
		rl.Take()
		p := path.Join(config.COS.ChunkPrefix, k)
		opt := &cos.ObjectRestoreOptions{
			Days: 3,
			Tier: &cos.CASJobParameters{
				// Standard, Exepdited and Bulk
				Tier: "Bulk",
			},
		}

		_, err := c.Object.PostRestore(context.Background(), p, opt)
		if err != nil {
			log.Println("Restore file error:", p, err)
		}
	}
}

func downloadChunk(config Config, cm ChunksMap) {
	log.Println("Downloading chunks")
	// Download chunk from map

	c := cosInit(config)

	for k := range cm {
		cp := path.Join(config.TargetDir, "chunks")
		err := os.MkdirAll(cp, 0755)
		if err != nil {
			log.Println("Can't create chunks directory: ", cp)
		}
		p := path.Join(config.COS.ChunkPrefix, k)
		lp := path.Join(cp, k[len(k)-2:], k)
		for {
			log.Println("Downloading:", p)
			if fi, err := os.Stat(lp); err == nil {
				h := xunleiHash(lp, fi.Size())
				if chunkHash(fi.Size(), h) == k {
					log.Println("File exists. Hash matches. Good, skip.")
					continue
				}
			}
			_, err = c.Object.GetToFile(context.Background(), p, lp, nil)
			if err != nil {
				log.Println("Download file error:", p, err)
			}
		}
	}
}

func cosInit(config Config) *cos.Client {
	u, _ := url.Parse(config.COS.URL)
	b := &cos.BaseURL{BucketURL: u}
	return cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.COS.ID,
			SecretKey: config.COS.Key,
		},
	})
}
