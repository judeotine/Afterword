package askprovider

import (
	"context"

	"github.com/judeotine/afterword/services/api/internal/meetings"
)

type meetingsAdapter struct {
	inner Provider
}

func (a meetingsAdapter) Name() string { return a.inner.Name() }

func (a meetingsAdapter) Answer(ctx context.Context, request meetings.AskRequest) (string, error) {
	passages := make([]Passage, 0, len(request.Passages))
	for _, passage := range request.Passages {
		passages = append(passages, Passage{
			MeetingID:    passage.MeetingID,
			MeetingTitle: passage.MeetingTitle,
			StartS:       passage.StartS,
			Text:         passage.Text,
		})
	}
	return a.inner.Answer(ctx, Request{Question: request.Question, Passages: passages})
}

func ForMeetings(inner Provider) meetings.AskProvider {
	if inner == nil {
		return nil
	}
	return meetingsAdapter{inner: inner}
}

func FromEnv(provider, ollamaURL, model string) Provider {
	switch provider {
	case "ollama":
		return NewOllama(ollamaURL, model)
	case "offline", "":
		return Offline{}
	default:
		return Offline{}
	}
}
