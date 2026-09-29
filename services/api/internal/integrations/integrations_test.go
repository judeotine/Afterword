package integrations

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type stubDoer struct {
	lastURL     string
	lastBody    string
	lastHeaders http.Header
	status      int
	err         error
}

func (s *stubDoer) Do(req *http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	body, _ := io.ReadAll(req.Body)
	s.lastURL = req.URL.String()
	s.lastBody = string(body)
	s.lastHeaders = req.Header.Clone()
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}

func sampleDelivery() Delivery {
	return Delivery{
		WorkspaceID: uuid.New(),
		MeetingID:   uuid.New(),
		Title:       "Weekly sync",
		Summary:     "We agreed on the launch plan.",
		Highlights:  []string{"Ship on Friday", "Owner is Ada"},
		MeetingURL:  "https://afterword.example/m/1",
	}
}

func TestFakeProviderRecordsDeliveries(t *testing.T) {
	fake := NewFakeProvider("slack")
	registry := NewRegistry()
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.DeliverTo(context.Background(), "slack", sampleDelivery()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if got := len(fake.Delivered()); got != 1 {
		t.Fatalf("expected 1 delivery, got %d", got)
	}
}

func TestRegistryRejectsUnknownProvider(t *testing.T) {
	registry := NewRegistry()
	err := registry.DeliverTo(context.Background(), "missing", sampleDelivery())
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}

func TestRegistryRejectsInvalidDelivery(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(NewFakeProvider("slack"))
	err := registry.DeliverTo(context.Background(), "slack", Delivery{})
	if !errors.Is(err, ErrInvalidDelivery) {
		t.Fatalf("expected invalid delivery error, got %v", err)
	}
}

func TestSlackProviderPostsText(t *testing.T) {
	stub := &stubDoer{}
	provider := NewSlackProvider("https://hooks.slack.example/abc", stub)
	if err := provider.Deliver(context.Background(), sampleDelivery()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if !strings.Contains(stub.lastBody, "Weekly sync") {
		t.Fatalf("expected title in body, got %q", stub.lastBody)
	}
	if !strings.Contains(stub.lastBody, "Ship on Friday") {
		t.Fatalf("expected highlight in body, got %q", stub.lastBody)
	}
}

func TestNotionProviderSetsAuthHeaders(t *testing.T) {
	stub := &stubDoer{}
	provider := NewNotionProvider("secret-token", "db-123", stub)
	if err := provider.Deliver(context.Background(), sampleDelivery()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if stub.lastHeaders.Get("authorization") != "Bearer secret-token" {
		t.Fatalf("expected bearer auth, got %q", stub.lastHeaders.Get("authorization"))
	}
	if stub.lastHeaders.Get("notion-version") == "" {
		t.Fatalf("expected notion version header")
	}
	if !strings.Contains(stub.lastBody, "db-123") {
		t.Fatalf("expected database id in body, got %q", stub.lastBody)
	}
}

func TestHubSpotProviderPostsNote(t *testing.T) {
	stub := &stubDoer{}
	provider := NewHubSpotProvider("hs-token", stub)
	if err := provider.Deliver(context.Background(), sampleDelivery()); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if stub.lastHeaders.Get("authorization") != "Bearer hs-token" {
		t.Fatalf("expected bearer auth, got %q", stub.lastHeaders.Get("authorization"))
	}
	if !strings.Contains(stub.lastURL, "crm/v3/objects/notes") {
		t.Fatalf("expected notes endpoint, got %q", stub.lastURL)
	}
}

func TestProviderFailsOnHTTPError(t *testing.T) {
	stub := &stubDoer{status: http.StatusInternalServerError}
	provider := NewSlackProvider("https://hooks.slack.example/abc", stub)
	err := provider.Deliver(context.Background(), sampleDelivery())
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("expected delivery failed, got %v", err)
	}
}

func TestProviderNotConfigured(t *testing.T) {
	provider := NewSlackProvider("", &stubDoer{})
	err := provider.Deliver(context.Background(), sampleDelivery())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected not configured, got %v", err)
	}
}

func TestRegistryFromConfigRegistersConfiguredProviders(t *testing.T) {
	registry := RegistryFromConfig(EnvConfig{
		SlackWebhookURL: "https://hooks.slack.example/abc",
		HubSpotToken:    "hs-token",
	})
	names := registry.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 providers, got %v", names)
	}
}
