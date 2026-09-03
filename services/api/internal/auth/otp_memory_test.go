package auth_test

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
)

type memoryOTPStore struct {
	mu      sync.Mutex
	records []auth.OTPRecord
}

func newMemoryOTPStore() *memoryOTPStore {
	return &memoryOTPStore{}
}

func (s *memoryOTPStore) CreateOTP(_ context.Context, params auth.CreateOTPParams) (auth.OTPRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := auth.OTPRecord{
		ID:          uuid.New(),
		Channel:     params.Channel,
		Destination: params.Destination,
		CodeHash:    params.CodeHash,
		ExpiresAt:   params.ExpiresAt,
		RequestIP:   params.RequestIP,
		CreatedAt:   params.CreatedAt,
	}
	s.records = append(s.records, record)
	return record, nil
}

func (s *memoryOTPStore) LatestOTP(_ context.Context, channel auth.Channel, destination string) (auth.OTPRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	matching := make([]auth.OTPRecord, 0, len(s.records))
	for _, record := range s.records {
		if record.Channel == channel && record.Destination == destination && record.ConsumedAt == nil {
			matching = append(matching, record)
		}
	}
	if len(matching) == 0 {
		return auth.OTPRecord{}, auth.ErrCodeNotFound
	}
	sort.Slice(matching, func(a, b int) bool {
		return matching[a].CreatedAt.After(matching[b].CreatedAt)
	})
	return matching[0], nil
}

func (s *memoryOTPStore) CountOTPsByDestination(_ context.Context, channel auth.Channel, destination string, since time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	for _, record := range s.records {
		if record.Channel == channel && record.Destination == destination && !record.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (s *memoryOTPStore) CountOTPsByIP(_ context.Context, ip string, since time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ip == "" {
		return 0, nil
	}
	var count int64
	for _, record := range s.records {
		if record.RequestIP == ip && !record.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (s *memoryOTPStore) RecordOTPAttempt(_ context.Context, id uuid.UUID) (int32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.records {
		if s.records[i].ID == id {
			s.records[i].Attempts++
			return s.records[i].Attempts, nil
		}
	}
	return 0, auth.ErrCodeNotFound
}

func (s *memoryOTPStore) ConsumeOTP(_ context.Context, id uuid.UUID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.records {
		if s.records[i].ID == id {
			if s.records[i].ConsumedAt != nil {
				return auth.ErrCodeNotFound
			}
			consumed := at
			s.records[i].ConsumedAt = &consumed
			return nil
		}
	}
	return auth.ErrCodeNotFound
}

func (s *memoryOTPStore) all() []auth.OTPRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]auth.OTPRecord(nil), s.records...)
}

type capturedMessage struct {
	Destination string
	Body        string
}

type recordingSender struct {
	mu       sync.Mutex
	emails   []capturedMessage
	texts    []capturedMessage
	emailErr error
	smsErr   error
}

func (s *recordingSender) SendEmail(_ context.Context, message auth.EmailMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emailErr != nil {
		return s.emailErr
	}
	s.emails = append(s.emails, capturedMessage{Destination: message.To, Body: message.Text})
	return nil
}

func (s *recordingSender) SendSMS(_ context.Context, to, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.smsErr != nil {
		return s.smsErr
	}
	s.texts = append(s.texts, capturedMessage{Destination: to, Body: text})
	return nil
}

func (s *recordingSender) sentEmails() []capturedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedMessage(nil), s.emails...)
}

func (s *recordingSender) sentTexts() []capturedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedMessage(nil), s.texts...)
}
