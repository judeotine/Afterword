package integrations

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotConfigured   = errors.New("integrations: provider is not configured")
	ErrUnknownProvider = errors.New("integrations: provider is not known")
	ErrProviderExists  = errors.New("integrations: provider is already registered")
	ErrInvalidDelivery = errors.New("integrations: delivery is not valid")
	ErrDeliveryFailed  = errors.New("integrations: provider could not accept the delivery")
)

type Delivery struct {
	WorkspaceID uuid.UUID
	MeetingID   uuid.UUID
	Title       string
	Summary     string
	Highlights  []string
	MeetingURL  string
	OccurredAt  time.Time
}

func (d Delivery) Validate() error {
	if d.WorkspaceID == uuid.Nil {
		return ErrInvalidDelivery
	}
	if d.MeetingID == uuid.Nil {
		return ErrInvalidDelivery
	}
	if strings.TrimSpace(d.Title) == "" && strings.TrimSpace(d.Summary) == "" {
		return ErrInvalidDelivery
	}
	return nil
}

type Provider interface {
	Name() string
	Deliver(ctx context.Context, delivery Delivery) error
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
