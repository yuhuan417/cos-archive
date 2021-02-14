package main

import (
	"context"
	"log"
	"net/http"
	"net/url"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// UploadCTX struct
type UploadCTX struct {
	path string
	hash string
}

func cosDownloadFile(config COSConfig, remote string, local string) error {
	c := cosInit(config)

	_, err := c.Object.GetToFile(context.Background(), remote, local, nil)
	if err != nil {
		log.Println("Download file error:", remote, local)
	}
	return err
}

func cosUploadFile(config COSConfig, key string, file string, class string, header http.Header) error {
	c := cosInit(config)

	opt := &cos.MultiUploadOptions{
		ThreadPoolSize: 2,
		OptIni: &cos.InitiateMultipartUploadOptions{
			ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
				XCosStorageClass: class,
				XCosMetaXXX:      &header,
			},
		},
	}
	_, _, err := c.Object.Upload(
		context.Background(), key, file, opt)
	return err
}

func cosGetHeader(config COSConfig, file string) http.Header {
	c := cosInit(config)
	resp, err := c.Object.Head(context.Background(), file, nil)
	if err != nil {
		return nil
	}
	return resp.Header
}

func cosDeleteFile(config COSConfig, file string) error {
	c := cosInit(config)
	_, err := c.Object.Delete(context.Background(), file)
	return err
}

func cosScanFiles(config COSConfig, prefix string, cb func(key string)) {
	c := cosInit(config)

	opt := &cos.BucketGetOptions{
		Prefix:  prefix,
		MaxKeys: 1000,
	}
	for {
		v, _, err := c.Bucket.Get(context.Background(), opt)
		if err != nil {
			log.Fatalln("Scan error:", err)
		}
		for _, c := range v.Contents {
			s := c.Key
			cb(s)
		}
		if !v.IsTruncated {
			break
		}
		opt = &cos.BucketGetOptions{
			Prefix:  config.ChunkPrefix,
			MaxKeys: 1000,
			Marker:  v.NextMarker,
		}
	}
	return
}

func cosRestoreFile(config COSConfig, file string) error {
	c := cosInit(config)
	opt := &cos.ObjectRestoreOptions{
		Days: 3,
		Tier: &cos.CASJobParameters{
			// Standard, Exepdited and Bulk
			Tier: "Bulk",
		},
	}
	_, err := c.Object.PostRestore(context.Background(), file, opt)
	return err
}

func cosInit(config COSConfig) *cos.Client {
	u, _ := url.Parse(config.URL)
	b := &cos.BaseURL{BucketURL: u}
	return cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.ID,
			SecretKey: config.Key,
		},
	})
}
