package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Settings struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func SettingsFromEnv() Settings {
	bucket := os.Getenv("MINIO_BUCKET")
	if bucket == "" {
		bucket = "hackathonkit"
	}
	return Settings{
		Endpoint:  os.Getenv("MINIO_ENDPOINT"),
		AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("MINIO_SECRET_KEY"),
		Bucket:    bucket,
		UseSSL:    os.Getenv("MINIO_USE_SSL") == "true",
	}
}

type Client struct {
	client *minio.Client
	bucket string
}

func Open(ctx context.Context, s Settings) (*Client, error) {
	if s.Endpoint == "" {
		return nil, fmt.Errorf("objectstore: MINIO_ENDPOINT is not set")
	}
	if strings.TrimSpace(s.AccessKey) == "" || strings.TrimSpace(s.SecretKey) == "" {
		return nil, fmt.Errorf("objectstore: MINIO_ACCESS_KEY and MINIO_SECRET_KEY are required")
	}
	cli, err := minio.New(s.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(s.AccessKey, s.SecretKey, ""),
		Secure: s.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	exists, err := cli.BucketExists(ctx, s.Bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := cli.MakeBucket(ctx, s.Bucket, minio.MakeBucketOptions{}); err != nil {
			// Another replica may have created it between the check and the
			// call; only fail when the bucket really is unavailable.
			if again, existsErr := cli.BucketExists(ctx, s.Bucket); existsErr != nil || !again {
				return nil, err
			}
		}
	}
	return &Client{client: cli, bucket: s.Bucket}, nil
}

func (c *Client) Put(ctx context.Context, name string, data []byte) error {
	_, err := c.client.PutObject(ctx, c.bucket, name, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (c *Client) Get(ctx context.Context, name string) ([]byte, error) {
	obj, err := c.client.GetObject(ctx, c.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	info, err := obj.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size > maxObjectBytes {
		return nil, fmt.Errorf("objectstore: object %s exceeds %d bytes", name, maxObjectBytes)
	}
	data, err := io.ReadAll(io.LimitReader(obj, maxObjectBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxObjectBytes {
		return nil, fmt.Errorf("objectstore: object %s exceeds %d bytes", name, maxObjectBytes)
	}
	return data, nil
}

const maxObjectBytes = 64 << 20
