package askprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Passage struct {
	MeetingID    string
	MeetingTitle string
	StartS       float64
	Text         string
}

type Request struct {
	Question string
	Passages []Passage
}

type Provider interface {
	Answer(ctx context.Context, request Request) (string, error)
	Name() string
}

type Offline struct{}

func (Offline) Name() string { return "offline" }

func (Offline) Answer(_ context.Context, request Request) (string, error) {
	if len(request.Passages) == 0 {
		return "No matching moments were found in this workspace.", nil
	}
	var b strings.Builder
	b.WriteString("Based on the meetings in this workspace:\n\n")
	for _, passage := range request.Passages {
		b.WriteString(fmt.Sprintf("- %s (at %.0fs): %s\n", passage.MeetingTitle, passage.StartS, passage.Text))
	}
	return b.String(), nil
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
}

type Ollama struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (o *Ollama) Name() string { return "ollama" }

func (o *Ollama) Answer(ctx context.Context, request Request) (string, error) {
	var excerpts strings.Builder
	for _, passage := range request.Passages {
		excerpts.WriteString(fmt.Sprintf("[%s @ %.0fs] %s\n", passage.MeetingTitle, passage.StartS, passage.Text))
	}
	system := "You answer questions about a team's meetings. Use only the provided excerpts. Cite the meeting title and timestamp for each claim. If the excerpts do not answer the question, say so."
	user := fmt.Sprintf("Question: %s\n\nExcerpts:\n%s", request.Question, excerpts.String())

	body, err := json.Marshal(ollamaRequest{
		Model: o.Model,
		Messages: []ollamaMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("encode ollama request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}
	var parsed ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}
	return parsed.Message.Content, nil
}
