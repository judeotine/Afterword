package meetings

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

func (s *Service) CreateFolder(ctx context.Context, actor auth.Membership, name string, parentID *uuid.UUID) (Folder, error) {
	if parentID != nil {
		if _, err := s.folder(ctx, actor.WorkspaceID, *parentID); err != nil {
			return Folder{}, err
		}
	}
	row, err := s.queries.CreateFolder(ctx, sqlcgen.CreateFolderParams{
		WorkspaceID: actor.WorkspaceID,
		Name:        name,
		ParentID:    parentID,
	})
	if err != nil {
		if isForeignKeyViolation(err) {
			return Folder{}, ErrFolderNotFound
		}
		return Folder{}, fmt.Errorf("create folder: %w", err)
	}
	return folderFromRow(row), nil
}

func (s *Service) ListFolders(ctx context.Context, actor auth.Membership) ([]Folder, error) {
	rows, err := s.queries.ListFolders(ctx, actor.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	folders := make([]Folder, 0, len(rows))
	for _, row := range rows {
		folders = append(folders, folderFromRow(row))
	}
	return folders, nil
}

type UpdateFolderParams struct {
	Name        *string
	ParentID    *uuid.UUID
	ClearParent bool
}

func (s *Service) UpdateFolder(ctx context.Context, actor auth.Membership, folderID uuid.UUID, params UpdateFolderParams) (Folder, error) {
	if _, err := s.folder(ctx, actor.WorkspaceID, folderID); err != nil {
		return Folder{}, err
	}
	if params.ParentID != nil && !params.ClearParent {
		if *params.ParentID == folderID {
			return Folder{}, ErrFolderCycle
		}
		if _, err := s.folder(ctx, actor.WorkspaceID, *params.ParentID); err != nil {
			return Folder{}, err
		}
		if err := s.rejectCycle(ctx, actor.WorkspaceID, folderID, *params.ParentID); err != nil {
			return Folder{}, err
		}
	}

	row, err := s.queries.UpdateFolderDetails(ctx, sqlcgen.UpdateFolderDetailsParams{
		ID:          folderID,
		WorkspaceID: actor.WorkspaceID,
		Name:        params.Name,
		ParentID:    params.ParentID,
		ClearParent: params.ClearParent,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Folder{}, ErrFolderNotFound
		}
		return Folder{}, fmt.Errorf("update folder: %w", err)
	}
	return folderFromRow(row), nil
}

func (s *Service) DeleteFolder(ctx context.Context, actor auth.Membership, folderID uuid.UUID) error {
	if _, err := s.folder(ctx, actor.WorkspaceID, folderID); err != nil {
		return err
	}
	children, err := s.queries.CountChildFolders(ctx, sqlcgen.CountChildFoldersParams{
		WorkspaceID: actor.WorkspaceID,
		ParentID:    &folderID,
	})
	if err != nil {
		return fmt.Errorf("count child folders: %w", err)
	}
	if children > 0 {
		return ErrFolderHasChildren
	}

	removed, err := s.queries.DeleteFolder(ctx, sqlcgen.DeleteFolderParams{ID: folderID, WorkspaceID: actor.WorkspaceID})
	if err != nil {
		return fmt.Errorf("delete folder: %w", err)
	}
	if removed == 0 {
		return ErrFolderNotFound
	}
	return nil
}

func (s *Service) folder(ctx context.Context, workspaceID, folderID uuid.UUID) (Folder, error) {
	row, err := s.queries.GetFolder(ctx, sqlcgen.GetFolderParams{ID: folderID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Folder{}, ErrFolderNotFound
		}
		return Folder{}, fmt.Errorf("read folder: %w", err)
	}
	return folderFromRow(row), nil
}

func (s *Service) rejectCycle(ctx context.Context, workspaceID, folderID, parentID uuid.UUID) error {
	current := parentID
	for depth := 0; depth < 64; depth++ {
		folder, err := s.folder(ctx, workspaceID, current)
		if err != nil {
			return err
		}
		if folder.ParentID == nil {
			return nil
		}
		if *folder.ParentID == folderID {
			return ErrFolderCycle
		}
		current = *folder.ParentID
	}
	return ErrFolderCycle
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation
}
