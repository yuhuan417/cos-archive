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
	"sync"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
)

type ChunksMap = map[string]bool

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

		chunksMap[chunkHash(fi)] = false
	}
	return chunksMap
}

func uploadPayload(config Config, notCheckBeforeUpload bool, ctx UploadCTX) {
	log.Println("Uploading:", ctx)
	u, _ := url.Parse(config.COS.URL)
	b := &cos.BaseURL{BucketURL: u}
	c := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.COS.ID,
			SecretKey: config.COS.Key,
		},
	})

	rp := path.Join(config.COS.Prefix, ctx.hash)
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
				XCosStorageClass: "ARCHIVE",
			},
		},
	}
	_, _, err := c.Object.Upload(
		context.Background(), rp, ctx.path, opt)
	if err != nil {
		log.Fatalln("Upload file error:", ctx, err)
	}
}

func uploadFiles(config Config, localIndex *Index, remoteIndex *Index) {
	deleteTime := time.Now().AddDate(0, 1, 0).Unix()
	remoteChunkMap := buildChunksMap(*remoteIndex)
	localChunkMap := buildChunksMap(*localIndex)

	localIndex.Chunks = make(ChunkDeleteMarkMap)
	for k, v := range remoteIndex.Chunks {
		if v > deleteTime {
			v = deleteTime
		}
		localIndex.Chunks[k] = v
	}
	wg := &sync.WaitGroup{}
	ch := make(chan UploadCTX, config.Threads)

	uploadFile := func(id int) {
		defer wg.Done()
		for ctx := range ch {
			uploadPayload(config, false, ctx)
		}
	}
	wg.Add(config.Threads)
	for i := 0; i < config.Threads; i++ {
		go uploadFile(i)
	}
	for fp, fi := range localIndex.Files {
		if fi.IsDir || fi.LinkTo != "" {
			continue
		}

		h := chunkHash(fi)
		// 检查相同的chunk是否已经处理过
		lc, _ := localChunkMap[h]
		if lc {
			continue
		}

		localChunkMap[h] = true
		_, ok := remoteChunkMap[h]
		if ok {
			remoteChunkMap[h] = true
		} else {
			// 需要上传
			ctx := new(UploadCTX)
			ctx.path = fp
			ctx.hash = h
			ch <- *ctx
		}
		_, ok = remoteIndex.Chunks[h]
		if ok {
			delete(localIndex.Chunks, h)
		}
	}

	close(ch)
	wg.Wait()

	for k, v := range remoteChunkMap {
		if !v {
			log.Println("Marking delete chunk:", k)
			localIndex.Chunks[k] = deleteTime
		}
	}
}

func downloadRemoteIndex(config Config, path string) bool {
	log.Println("Download remote index to ", path)
	u, _ := url.Parse(config.COS.URL)
	b := &cos.BaseURL{BucketURL: u}
	c := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.COS.ID,
			SecretKey: config.COS.Key,
		},
	})

	_, err := c.Object.GetToFile(context.Background(), "meta.json", path, nil)
	if err != nil {
		log.Println("Download index error:", err)
		return false
	}
	return true
}

func uploadRemoteIndex(config Config, content []byte) {
	u, _ := url.Parse(config.COS.URL)
	b := &cos.BaseURL{BucketURL: u}
	c := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.COS.ID,
			SecretKey: config.COS.Key,
		},
	})

	rh := getRemoteMetaHash(config)
	tmpfp := path.Join(config.WorkingDir, "meta.json.new")
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
			context.Background(), "meta.json", tmpfp, opt)
		if err != nil {
			log.Fatalln("Upload index fail:", err)
		}
	}
	fp := path.Join(config.WorkingDir, "meta.json")
	os.Rename(tmpfp, fp)
}

func getRemoteMetaHash(config Config) string {
	u, _ := url.Parse(config.COS.URL)
	b := &cos.BaseURL{BucketURL: u}
	c := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.COS.ID,
			SecretKey: config.COS.Key,
		},
	})
	resp, err := c.Object.Head(context.Background(), "meta.json", nil)
	if err != nil {
		return ""
	}
	return resp.Header.Get("x-cos-meta-hash")
}

func deleteOutdatedChunks(config Config, index *Index) {
	now := time.Now().Unix()
	u, _ := url.Parse(config.COS.URL)
	b := &cos.BaseURL{BucketURL: u}
	c := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.COS.ID,
			SecretKey: config.COS.Key,
		},
	})
	for fp, t := range index.Chunks {
		if now > t {
			log.Println("Deleting remote chunk: ", fp)
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
	u, _ := url.Parse(config.COS.URL)
	b := &cos.BaseURL{BucketURL: u}
	c := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.COS.ID,
			SecretKey: config.COS.Key,
		},
	})

	opt := &cos.BucketGetOptions{
		Prefix:  config.COS.Prefix,
		MaxKeys: 1000,
	}
	for {
		v, _, err := c.Bucket.Get(context.Background(), opt)
		if err != nil {
			log.Fatalln("Scan error:", err)
		}
		prefix := filepath.Clean(config.COS.Prefix) + "/"
		for _, c := range v.Contents {
			s := c.Key
			cm[strings.TrimPrefix(s, prefix)] = false
		}
		if !v.IsTruncated {
			break
		}
		opt = &cos.BucketGetOptions{
			Prefix:  config.COS.Prefix,
			MaxKeys: 1000,
			Marker:  v.NextMarker,
		}
	}
	return cm
}
