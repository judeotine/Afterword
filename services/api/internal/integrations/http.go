package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func defaultHTTPClient() httpDoer {
	return &http.Client{Timeout: 15 * time.Second}
}

func postJSON(ctx context.Context, client httpDoer, url string, headers map[string]string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDelivery, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeliveryFailed, err)
	}
	req.Header.Set("content-type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeliveryFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("%w: status %d %s", ErrDeliveryFailed, resp.StatusCode, strings.TrimSpace(string(snippet)))
}

func renderLines(delivery Delivery) string {
	var builder strings.Builder
	if strings.TrimSpace(delivery.Title) != "" {
		builder.WriteString(delivery.Title)
		builder.WriteString("\n\n")
	}
	if strings.TrimSpace(delivery.Summary) != "" {
		builder.WriteString(delivery.Summary)
		builder.WriteString("\n")
	}
	for _, highlight := range delivery.Highlights {
		if strings.TrimSpace(highlight) == "" {
			continue
		}
		builder.WriteString("- ")
		builder.WriteString(highlight)
		builder.WriteString("\n")
	}
	if strings.TrimSpace(delivery.MeetingURL) != "" {
		builder.WriteString("\n")
		builder.WriteString(delivery.MeetingURL)
	}
	return strings.TrimSpace(builder.String())
}
