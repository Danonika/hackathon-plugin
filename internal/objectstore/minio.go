package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

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
			return nil, err
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
	if _, err := obj.Stat(); err != nil {
		return nil, err
	}
	return io.ReadAll(obj)
}
