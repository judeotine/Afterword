package meetings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

const (
	MaxKeywordPhraseLength = 200

	AlertChannelEmail    = "email"
	AlertChannelSlack    = "slack"
	AlertChannelWhatsapp = "whatsapp"
)

var AlertChannels = []string{AlertChannelEmail, AlertChannelSlack, AlertChannelWhatsapp}

var (
	ErrKeywordAlertNotFound = errors.New("meetings: keyword alert not found")
	ErrEmptyPhrase          = errors.New("meetings: keyword alert phrase cannot be empty")
	ErrInvalidChannel       = errors.New("meetings: keyword alert channel is not valid")
)

type KeywordAlert struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Phrase      string
	Channel     string
	CreatedAt   time.Time
}

type KeywordAlertPage struct {
	Alerts     []KeywordAlert
	NextCursor string
}

func keywordAlertFromRow(row sqlcgen.KeywordAlert) KeywordAlert {
	return KeywordAlert{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		UserID:      row.UserID,
		Phrase:      row.Phrase,
		Channel:     row.Channel,
		CreatedAt:   moment(row.CreatedAt),
	}
}

func validChannel(channel string) bool {
	for _, candidate := range AlertChannels {
		if candidate == channel {
			return true
		}
	}
	return false
}

func (s *Service) CreateKeywordAlert(ctx context.Context, actor auth.Membership, phrase, channel string) (KeywordAlert, error) {
	trimmed := strings.TrimSpace(phrase)
	if trimmed == "" {
		return KeywordAlert{}, ErrEmptyPhrase
	}
	if !validChannel(channel) {
		return KeywordAlert{}, ErrInvalidChannel
	}
	row, err := s.queries.CreateKeywordAlert(ctx, sqlcgen.CreateKeywordAlertParams{
		WorkspaceID: actor.WorkspaceID,
		UserID:      actor.UserID,
		Phrase:      trimmed,
		Channel:     channel,
	})
	if err != nil {
		return KeywordAlert{}, fmt.Errorf("create keyword alert: %w", err)
	}
	return keywordAlertFromRow(row), nil
}

func (s *Service) ListKeywordAlerts(ctx context.Context, actor auth.Membership, cursor string, pageSize int32) (KeywordAlertPage, error) {
	size := pageSize
	if size <= 0 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}
	params := sqlcgen.ListKeywordAlertsByWorkspaceParams{
		WorkspaceID: actor.WorkspaceID,
		PageSize:    size + 1,
	}
	if cursor != "" {
		at, id, err := DecodeCursor(cursor)
		if err != nil {
			return KeywordAlertPage{}, err
		}
		params.CursorCreatedAt = optionalTimestamp(&at)
		params.CursorID = &id
	}
	rows, err := s.queries.ListKeywordAlertsByWorkspace(ctx, params)
	if err != nil {
		return KeywordAlertPage{}, fmt.Errorf("list keyword alerts: %w", err)
	}
	page := KeywordAlertPage{Alerts: make([]KeywordAlert, 0, len(rows))}
	for index, row := range rows {
		if int32(index) == size {
			last := page.Alerts[len(page.Alerts)-1]
			page.NextCursor = EncodeCursor(last.CreatedAt, last.ID)
			break
		}
		page.Alerts = append(page.Alerts, keywordAlertFromRow(row))
	}
	return page, nil
}

func (s *Service) UpdateKeywordAlert(ctx context.Context, actor auth.Membership, alertID uuid.UUID, phrase, channel *string) (KeywordAlert, error) {
	var trimmed *string
	if phrase != nil {
		value := strings.TrimSpace(*phrase)
		if value == "" {
			return KeywordAlert{}, ErrEmptyPhrase
		}
		trimmed = &value
	}
	if channel != nil && !validChannel(*channel) {
		return KeywordAlert{}, ErrInvalidChannel
	}
	row, err := s.queries.UpdateKeywordAlert(ctx, sqlcgen.UpdateKeywordAlertParams{
		Phrase:      trimmed,
		Channel:     channel,
		ID:          alertID,
		WorkspaceID: actor.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return KeywordAlert{}, ErrKeywordAlertNotFound
		}
		return KeywordAlert{}, fmt.Errorf("update keyword alert: %w", err)
	}
	return keywordAlertFromRow(row), nil
}

func (s *Service) DeleteKeywordAlert(ctx context.Context, actor auth.Membership, alertID uuid.UUID) error {
	affected, err := s.queries.DeleteKeywordAlert(ctx, sqlcgen.DeleteKeywordAlertParams{
		ID:          alertID,
		WorkspaceID: actor.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("delete keyword alert: %w", err)
	}
	if affected == 0 {
		return ErrKeywordAlertNotFound
	}
	return nil
}
