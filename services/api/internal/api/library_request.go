package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/validation"
)

const maxTranscriptBody = 48 << 20

func decodeTranscriptJSON(w http.ResponseWriter, r *http.Request, target any) error {
	if r.Body == nil {
		return errBadRequestBody
	}
	reader := http.MaxBytesReader(w, r.Body, maxTranscriptBody)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errBadRequestBody
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errBadRequestBody
	}
	return nil
}

func queryString(r *http.Request, name string) string {
	return strings.TrimSpace(r.URL.Query().Get(name))
}

func queryInt32(v *validation.Validator, r *http.Request, name string, fallback, maximum int32) int32 {
	raw := queryString(r, name)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || parsed <= 0 {
		v.Add(name, "must be a positive number")
		return fallback
	}
	if int32(parsed) > maximum {
		return maximum
	}
	return int32(parsed)
}

func queryOptionalInt32(v *validation.Validator, r *http.Request, name string) *int32 {
	raw := queryString(r, name)
	if raw == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || parsed < 0 {
		v.Add(name, "must be a number of zero or more")
		return nil
	}
	value := int32(parsed)
	return &value
}

func queryUUID(v *validation.Validator, r *http.Request, name string) *uuid.UUID {
	raw := queryString(r, name)
	if raw == "" {
		return nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil {
		v.Add(name, "is not a valid identifier")
		return nil
	}
	return &parsed
}

func queryTime(v *validation.Validator, r *http.Request, name string) *time.Time {
	raw := queryString(r, name)
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		v.Add(name, "must be an RFC 3339 timestamp")
		return nil
	}
	value := parsed.UTC()
	return &value
}

func optionalTime(v *validation.Validator, field, raw string) *time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		v.Add(field, "must be an RFC 3339 timestamp")
		return nil
	}
	value := parsed.UTC()
	return &value
}
