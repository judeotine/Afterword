package billing

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
)

func EncodeCursor(at time.Time, id uuid.UUID) string {
	raw := at.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func DecodeCursor(value string) (time.Time, uuid.UUID, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	at, rest, found := strings.Cut(string(decoded), "|")
	if !found {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	parsedAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	parsedID, err := uuid.Parse(rest)
	if err != nil || parsedID == uuid.Nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	return parsedAt.UTC(), parsedID, nil
}
