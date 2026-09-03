package storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type Options struct {
	Endpoint     string
	Region       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	HTTPClient   *http.Client
}

type S3Client struct {
	api     *s3.Client
	presign *s3.PresignClient
}

var _ Client = (*S3Client)(nil)

func NewS3Client(opts Options) (*S3Client, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(opts.Endpoint), "/")
	if endpoint == "" {
		return nil, errors.New("storage: an s3 endpoint is required")
	}
	region := fallback(opts.Region, "us-east-1")
	if strings.TrimSpace(opts.AccessKey) == "" || strings.TrimSpace(opts.SecretKey) == "" {
		return nil, errors.New("storage: s3 credentials are required")
	}

	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(opts.AccessKey, opts.SecretKey, ""),
	}
	if opts.HTTPClient != nil {
		cfg.HTTPClient = opts.HTTPClient
	}

	api := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = &endpoint
		o.UsePathStyle = opts.UsePathStyle
	})
	return &S3Client{api: api, presign: s3.NewPresignClient(api)}, nil
}

func (c *S3Client) PresignUpload(ctx context.Context, bucket, key, contentType string, maxBytes int64, ttl time.Duration) (PresignedRequest, error) {
	if err := validate(bucket, key); err != nil {
		return PresignedRequest{}, err
	}
	window := normalizeTTL(ttl)

	input := &s3.PutObjectInput{Bucket: &bucket, Key: &key}
	if trimmed := strings.TrimSpace(contentType); trimmed != "" {
		input.ContentType = &trimmed
	}

	signed, err := c.presign.PresignPutObject(ctx, input, s3.WithPresignExpires(window))
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign upload: %w", err)
	}
	return PresignedRequest{
		Method:    signed.Method,
		URL:       signed.URL,
		Headers:   signedHeaders(signed.SignedHeader),
		MaxBytes:  maxBytes,
		ExpiresAt: time.Now().UTC().Add(window),
	}, nil
}

func (c *S3Client) PresignDownload(ctx context.Context, bucket, key string, ttl time.Duration) (PresignedRequest, error) {
	if err := validate(bucket, key); err != nil {
		return PresignedRequest{}, err
	}
	window := normalizeTTL(ttl)

	signed, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key}, s3.WithPresignExpires(window))
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign download: %w", err)
	}
	return PresignedRequest{
		Method:    signed.Method,
		URL:       signed.URL,
		Headers:   signedHeaders(signed.SignedHeader),
		ExpiresAt: time.Now().UTC().Add(window),
	}, nil
}

func (c *S3Client) Delete(ctx context.Context, bucket, key string) error {
	if err := validate(bucket, key); err != nil {
		return err
	}
	if _, err := c.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &key}); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

func (c *S3Client) Head(ctx context.Context, bucket, key string) (ObjectInfo, error) {
	if err := validate(bucket, key); err != nil {
		return ObjectInfo{}, err
	}
	out, err := c.api.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		if isNotFound(err) {
			return ObjectInfo{}, ErrNotFound
		}
		return ObjectInfo{}, fmt.Errorf("head object: %w", err)
	}

	info := ObjectInfo{Bucket: bucket, Key: key}
	if out.ContentLength != nil {
		info.Size = *out.ContentLength
	}
	if out.ContentType != nil {
		info.ContentType = *out.ContentType
	}
	if out.ETag != nil {
		info.ETag = strings.Trim(*out.ETag, `"`)
	}
	if out.LastModified != nil {
		info.LastModified = out.LastModified.UTC()
	}
	return info, nil
}

func (c *S3Client) Copy(ctx context.Context, sourceBucket, sourceKey, targetBucket, targetKey string) error {
	if err := validate(sourceBucket, sourceKey); err != nil {
		return err
	}
	if err := validate(targetBucket, targetKey); err != nil {
		return err
	}
	source := sourceBucket + "/" + sourceKey
	if _, err := c.api.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     &targetBucket,
		Key:        &targetKey,
		CopySource: &source,
	}); err != nil {
		if isNotFound(err) {
			return ErrNotFound
		}
		return fmt.Errorf("copy object: %w", err)
	}
	return nil
}

func (c *S3Client) EnsureBucket(ctx context.Context, bucket string) error {
	if strings.TrimSpace(bucket) == "" {
		return ErrBucketRequired
	}
	if _, err := c.api.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket}); err == nil {
		return nil
	}
	if _, err := c.api.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &bucket}); err != nil {
		var owned *types.BucketAlreadyOwnedByYou
		var exists *types.BucketAlreadyExists
		if errors.As(err, &owned) || errors.As(err, &exists) {
			return nil
		}
		return fmt.Errorf("create bucket %s: %w", bucket, err)
	}
	return nil
}

func signedHeaders(header http.Header) map[string]string {
	if len(header) == 0 {
		return nil
	}
	headers := make(map[string]string, len(header))
	for name := range header {
		value := header.Get(name)
		if value != "" {
			headers[name] = value
		}
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func isNotFound(err error) bool {
	var noSuchKey *types.NoSuchKey
	var notFound *types.NotFound
	var noSuchBucket *types.NoSuchBucket
	if errors.As(err, &noSuchKey) || errors.As(err, &notFound) || errors.As(err, &noSuchBucket) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchBucket", "404":
			return true
		}
	}
	return false
}
