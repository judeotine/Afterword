package integrations

import (
	"context"
	"strings"
)

type NotionProvider struct {
	token      string
	databaseID string
	client     httpDoer
	apiVersion string
}

func NewNotionProvider(token string, databaseID string, client httpDoer) *NotionProvider {
	if client == nil {
		client = defaultHTTPClient()
	}
	return &NotionProvider{token: token, databaseID: databaseID, client: client, apiVersion: "2022-06-28"}
}

func (n *NotionProvider) Name() string {
	return "notion"
}

func (n *NotionProvider) Deliver(ctx context.Context, delivery Delivery) error {
	if n.token == "" || n.databaseID == "" {
		return ErrNotConfigured
	}
	title := strings.TrimSpace(delivery.Title)
	if title == "" {
		title = "Meeting summary"
	}
	headers := map[string]string{
		"authorization":  "Bearer " + n.token,
		"notion-version": n.apiVersion,
	}
	body := map[string]any{
		"parent": map[string]any{"database_id": n.databaseID},
		"properties": map[string]any{
			"Name": map[string]any{
				"title": []any{
					map[string]any{"text": map[string]any{"content": title}},
				},
			},
		},
		"children": []any{
			map[string]any{
				"object": "block",
				"type":   "paragraph",
				"paragraph": map[string]any{
					"rich_text": []any{
						map[string]any{"type": "text", "text": map[string]any{"content": renderLines(delivery)}},
					},
				},
			},
		},
	}
	return postJSON(ctx, n.client, "https://api.notion.com/v1/pages", headers, body)
}
