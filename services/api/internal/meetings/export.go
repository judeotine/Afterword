package meetings

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/integrations"
)

type IntegrationDelivery interface {
	DeliverTo(ctx context.Context, name string, delivery integrations.Delivery) error
}

func (s *Service) Export(ctx context.Context, actor auth.Membership, meetingID uuid.UUID, target string, registry IntegrationDelivery) error {
	if registry == nil {
		return integrations.ErrNotConfigured
	}
	if strings.TrimSpace(target) == "" {
		return integrations.ErrUnknownProvider
	}
	meeting, err := s.viewable(ctx, actor, meetingID)
	if err != nil {
		return err
	}
	delivery := integrations.Delivery{
		WorkspaceID: meeting.WorkspaceID,
		MeetingID:   meeting.ID,
		Title:       meeting.Title,
	}
	if meeting.StartedAt != nil {
		delivery.OccurredAt = *meeting.StartedAt
	}
	summary, err := s.latestSummary(ctx, meeting.WorkspaceID, meeting.ID)
	if err != nil {
		return err
	}
	if summary != nil {
		delivery.Summary = summary.Markdown
	}
	return registry.DeliverTo(ctx, target, delivery)
}

func (s *Service) latestSummary(ctx context.Context, workspaceID, meetingID uuid.UUID) (*sqlcgen.Summary, error) {
	summaries, err := s.queries.ListSummariesByWorkspace(ctx, sqlcgen.ListSummariesByWorkspaceParams{
		WorkspaceID: workspaceID,
		MeetingID:   uuidPtr(meetingID),
		PageSize:    1,
	})
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return nil, nil
	}
	return &summaries[0], nil
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	return &id
}
