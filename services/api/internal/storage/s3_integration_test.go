//go:build integration

package storage_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/storage"
	"github.com/judeotine/afterword/services/api/internal/storagetest"
)

func TestS3ClientRoundTripsAgainstMinIO(t *testing.T) {
	client, buckets := storagetest.New(t)
	ctx := t.Context()

	workspace := uuid.New()
	meeting := uuid.New()
	key := storage.AudioKey(workspace, meeting, "opus")
	body := bytes.Repeat([]byte("afterword"), 512)

	upload, err := client.PresignUpload(ctx, buckets.Audio, key, "audio/opus", int64(len(body)), 5*time.Minute)
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	if upload.Method != http.MethodPut || upload.URL == "" {
		t.Fatalf("unexpected upload request: %+v", upload)
	}

	if _, err := client.Head(ctx, buckets.Audio, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("head before upload = %v, want ErrNotFound", err)
	}

	put(t, upload, body)

	info, err := client.Head(ctx, buckets.Audio, key)
	if err != nil {
		t.Fatalf("head after upload: %v", err)
	}
	if info.Size != int64(len(body)) {
		t.Fatalf("size %d, want %d", info.Size, len(body))
	}
	if info.ContentType != "audio/opus" {
		t.Fatalf("content type %q", info.ContentType)
	}
	if info.ETag == "" || info.LastModified.IsZero() {
		t.Fatalf("incomplete object info: %+v", info)
	}

	download, err := client.PresignDownload(ctx, buckets.Audio, key, 5*time.Minute)
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	if !bytes.Equal(get(t, download.URL), body) {
		t.Fatal("the downloaded body does not match what was uploaded")
	}

	clipKey := "ws/" + workspace.String() + "/clips/highlight.opus"
	if err := client.Copy(ctx, buckets.Audio, key, buckets.Clips, clipKey); err != nil {
		t.Fatalf("copy: %v", err)
	}
	copied, err := client.Head(ctx, buckets.Clips, clipKey)
	if err != nil {
		t.Fatalf("head copy: %v", err)
	}
	if copied.Size != info.Size {
		t.Fatalf("copy size %d, want %d", copied.Size, info.Size)
	}

	if err := client.Delete(ctx, buckets.Audio, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := client.Head(ctx, buckets.Audio, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("head after delete = %v, want ErrNotFound", err)
	}
	if err := client.Delete(ctx, buckets.Audio, key); err != nil {
		t.Fatalf("delete is not idempotent: %v", err)
	}
}

func TestS3CopyReportsAMissingSource(t *testing.T) {
	client, buckets := storagetest.New(t)

	err := client.Copy(t.Context(), buckets.Audio, "ws/none/missing.opus", buckets.Clips, "copy.opus")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("copy missing source = %v, want ErrNotFound", err)
	}
}

func TestS3PresignedUploadExpires(t *testing.T) {
	client, buckets := storagetest.New(t)

	upload, err := client.PresignUpload(t.Context(), buckets.Transcripts, "ws/expiry/transcript.json", "application/json", 16, time.Second)
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	time.Sleep(2 * time.Second)

	request, err := http.NewRequestWithContext(t.Context(), upload.Method, upload.URL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for name, value := range upload.Headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode < 400 {
		t.Fatalf("an expired upload url was accepted with status %d", response.StatusCode)
	}
}

func put(t *testing.T, upload storage.PresignedRequest, body []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), upload.Method, upload.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build upload request: %v", err)
	}
	for name, value := range upload.Headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send upload request: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("upload returned %d: %s", response.StatusCode, payload)
	}
}

func get(t *testing.T, url string) []byte {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build download request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send download request: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("download returned %d", response.StatusCode)
	}
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read download body: %v", err)
	}
	return payload
}
