package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type memoryObject struct {
	data         []byte
	contentType  string
	lastModified time.Time
}

type Memory struct {
	mu       sync.RWMutex
	objects  map[string]memoryObject
	baseURL  string
	clock    func() time.Time
	presigns []PresignRecord
	deletes  []ObjectRef
}

type ObjectRef struct {
	Bucket string
	Key    string
}

type PresignRecord struct {
	ObjectRef
	Method      string
	ContentType string
	MaxBytes    int64
	SizeBytes   int64
	TTL         time.Duration
}

var _ Client = (*Memory)(nil)

func NewMemory() *Memory {
	return &Memory{
		objects: map[string]memoryObject{},
		baseURL: "https://storage.test",
		clock:   func() time.Time { return time.Now().UTC() },
	}
}

func (m *Memory) SetClock(clock func() time.Time) {
	if clock == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clock = clock
}

func (m *Memory) Put(bucket, key string, data []byte, contentType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := make([]byte, len(data))
	copy(stored, data)
	m.objects[reference(bucket, key)] = memoryObject{data: stored, contentType: contentType, lastModified: m.clock()}
}

func (m *Memory) Exists(bucket, key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.objects[reference(bucket, key)]
	return ok
}

func (m *Memory) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.objects))
	for key := range m.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (m *Memory) Presigns() []PresignRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]PresignRecord(nil), m.presigns...)
}

func (m *Memory) Deletes() []ObjectRef {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]ObjectRef(nil), m.deletes...)
}

func (m *Memory) PresignUpload(_ context.Context, upload UploadRequest) (PresignedRequest, error) {
	if err := validate(upload.Bucket, upload.Key); err != nil {
		return PresignedRequest{}, err
	}
	window := normalizeTTL(upload.TTL)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.presigns = append(m.presigns, PresignRecord{
		ObjectRef:   ObjectRef{Bucket: upload.Bucket, Key: upload.Key},
		Method:      "PUT",
		ContentType: upload.ContentType,
		MaxBytes:    upload.MaxBytes,
		SizeBytes:   upload.SizeBytes,
		TTL:         window,
	})

	request := PresignedRequest{
		Method:    "PUT",
		URL:       m.signedURL(upload.Bucket, upload.Key, "PUT", window),
		MaxBytes:  upload.MaxBytes,
		SizeBytes: upload.SizeBytes,
		ExpiresAt: m.clock().Add(window),
	}
	if trimmed := strings.TrimSpace(upload.ContentType); trimmed != "" {
		request.Headers = map[string]string{"Content-Type": trimmed}
	}
	return request, nil
}

func (m *Memory) PutSigned(bucket, key string, data []byte, contentType string) error {
	if err := validate(bucket, key); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	signed := int64(0)
	for i := len(m.presigns) - 1; i >= 0; i-- {
		record := m.presigns[i]
		if record.Method == "PUT" && record.Bucket == bucket && record.Key == key {
			signed = record.SizeBytes
			break
		}
	}
	if signed > 0 && int64(len(data)) != signed {
		return fmt.Errorf("%w: %s/%s was signed for %d bytes and the upload carried %d",
			ErrSizeMismatch, bucket, key, signed, len(data))
	}

	stored := make([]byte, len(data))
	copy(stored, data)
	m.objects[reference(bucket, key)] = memoryObject{data: stored, contentType: contentType, lastModified: m.clock()}
	return nil
}

func (m *Memory) PresignDownload(_ context.Context, bucket, key string, ttl time.Duration) (PresignedRequest, error) {
	if err := validate(bucket, key); err != nil {
		return PresignedRequest{}, err
	}
	window := normalizeTTL(ttl)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.presigns = append(m.presigns, PresignRecord{
		ObjectRef: ObjectRef{Bucket: bucket, Key: key},
		Method:    "GET",
		TTL:       window,
	})
	return PresignedRequest{
		Method:    "GET",
		URL:       m.signedURL(bucket, key, "GET", window),
		ExpiresAt: m.clock().Add(window),
	}, nil
}

func (m *Memory) Delete(_ context.Context, bucket, key string) error {
	if err := validate(bucket, key); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, reference(bucket, key))
	m.deletes = append(m.deletes, ObjectRef{Bucket: bucket, Key: key})
	return nil
}

func (m *Memory) Head(_ context.Context, bucket, key string) (ObjectInfo, error) {
	if err := validate(bucket, key); err != nil {
		return ObjectInfo{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	object, ok := m.objects[reference(bucket, key)]
	if !ok {
		return ObjectInfo{}, ErrNotFound
	}
	sum := md5.Sum(object.data)
	return ObjectInfo{
		Bucket:       bucket,
		Key:          key,
		Size:         int64(len(object.data)),
		ContentType:  object.contentType,
		ETag:         hex.EncodeToString(sum[:]),
		LastModified: object.lastModified,
	}, nil
}

func (m *Memory) Copy(_ context.Context, sourceBucket, sourceKey, targetBucket, targetKey string) error {
	if err := validate(sourceBucket, sourceKey); err != nil {
		return err
	}
	if err := validate(targetBucket, targetKey); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	object, ok := m.objects[reference(sourceBucket, sourceKey)]
	if !ok {
		return ErrNotFound
	}
	stored := make([]byte, len(object.data))
	copy(stored, object.data)
	m.objects[reference(targetBucket, targetKey)] = memoryObject{
		data:         stored,
		contentType:  object.contentType,
		lastModified: m.clock(),
	}
	return nil
}

type failingClient struct {
	Client
	err error
}

func NewFailing(err error) Client {
	if err == nil {
		err = errors.New("storage: failure injected by the test double")
	}
	return &failingClient{Client: NewMemory(), err: err}
}

func (f *failingClient) PresignUpload(context.Context, UploadRequest) (PresignedRequest, error) {
	return PresignedRequest{}, f.err
}

func (f *failingClient) PresignDownload(context.Context, string, string, time.Duration) (PresignedRequest, error) {
	return PresignedRequest{}, f.err
}

func (m *Memory) signedURL(bucket, key, method string, ttl time.Duration) string {
	query := url.Values{}
	query.Set("method", method)
	query.Set("expires", fmt.Sprintf("%d", m.clock().Add(ttl).Unix()))
	return m.baseURL + "/" + bucket + "/" + key + "?" + query.Encode()
}

func reference(bucket, key string) string {
	return bucket + "/" + key
}
