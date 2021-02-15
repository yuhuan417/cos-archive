package main

import (
	"context"
	"log"
	"net/http"
	"net/url"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// COS struct
type COS struct {
	c      *cos.Client
	config COSConfig
}

// NewCOS new COS
func NewCOS(config COSConfig) *COS {
	var c = new(COS)
	u, _ := url.Parse(config.URL)
	b := &cos.BaseURL{BucketURL: u}
	c.c = cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.ID,
			SecretKey: config.Key,
		},
	})
	c.config = config
	return c
}

// DownloadFile from remote to local
func (c *COS) DownloadFile(remote string, local string) error {
	_, err := c.c.Object.GetToFile(context.Background(), remote, local, nil)
	if err != nil {
		log.Println("Download file error:", remote, local)
	}
	return err
}

// UploadFile to cos
// Server error: ServiceUnavailable, retry
func (c *COS) UploadFile(key string, file string, class string, header http.Header) error {
	opt := &cos.MultiUploadOptions{
		ThreadPoolSize: 2,
		OptIni: &cos.InitiateMultipartUploadOptions{
			ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
				XCosStorageClass: class,
				XCosMetaXXX:      &header,
			},
		},
	}
	var err error
	for i := 0; i <= c.config.Retries; i++ {
		_, _, err = c.c.Object.Upload(
			context.Background(), key, file, opt)
		if err == nil {
			return nil
		}
		if e, ok := cos.IsCOSError(err); ok {
			if e.Code != "ServiceUnavailable" {
				return err
			}
		}
	}
	return err
}

// GetHeader from cos
func (c *COS) GetHeader(file string) http.Header {
	resp, err := c.c.Object.Head(context.Background(), file, nil)
	if err != nil {
		return nil
	}
	return resp.Header
}

// DeleteFile from cos
func (c *COS) DeleteFile(file string) error {
	_, err := c.c.Object.Delete(context.Background(), file)
	return err
}

// ScanFiles from cos
func (c *COS) ScanFiles(prefix string, cb func(key string)) {
	opt := &cos.BucketGetOptions{
		Prefix:  prefix,
		MaxKeys: 1000,
	}
	for {
		v, _, err := c.c.Bucket.Get(context.Background(), opt)
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
			Prefix:  prefix,
			MaxKeys: 1000,
			Marker:  v.NextMarker,
		}
	}
	return
}

// RestoreFile from COS
func (c *COS) RestoreFile(file string) error {
	opt := &cos.ObjectRestoreOptions{
		Days: 3,
		Tier: &cos.CASJobParameters{
			// Standard, Exepdited and Bulk
			Tier: "Bulk",
		},
	}
	_, err := c.c.Object.PostRestore(context.Background(), file, opt)
	return err
}
