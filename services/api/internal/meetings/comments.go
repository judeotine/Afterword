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

const MaxCommentLength = 5000

var (
	ErrCommentNotFound = errors.New("meetings: comment not found")
	ErrEmptyComment    = errors.New("meetings: comment body cannot be empty")
)

type Comment struct {
	ID        uuid.UUID
	MeetingID uuid.UUID
	UserID    *uuid.UUID
	AtS       float64
	Body      string
	CreatedAt time.Time
}

type CommentPage struct {
	Comments   []Comment
	NextCursor string
}

func commentFromRow(row sqlcgen.Comment) Comment {
	return Comment{
		ID:        row.ID,
		MeetingID: row.MeetingID,
		UserID:    row.UserID,
		AtS:       row.AtS,
		Body:      row.Body,
		CreatedAt: moment(row.CreatedAt),
	}
}

func (s *Service) CreateComment(ctx context.Context, actor auth.Membership, meetingID uuid.UUID, atS float64, body string) (Comment, error) {
	if _, err := s.viewable(ctx, actor, meetingID); err != nil {
		return Comment{}, err
	}
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return Comment{}, ErrEmptyComment
	}
	if atS < 0 {
		atS = 0
	}
	userID := actor.UserID
	row, err := s.queries.CreateComment(ctx, sqlcgen.CreateCommentParams{
		MeetingID:   meetingID,
		UserID:      &userID,
		AtS:         atS,
		Body:        trimmed,
		WorkspaceID: actor.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Comment{}, ErrMeetingNotFound
		}
		return Comment{}, fmt.Errorf("create comment: %w", err)
	}
	return commentFromRow(row), nil
}

func (s *Service) ListComments(ctx context.Context, actor auth.Membership, meetingID uuid.UUID, cursor string, pageSize int32) (CommentPage, error) {
	if _, err := s.viewable(ctx, actor, meetingID); err != nil {
		return CommentPage{}, err
	}
	size := pageSize
	if size <= 0 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}

	params := sqlcgen.ListCommentsByWorkspaceParams{
		WorkspaceID: actor.WorkspaceID,
		MeetingID:   &meetingID,
		PageSize:    size + 1,
	}
	if cursor != "" {
		at, id, err := DecodeCursor(cursor)
		if err != nil {
			return CommentPage{}, err
		}
		params.CursorCreatedAt = optionalTimestamp(&at)
		params.CursorID = &id
	}

	rows, err := s.queries.ListCommentsByWorkspace(ctx, params)
	if err != nil {
		return CommentPage{}, fmt.Errorf("list comments: %w", err)
	}

	page := CommentPage{Comments: make([]Comment, 0, len(rows))}
	for index, row := range rows {
		if int32(index) == size {
			last := page.Comments[len(page.Comments)-1]
			page.NextCursor = EncodeCursor(last.CreatedAt, last.ID)
			break
		}
		page.Comments = append(page.Comments, commentFromRow(row))
	}
	return page, nil
}

func (s *Service) UpdateComment(ctx context.Context, actor auth.Membership, meetingID, commentID uuid.UUID, body *string, atS *float64) (Comment, error) {
	if _, err := s.viewable(ctx, actor, meetingID); err != nil {
		return Comment{}, err
	}
	existing, err := s.queries.GetComment(ctx, sqlcgen.GetCommentParams{ID: commentID, WorkspaceID: actor.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Comment{}, ErrCommentNotFound
		}
		return Comment{}, fmt.Errorf("load comment: %w", err)
	}
	if existing.MeetingID != meetingID {
		return Comment{}, ErrCommentNotFound
	}
	if !s.mayModifyComment(actor, existing) {
		return Comment{}, ErrNotPermitted
	}

	var trimmed *string
	if body != nil {
		value := strings.TrimSpace(*body)
		if value == "" {
			return Comment{}, ErrEmptyComment
		}
		trimmed = &value
	}

	row, err := s.queries.UpdateComment(ctx, sqlcgen.UpdateCommentParams{
		AtS:         atS,
		Body:        trimmed,
		ID:          commentID,
		WorkspaceID: actor.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Comment{}, ErrCommentNotFound
		}
		return Comment{}, fmt.Errorf("update comment: %w", err)
	}
	return commentFromRow(row), nil
}

func (s *Service) DeleteComment(ctx context.Context, actor auth.Membership, meetingID, commentID uuid.UUID) error {
	if _, err := s.viewable(ctx, actor, meetingID); err != nil {
		return err
	}
	existing, err := s.queries.GetComment(ctx, sqlcgen.GetCommentParams{ID: commentID, WorkspaceID: actor.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCommentNotFound
		}
		return fmt.Errorf("load comment: %w", err)
	}
	if existing.MeetingID != meetingID {
		return ErrCommentNotFound
	}
	if !s.mayModifyComment(actor, existing) {
		return ErrNotPermitted
	}
	affected, err := s.queries.DeleteComment(ctx, sqlcgen.DeleteCommentParams{ID: commentID, WorkspaceID: actor.WorkspaceID})
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	if affected == 0 {
		return ErrCommentNotFound
	}
	return nil
}

func (s *Service) mayModifyComment(actor auth.Membership, comment sqlcgen.Comment) bool {
	if comment.UserID != nil && *comment.UserID == actor.UserID {
		return true
	}
	return actor.Role == auth.RoleOwner || actor.Role == auth.RoleAdmin
}
