package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
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
	fmt.Println("Upload " + ctx.path + " as " + ctx.hash)
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
		panic(err)
	}
}

func uploadFiles(config Config, localIndex *Index, remoteIndex *Index) {
	deleteTime := time.Now().AddDate(0, 1, 0).Unix()
	remoteChunkMap := buildChunksMap(*remoteIndex)
	localChunkMap := buildChunksMap(*localIndex)

	localIndex.Chunks = make(ChunkDeleteMarkMap)
	for k, v := range remoteIndex.Chunks {
		localIndex.Chunks[k] = v
	}
	wg := &sync.WaitGroup{}
	ch := make(chan UploadCTX, config.Threads)

	uploadFile := func(id int) {
		defer wg.Done()
		for ctx := range ch {
			uploadPayload(config, remoteIndex.fromCOS, ctx)
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
			delete(localIndex.Chunks, h)
		} else {
			// 需要上传
			ctx := new(UploadCTX)
			ctx.path = fp
			ctx.hash = h
			ch <- *ctx
		}
	}

	close(ch)
	wg.Wait()

	for k, v := range remoteChunkMap {
		if !v {
			localIndex.Chunks[k] = deleteTime
		}
	}
}

func downloadRemoteIndex(config Config, path string) bool {
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
			panic(err)
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
		fmt.Println("remote meta not found.")
		return ""
	}
	return resp.Header.Get("x-cos-meta-hash")
}
