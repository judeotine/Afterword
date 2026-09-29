package integrations

import (
	"context"
	"strings"
)

type HubSpotProvider struct {
	token  string
	client httpDoer
}

func NewHubSpotProvider(token string, client httpDoer) *HubSpotProvider {
	if client == nil {
		client = defaultHTTPClient()
	}
	return &HubSpotProvider{token: token, client: client}
}

func (h *HubSpotProvider) Name() string {
	return "hubspot"
}

func (h *HubSpotProvider) Deliver(ctx context.Context, delivery Delivery) error {
	if h.token == "" {
		return ErrNotConfigured
	}
	title := strings.TrimSpace(delivery.Title)
	if title == "" {
		title = "Meeting summary"
	}
	headers := map[string]string{
		"authorization": "Bearer " + h.token,
	}
	body := map[string]any{
		"properties": map[string]any{
			"hs_note_body":      renderLines(delivery),
			"hs_timestamp":      deliveryTimestamp(delivery),
			"hs_attachment_ids": "",
		},
	}
	return postJSON(ctx, h.client, "https://api.hubapi.com/crm/v3/objects/notes", headers, body)
}

func deliveryTimestamp(delivery Delivery) int64 {
	if delivery.OccurredAt.IsZero() {
		return 0
	}
	return delivery.OccurredAt.UnixMilli()
}
