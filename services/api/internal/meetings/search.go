package meetings

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

const MaxSearchQueryLength = 500

type SearchHit struct {
	MeetingID    uuid.UUID
	MeetingTitle string
	Seq          int32
	Speaker      string
	StartS       float64
	EndS         float64
	Text         string
	Rank         float32
}

type SearchResults struct {
	Query string
	Hits  []SearchHit
}

func (s *Service) SearchSegments(ctx context.Context, actor auth.Membership, query string, folderID *uuid.UUID, pageSize int32) (SearchResults, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return SearchResults{Query: "", Hits: []SearchHit{}}, nil
	}

	size := pageSize
	if size <= 0 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}

	rows, err := s.queries.SearchSegments(ctx, sqlcgen.SearchSegmentsParams{
		Query:        trimmed,
		WorkspaceID:  actor.WorkspaceID,
		ViewerUserID: &actor.UserID,
		FolderID:     folderID,
		PageSize:     size,
	})
	if err != nil {
		return SearchResults{}, fmt.Errorf("search transcript segments: %w", err)
	}

	hits := make([]SearchHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, SearchHit{
			MeetingID:    row.MeetingID,
			MeetingTitle: row.MeetingTitle,
			Seq:          row.Seq,
			Speaker:      text(row.Speaker),
			StartS:       row.StartS,
			EndS:         row.EndS,
			Text:         row.Text,
			Rank:         row.Rank,
		})
	}
	return SearchResults{Query: trimmed, Hits: hits}, nil
}
