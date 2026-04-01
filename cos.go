package main

import (
	"context"
	"fmt"
	"log/slog"
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
func NewCOS(config COSConfig) (*COS, error) {
	u, err := url.Parse(config.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid COS URL %q: %v", ErrConfigInvalid, config.URL, err)
	}
	b := &cos.BaseURL{BucketURL: u}
	return &COS{
		c: cos.NewClient(b, &http.Client{
			Transport: &cos.AuthorizationTransport{
				SecretID:  config.ID,
				SecretKey: config.Key,
			},
		}),
		config: config,
	}, nil
}

// DownloadFile from remote to local
func (c *COS) DownloadFile(ctx context.Context, remote string, local string) error {
	_, err := c.c.Object.GetToFile(ctx, remote, local, nil)
	if err != nil {
		slog.Debug("Download file error", "remote", remote, "local", local, "error", err)
	}
	return err
}

// UploadFile to cos
// Server error: ServiceUnavailable, retry
func (c *COS) UploadFile(ctx context.Context, key string, file string, class string, header http.Header) error {
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
		_, _, err = c.c.Object.Upload(ctx, key, file, opt)
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
func (c *COS) GetHeader(ctx context.Context, file string) (http.Header, error) {
	resp, err := c.c.Object.Head(ctx, file, nil)
	if err != nil {
		if cos.IsNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	return resp.Header, nil
}

// DeleteFile from cos
func (c *COS) DeleteFile(ctx context.Context, file string) error {
	_, err := c.c.Object.Delete(ctx, file)
	return err
}

// ScanFiles from cos
func (c *COS) ScanFiles(ctx context.Context, prefix string, cb func(cos.Object)) error {
	opt := &cos.BucketGetOptions{
		Prefix:  prefix,
		MaxKeys: 1000,
	}
	for {
		v, _, err := c.c.Bucket.Get(ctx, opt)
		if err != nil {
			return err
		}
		for _, content := range v.Contents {
			cb(content)
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
	return nil
}

// RestoreFile from COS
func (c *COS) RestoreFile(ctx context.Context, file string) error {
	opt := &cos.ObjectRestoreOptions{
		Days: 3,
		Tier: &cos.CASJobParameters{
			Tier: "Bulk",
		},
	}
	_, err := c.c.Object.PostRestore(ctx, file, opt)
	return err
}
