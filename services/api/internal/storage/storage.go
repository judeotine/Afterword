package storage

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultAudioExtension = "opus"
	TranscriptFileName    = "transcript.json"
	DefaultPresignTTL     = 15 * time.Minute
	MaxPresignTTL         = 7 * 24 * time.Hour
)

var (
	ErrNotFound       = errors.New("storage: object not found")
	ErrBucketRequired = errors.New("storage: a bucket is required")
	ErrKeyRequired    = errors.New("storage: an object key is required")
	ErrSizeMismatch   = errors.New("storage: the upload does not match the signed content length")
)

var audioExtensions = map[string]struct{}{
	"opus": {}, "ogg": {}, "wav": {}, "mp3": {}, "m4a": {}, "flac": {}, "webm": {}, "mp4": {},
}

type ObjectInfo struct {
	Bucket       string
	Key          string
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
}

type PresignedRequest struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	MaxBytes  int64             `json:"max_bytes,omitempty"`
	SizeBytes int64             `json:"size_bytes,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type UploadRequest struct {
	Bucket      string
	Key         string
	ContentType string
	MaxBytes    int64
	SizeBytes   int64
	TTL         time.Duration
}

type Client interface {
	PresignUpload(ctx context.Context, request UploadRequest) (PresignedRequest, error)
	PresignDownload(ctx context.Context, bucket, key string, ttl time.Duration) (PresignedRequest, error)
	Delete(ctx context.Context, bucket, key string) error
	Head(ctx context.Context, bucket, key string) (ObjectInfo, error)
	Copy(ctx context.Context, sourceBucket, sourceKey, targetBucket, targetKey string) error
}

type Buckets struct {
	Audio       string
	Transcripts string
	Clips       string
	Exports     string
}

func (b Buckets) WithDefaults() Buckets {
	return Buckets{
		Audio:       fallback(b.Audio, "audio"),
		Transcripts: fallback(b.Transcripts, "transcripts"),
		Clips:       fallback(b.Clips, "clips"),
		Exports:     fallback(b.Exports, "exports"),
	}
}

func (b Buckets) Names() []string {
	filled := b.WithDefaults()
	return []string{filled.Audio, filled.Transcripts, filled.Clips, filled.Exports}
}

func MeetingPrefix(workspaceID, meetingID uuid.UUID) string {
	return "ws/" + workspaceID.String() + "/meetings/" + meetingID.String() + "/"
}

func AudioKey(workspaceID, meetingID uuid.UUID, extension string) string {
	return MeetingPrefix(workspaceID, meetingID) + "audio." + NormalizeAudioExtension(extension)
}

func TranscriptKey(workspaceID, meetingID uuid.UUID) string {
	return MeetingPrefix(workspaceID, meetingID) + TranscriptFileName
}

func ClipKey(workspaceID, meetingID, clipID uuid.UUID) string {
	return MeetingPrefix(workspaceID, meetingID) + "clips/" + clipID.String() + ".opus"
}

func NormalizeAudioExtension(extension string) string {
	trimmed := strings.ToLower(strings.TrimSpace(extension))
	trimmed = strings.TrimPrefix(trimmed, ".")
	if trimmed == "" || trimmed != path.Base(trimmed) {
		return DefaultAudioExtension
	}
	if _, ok := audioExtensions[trimmed]; !ok {
		return DefaultAudioExtension
	}
	return trimmed
}

func normalizeTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return DefaultPresignTTL
	}
	if ttl > MaxPresignTTL {
		return MaxPresignTTL
	}
	return ttl
}

func validate(bucket, key string) error {
	if strings.TrimSpace(bucket) == "" {
		return ErrBucketRequired
	}
	if strings.TrimSpace(key) == "" {
		return ErrKeyRequired
	}
	return nil
}

func fallback(value, alternative string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return alternative
	}
	return trimmed
}
