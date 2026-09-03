package meetings_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/meetings"
)

func TestCursorRoundTrips(t *testing.T) {
	at := time.Date(2026, 9, 4, 12, 30, 15, 123456789, time.UTC)
	id := uuid.New()

	decodedAt, decodedID, err := meetings.DecodeCursor(meetings.EncodeCursor(at, id))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !decodedAt.Equal(at) || decodedID != id {
		t.Fatalf("round trip produced %v / %v", decodedAt, decodedID)
	}
}

func TestCursorRejectsRubbish(t *testing.T) {
	for _, value := range []string{"", "   ", "not base64!", "aGVsbG8", "MjAyNi0wOS0wNHxub3QtYS11dWlk"} {
		if _, _, err := meetings.DecodeCursor(value); !errors.Is(err, meetings.ErrInvalidCursor) {
			t.Fatalf("cursor %q returned %v, want ErrInvalidCursor", value, err)
		}
	}
}
