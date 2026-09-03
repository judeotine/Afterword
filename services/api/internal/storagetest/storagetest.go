package storagetest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/judeotine/afterword/services/api/internal/storage"
)

const (
	ContainerImage = "minio/minio:RELEASE.2025-09-07T16-13-09Z"
	AccessKey      = "afterword"
	SecretKey      = "afterword-secret"

	SkipReason = "no reachable Docker daemon: start Docker to run the MinIO integration tests"
)

var (
	once     sync.Once
	endpoint string
	startErr error
)

func Endpoint(t *testing.T) string {
	t.Helper()
	once.Do(start)
	if startErr != nil {
		t.Fatalf("start minio: %v", startErr)
	}
	if endpoint == "" {
		t.Skip(SkipReason)
	}
	return endpoint
}

func start() {
	if !dockerAvailable() {
		return
	}

	ctx := context.Background()
	container, err := tcminio.Run(ctx, ContainerImage,
		tcminio.WithUsername(AccessKey),
		tcminio.WithPassword(SecretKey),
	)
	if err != nil {
		startErr = fmt.Errorf("run minio container: %w", err)
		return
	}
	address, err := container.ConnectionString(ctx)
	if err != nil {
		startErr = fmt.Errorf("minio connection string: %w", err)
		return
	}
	if !strings.HasPrefix(address, "http") {
		address = "http://" + address
	}
	endpoint = address
}

func dockerAvailable() bool {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return false
	}
	defer func() {
		_ = provider.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return provider.Health(ctx) == nil
}

func New(t *testing.T) (*storage.S3Client, storage.Buckets) {
	t.Helper()
	client, err := storage.NewS3Client(storage.Options{
		Endpoint:     Endpoint(t),
		Region:       "us-east-1",
		AccessKey:    AccessKey,
		SecretKey:    SecretKey,
		UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("new s3 client: %v", err)
	}

	suffix := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	suffix = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, suffix)
	if len(suffix) > 40 {
		suffix = suffix[:40]
	}
	suffix = strings.Trim(suffix, "-")
	if suffix == "" {
		suffix = "default"
	}
	buckets := storage.Buckets{
		Audio:       "audio-" + suffix,
		Transcripts: "transcripts-" + suffix,
		Clips:       "clips-" + suffix,
		Exports:     "exports-" + suffix,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, bucket := range buckets.Names() {
		if err := client.EnsureBucket(ctx, bucket); err != nil {
			t.Fatalf("ensure bucket %s: %v", bucket, err)
		}
	}
	return client, buckets
}
