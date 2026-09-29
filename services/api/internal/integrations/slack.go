package integrations

import "context"

type SlackProvider struct {
	webhookURL string
	client     httpDoer
}

func NewSlackProvider(webhookURL string, client httpDoer) *SlackProvider {
	if client == nil {
		client = defaultHTTPClient()
	}
	return &SlackProvider{webhookURL: webhookURL, client: client}
}

func (s *SlackProvider) Name() string {
	return "slack"
}

func (s *SlackProvider) Deliver(ctx context.Context, delivery Delivery) error {
	if s.webhookURL == "" {
		return ErrNotConfigured
	}
	body := map[string]any{
		"text": renderLines(delivery),
	}
	return postJSON(ctx, s.client, s.webhookURL, nil, body)
}
