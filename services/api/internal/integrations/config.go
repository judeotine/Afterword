package integrations

import (
	"os"
	"strings"
)

type EnvConfig struct {
	SlackWebhookURL string
	NotionToken     string
	NotionDatabase  string
	HubSpotToken    string
}

func ConfigFromEnv() EnvConfig {
	return EnvConfig{
		SlackWebhookURL: strings.TrimSpace(os.Getenv("SLACK_WEBHOOK_URL")),
		NotionToken:     strings.TrimSpace(os.Getenv("NOTION_TOKEN")),
		NotionDatabase:  strings.TrimSpace(os.Getenv("NOTION_DATABASE_ID")),
		HubSpotToken:    strings.TrimSpace(os.Getenv("HUBSPOT_TOKEN")),
	}
}

func RegistryFromConfig(config EnvConfig) *Registry {
	registry := NewRegistry()
	if config.SlackWebhookURL != "" {
		_ = registry.Register(NewSlackProvider(config.SlackWebhookURL, nil))
	}
	if config.NotionToken != "" && config.NotionDatabase != "" {
		_ = registry.Register(NewNotionProvider(config.NotionToken, config.NotionDatabase, nil))
	}
	if config.HubSpotToken != "" {
		_ = registry.Register(NewHubSpotProvider(config.HubSpotToken, nil))
	}
	return registry
}
