package meetings

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

const (
	MaxAskQuestionLength = 1000
	AskCreditCost        = 1
	askRetrievalLimit    = 8
)

var (
	ErrAskUnavailable = errors.New("meetings: ask is not configured")
	ErrEmptyQuestion  = errors.New("meetings: question cannot be empty")
)

type AskCitation struct {
	MeetingID    uuid.UUID
	MeetingTitle string
	StartS       float64
	Text         string
}

type AskPassage struct {
	MeetingID    string
	MeetingTitle string
	StartS       float64
	Text         string
}

type AskRequest struct {
	Question string
	Passages []AskPassage
}

type AskProvider interface {
	Answer(ctx context.Context, request AskRequest) (string, error)
	Name() string
}

type AskResult struct {
	Answer    string
	Citations []AskCitation
}

func (s *Service) Ask(ctx context.Context, actor auth.Membership, question string) (AskResult, error) {
	if s.ask == nil {
		return AskResult{}, ErrAskUnavailable
	}
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return AskResult{}, ErrEmptyQuestion
	}

	rows, err := s.queries.SearchSegments(ctx, sqlcgen.SearchSegmentsParams{
		Query:        trimmed,
		WorkspaceID:  actor.WorkspaceID,
		ViewerUserID: &actor.UserID,
		FolderID:     nil,
		PageSize:     askRetrievalLimit,
	})
	if err != nil {
		return AskResult{}, fmt.Errorf("retrieve segments for ask: %w", err)
	}

	citations := make([]AskCitation, 0, len(rows))
	passages := make([]AskPassage, 0, len(rows))
	for _, row := range rows {
		citations = append(citations, AskCitation{
			MeetingID:    row.MeetingID,
			MeetingTitle: row.MeetingTitle,
			StartS:       row.StartS,
			Text:         row.Text,
		})
		passages = append(passages, AskPassage{
			MeetingID:    row.MeetingID.String(),
			MeetingTitle: row.MeetingTitle,
			StartS:       row.StartS,
			Text:         row.Text,
		})
	}

	if s.credits != nil {
		if _, err := s.credits.Require(ctx, actor.WorkspaceID, AskCreditCost); err != nil {
			return AskResult{}, err
		}
	}

	answer, err := s.ask.Answer(ctx, AskRequest{Question: trimmed, Passages: passages})
	if err != nil {
		return AskResult{}, fmt.Errorf("ask provider failed: %w", err)
	}

	if s.credits != nil {
		refID := "ask:" + actor.WorkspaceID.String() + ":" + uuid.NewString()
		if _, err := s.credits.Debit(ctx, actor.WorkspaceID, AskCreditCost, credits.ReasonAskUsage, refID); err != nil {
			return AskResult{}, err
		}
	}

	return AskResult{Answer: answer, Citations: citations}, nil
}
