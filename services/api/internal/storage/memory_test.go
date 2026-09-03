package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/judeotine/afterword/services/api/internal/storage"
)

func TestMemoryHeadReportsMissingObjects(t *testing.T) {
	client := storage.NewMemory()

	if _, err := client.Head(context.Background(), "audio", "ws/a/meetings/b/audio.opus"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("head missing object = %v, want ErrNotFound", err)
	}
}

func TestMemoryRoundTripsAnObject(t *testing.T) {
	client := storage.NewMemory()
	client.Put("audio", "key", []byte("hello"), "audio/opus")

	info, err := client.Head(context.Background(), "audio", "key")
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if info.Size != 5 || info.ContentType != "audio/opus" || info.ETag == "" {
		t.Fatalf("unexpected object info: %+v", info)
	}

	if err := client.Copy(context.Background(), "audio", "key", "clips", "copy"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !client.Exists("clips", "copy") {
		t.Fatal("copy did not create the target object")
	}

	if err := client.Delete(context.Background(), "audio", "key"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if client.Exists("audio", "key") {
		t.Fatal("delete left the object behind")
	}
	if len(client.Deletes()) != 1 {
		t.Fatalf("expected one recorded delete, got %v", client.Deletes())
	}
}

func TestMemoryDeleteIsIdempotent(t *testing.T) {
	client := storage.NewMemory()
	for range 3 {
		if err := client.Delete(context.Background(), "audio", "gone"); err != nil {
			t.Fatalf("delete missing object: %v", err)
		}
	}
}

func TestMemoryRecordsPresignRequests(t *testing.T) {
	client := storage.NewMemory()

	upload, err := client.PresignUpload(context.Background(), "audio", "key", "audio/opus", 1024, time.Minute)
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	if upload.Method != "PUT" || upload.URL == "" || upload.MaxBytes != 1024 {
		t.Fatalf("unexpected upload request: %+v", upload)
	}
	if upload.Headers["Content-Type"] != "audio/opus" {
		t.Fatalf("unexpected upload headers: %+v", upload.Headers)
	}

	download, err := client.PresignDownload(context.Background(), "audio", "key", 0)
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	if download.Method != "GET" || download.URL == "" {
		t.Fatalf("unexpected download request: %+v", download)
	}

	records := client.Presigns()
	if len(records) != 2 || records[0].MaxBytes != 1024 || records[1].TTL != storage.DefaultPresignTTL {
		t.Fatalf("unexpected presign records: %+v", records)
	}
}

func TestMemoryRejectsEmptyBucketsAndKeys(t *testing.T) {
	client := storage.NewMemory()

	if _, err := client.PresignUpload(context.Background(), "", "key", "", 0, 0); !errors.Is(err, storage.ErrBucketRequired) {
		t.Fatalf("empty bucket = %v, want ErrBucketRequired", err)
	}
	if _, err := client.PresignDownload(context.Background(), "audio", " ", 0); !errors.Is(err, storage.ErrKeyRequired) {
		t.Fatalf("empty key = %v, want ErrKeyRequired", err)
	}
}

func TestFailingClientRefusesToPresign(t *testing.T) {
	sentinel := errors.New("boom")
	client := storage.NewFailing(sentinel)

	if _, err := client.PresignUpload(context.Background(), "audio", "key", "", 0, 0); !errors.Is(err, sentinel) {
		t.Fatalf("presign upload = %v, want the injected failure", err)
	}
	if _, err := client.PresignDownload(context.Background(), "audio", "key", 0); !errors.Is(err, sentinel) {
		t.Fatalf("presign download = %v, want the injected failure", err)
	}
}
