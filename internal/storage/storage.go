package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	InputBucket     string
}

type ObjectInfo struct {
	Bucket      string
	ObjectKey   string
	Size        int64
	ETag        string
	ContentType string
}

type Client struct {
	minio       *minio.Client
	inputBucket string
}

func ConfigFromEnv() Config {
	useSSL, _ := strconv.ParseBool(envDefault("S3_USE_SSL", "false"))
	return Config{
		Endpoint:        strings.TrimSpace(os.Getenv("S3_ENDPOINT")),
		AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		UseSSL:          useSSL,
		InputBucket:     envDefault("S3_INPUT_BUCKET", "inputs"),
	}
}

func NewFromEnv() (*Client, error) {
	return New(ConfigFromEnv())
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("S3_ENDPOINT is required")
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" {
		return nil, fmt.Errorf("S3_ACCESS_KEY_ID is required")
	}
	if strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, fmt.Errorf("S3_SECRET_ACCESS_KEY is required")
	}
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	return &Client{minio: c, inputBucket: envDefaultValue(cfg.InputBucket, "inputs")}, nil
}

func (c *Client) DefaultInputBucket() string { return c.inputBucket }

func (c *Client) PutObject(ctx context.Context, bucket string, objectKey string, reader io.Reader, size int64, contentType string) (ObjectInfo, error) {
	info, err := c.minio.PutObject(ctx, bucket, objectKey, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return ObjectInfo{}, err
	}
	obj, err := c.StatObject(ctx, bucket, objectKey)
	if err != nil {
		return ObjectInfo{Bucket: bucket, ObjectKey: objectKey, Size: info.Size, ETag: info.ETag, ContentType: contentType}, nil
	}
	return obj, nil
}

func (c *Client) StatObject(ctx context.Context, bucket string, objectKey string) (ObjectInfo, error) {
	info, err := c.minio.StatObject(ctx, bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Bucket: bucket, ObjectKey: objectKey, Size: info.Size, ETag: info.ETag, ContentType: info.ContentType}, nil
}

func (c *Client) PresignedGetObject(ctx context.Context, bucket string, objectKey string, expiry time.Duration) (string, error) {
	u, err := c.minio.PresignedGetObject(ctx, bucket, objectKey, expiry, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func URI(bucket string, objectKey string) string {
	return "s3://" + strings.Trim(bucket, "/") + "/" + strings.TrimLeft(objectKey, "/")
}

func envDefault(key string, fallback string) string {
	return envDefaultValue(os.Getenv(key), fallback)
}

func envDefaultValue(v string, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}
