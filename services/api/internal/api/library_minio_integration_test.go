//go:build integration

package api_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
	"github.com/judeotine/afterword/services/api/internal/storagetest"
)

func TestFinalizeAndPurgeAgainstMinIO(t *testing.T) {
	client, buckets := storagetest.New(t)
	pool := dbtest.New(t)
	harness := buildLibraryHarnessWithBuckets(t, pool, client, buckets)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Recorded on MinIO", "source": "desktop"})
	audioKey := fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, created.Meeting.ID)
	transcriptKey := fmt.Sprintf("ws/%s/meetings/%s/transcript.json", owner.Workspace.ID, created.Meeting.ID)

	tooEarly := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if tooEarly.Status != http.StatusConflict {
		t.Fatalf("finalize before upload: status %d, body %s", tooEarly.Status, tooEarly.Body)
	}

	audio := bytes.Repeat([]byte("opus"), 1024)
	uploadTo(t, created.Upload.AudioURL, created.Upload.AudioHeaders, audio)
	transcript := []byte(`{"segments":[]}`)
	uploadTo(t, created.Upload.TranscriptURL, created.Upload.TranscriptHeaders, transcript)

	finalized := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if finalized.Status != http.StatusOK {
		t.Fatalf("finalize: status %d, body %s", finalized.Status, finalized.Body)
	}
	var result struct {
		Meeting meetingPayload `json:"meeting"`
		Queued  []string       `json:"queued"`
	}
	finalized.decode(t, &result)
	if result.Meeting.AudioBytes == nil || *result.Meeting.AudioBytes != int64(len(audio)) {
		t.Fatalf("audio bytes %+v, want %d", result.Meeting.AudioBytes, len(audio))
	}
	if result.Meeting.Transcript == nil || *result.Meeting.Transcript != int64(len(transcript)) {
		t.Fatalf("transcript bytes %+v, want %d", result.Meeting.Transcript, len(transcript))
	}
	if len(result.Queued) != 1 || result.Queued[0] != meetings.KindSummarise {
		t.Fatalf("queued %+v", result.Queued)
	}

	detail := harness.call(http.MethodGet, "/v1/meetings/"+created.Meeting.ID, nil, harness.as(owner)...)
	var view meetingDetailPayload
	detail.decode(t, &view)
	if view.Download.Audio == nil {
		t.Fatal("no audio download url after finalize")
	}
	if !bytes.Equal(download(t, view.Download.Audio.URL), audio) {
		t.Fatal("the presigned download url did not return the uploaded audio")
	}

	removed := harness.call(http.MethodDelete, "/v1/meetings/"+created.Meeting.ID, nil, harness.as(owner)...)
	if removed.Status != http.StatusNoContent {
		t.Fatalf("delete: status %d, body %s", removed.Status, removed.Body)
	}
	if _, err := client.Head(t.Context(), buckets.Audio, audioKey); err != nil {
		t.Fatalf("the delete request removed the object inline instead of queueing a purge: %v", err)
	}

	job, err := harness.queue.GetByIdempotencyKey(t.Context(), meetings.KindPurge, "purge:meeting:"+created.Meeting.ID)
	if err != nil {
		t.Fatalf("read purge job: %v", err)
	}
	handler := meetings.NewPurgeHandler(client)
	if err := handler(t.Context(), job); err != nil {
		t.Fatalf("run purge: %v", err)
	}
	for bucket, key := range map[string]string{buckets.Audio: audioKey, buckets.Transcripts: transcriptKey} {
		if _, err := client.Head(t.Context(), bucket, key); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("purge left %s/%s behind: %v", bucket, key, err)
		}
	}
	if err := handler(t.Context(), job); err != nil {
		t.Fatalf("purge is not idempotent: %v", err)
	}
}

func uploadTo(t *testing.T, url string, headers map[string]string, body []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build upload request: %v", err)
	}
	for name, value := range headers {
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

func download(t *testing.T, url string) []byte {
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
