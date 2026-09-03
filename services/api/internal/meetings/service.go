package meetings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

const (
	DefaultUploadTTL     = 30 * time.Minute
	DefaultDownloadTTL   = 15 * time.Minute
	DefaultMaxAudioBytes = int64(2) << 30
	audioContentType     = "application/octet-stream"
	transcriptType       = "application/json"
)

type Enqueuer interface {
	Enqueue(ctx context.Context, kind string, payload any, runAt time.Time, idempotencyKey string) (*jobs.Job, error)
}

type ServiceOptions struct {
	Pool          *pgxpool.Pool
	Storage       storage.Client
	Buckets       storage.Buckets
	Jobs          Enqueuer
	UploadTTL     time.Duration
	DownloadTTL   time.Duration
	MaxAudioBytes int64
	Clock         func() time.Time
}

type Service struct {
	pool          *pgxpool.Pool
	queries       *sqlcgen.Queries
	storage       storage.Client
	buckets       storage.Buckets
	jobs          Enqueuer
	uploadTTL     time.Duration
	downloadTTL   time.Duration
	maxAudioBytes int64
	clock         func() time.Time
}

func NewService(options ServiceOptions) (*Service, error) {
	switch {
	case options.Pool == nil:
		return nil, errors.New("meetings: a database pool is required")
	case options.Storage == nil:
		return nil, errors.New("meetings: a storage client is required")
	case options.Jobs == nil:
		return nil, errors.New("meetings: a job queue is required")
	}

	service := &Service{
		pool:          options.Pool,
		queries:       sqlcgen.New(options.Pool),
		storage:       options.Storage,
		buckets:       options.Buckets.WithDefaults(),
		jobs:          options.Jobs,
		uploadTTL:     options.UploadTTL,
		downloadTTL:   options.DownloadTTL,
		maxAudioBytes: options.MaxAudioBytes,
		clock:         options.Clock,
	}
	if service.uploadTTL <= 0 {
		service.uploadTTL = DefaultUploadTTL
	}
	if service.downloadTTL <= 0 {
		service.downloadTTL = DefaultDownloadTTL
	}
	if service.maxAudioBytes <= 0 {
		service.maxAudioBytes = DefaultMaxAudioBytes
	}
	if service.clock == nil {
		service.clock = func() time.Time { return time.Now().UTC() }
	}
	return service, nil
}

func (s *Service) Buckets() storage.Buckets {
	return s.buckets
}

type CreateParams struct {
	Title          string
	Source         string
	Platform       string
	StartedAt      *time.Time
	DurationS      int32
	Visibility     string
	FolderID       *uuid.UUID
	AudioExtension string
}

func (s *Service) Create(ctx context.Context, actor auth.Membership, params CreateParams) (Created, error) {
	if params.FolderID != nil {
		if _, err := s.folder(ctx, actor.WorkspaceID, *params.FolderID); err != nil {
			return Created{}, err
		}
	}

	owner := actor.UserID
	visibility := params.Visibility
	if visibility == "" {
		visibility = VisibilityPrivate
	}

	row, err := s.queries.CreateMeeting(ctx, sqlcgen.CreateMeetingParams{
		WorkspaceID:  actor.WorkspaceID,
		OwnerUserID:  &owner,
		Title:        params.Title,
		Source:       params.Source,
		Platform:     optionalText(params.Platform),
		StartedAt:    optionalTimestamp(params.StartedAt),
		DurationS:    params.DurationS,
		ConsentState: "unknown",
		Visibility:   visibility,
		FolderID:     params.FolderID,
		Status:       StatusPending,
	})
	if err != nil {
		return Created{}, fmt.Errorf("create meeting: %w", err)
	}

	audioKey := storage.AudioKey(actor.WorkspaceID, row.ID, params.AudioExtension)
	transcriptKey := storage.TranscriptKey(actor.WorkspaceID, row.ID)

	updated, err := s.queries.UpdateMeeting(ctx, sqlcgen.UpdateMeetingParams{
		ID:               row.ID,
		WorkspaceID:      actor.WorkspaceID,
		AudioObject:      &audioKey,
		TranscriptObject: &transcriptKey,
	})
	if err != nil {
		return Created{}, fmt.Errorf("record meeting objects: %w", err)
	}

	audio, err := s.storage.PresignUpload(ctx, s.buckets.Audio, audioKey, audioContentType, s.maxAudioBytes, s.uploadTTL)
	if err != nil {
		return Created{}, fmt.Errorf("presign audio upload: %w", err)
	}
	transcript, err := s.storage.PresignUpload(ctx, s.buckets.Transcripts, transcriptKey, transcriptType, s.maxAudioBytes, s.uploadTTL)
	if err != nil {
		return Created{}, fmt.Errorf("presign transcript upload: %w", err)
	}

	return Created{
		Meeting: meetingFromRow(updated),
		Upload: UploadTargets{
			Audio:      audio,
			Transcript: transcript,
			ExpiresAt:  s.clock().Add(s.uploadTTL),
		},
	}, nil
}

func (s *Service) Get(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) (Detail, error) {
	meeting, err := s.viewable(ctx, actor, meetingID)
	if err != nil {
		return Detail{}, err
	}

	detail := Detail{Meeting: meeting}
	if meeting.FolderID != nil {
		folder, err := s.folder(ctx, actor.WorkspaceID, *meeting.FolderID)
		if err != nil && !errors.Is(err, ErrFolderNotFound) {
			return Detail{}, err
		}
		if err == nil {
			detail.Folder = &folder
		}
	}

	downloads, err := s.downloads(ctx, meeting)
	if err != nil {
		return Detail{}, err
	}
	detail.Downloads = downloads
	return detail, nil
}

type ListFilter struct {
	FolderID    *uuid.UUID
	Source      string
	From        *time.Time
	To          *time.Time
	Query       string
	CursorAt    *time.Time
	CursorID    *uuid.UUID
	PageSize    int32
	IncludeAll  bool
	OwnerFilter *uuid.UUID
}

func (s *Service) List(ctx context.Context, actor auth.Membership, filter ListFilter) (Page, error) {
	size := filter.PageSize
	if size <= 0 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}

	viewer := actor.UserID
	rows, err := s.queries.ListMeetingsPage(ctx, sqlcgen.ListMeetingsPageParams{
		WorkspaceID:     actor.WorkspaceID,
		ViewerUserID:    &viewer,
		FolderID:        filter.FolderID,
		Source:          optionalText(filter.Source),
		FromTime:        optionalTimestamp(filter.From),
		ToTime:          optionalTimestamp(filter.To),
		Search:          optionalText(escapeLike(filter.Query)),
		CursorCreatedAt: optionalTimestamp(filter.CursorAt),
		CursorID:        filter.CursorID,
		PageSize:        size + 1,
	})
	if err != nil {
		return Page{}, fmt.Errorf("list meetings: %w", err)
	}

	page := Page{Meetings: make([]Meeting, 0, len(rows))}
	for index, row := range rows {
		if int32(index) == size {
			previous := page.Meetings[len(page.Meetings)-1]
			page.NextCursor = EncodeCursor(previous.CreatedAt, previous.ID)
			break
		}
		page.Meetings = append(page.Meetings, meetingFromRow(row))
	}
	return page, nil
}

type UpdateParams struct {
	Title       *string
	Visibility  *string
	FolderID    *uuid.UUID
	ClearFolder bool
}

func (s *Service) Update(ctx context.Context, actor auth.Membership, meetingID uuid.UUID, params UpdateParams) (Meeting, error) {
	if _, err := s.manageable(ctx, actor, meetingID); err != nil {
		return Meeting{}, err
	}
	if params.FolderID != nil && !params.ClearFolder {
		if _, err := s.folder(ctx, actor.WorkspaceID, *params.FolderID); err != nil {
			return Meeting{}, err
		}
	}

	row, err := s.queries.UpdateMeetingDetails(ctx, sqlcgen.UpdateMeetingDetailsParams{
		ID:          meetingID,
		WorkspaceID: actor.WorkspaceID,
		Title:       params.Title,
		Visibility:  params.Visibility,
		FolderID:    params.FolderID,
		ClearFolder: params.ClearFolder,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meeting{}, ErrMeetingNotFound
		}
		return Meeting{}, fmt.Errorf("update meeting: %w", err)
	}
	return meetingFromRow(row), nil
}

func (s *Service) Delete(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) error {
	if _, err := s.manageable(ctx, actor, meetingID); err != nil {
		return err
	}

	row, err := s.queries.DeleteMeetingReturning(ctx, sqlcgen.DeleteMeetingReturningParams{
		ID:          meetingID,
		WorkspaceID: actor.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMeetingNotFound
		}
		return fmt.Errorf("delete meeting: %w", err)
	}

	payload := PurgePayload{
		WorkspaceID: actor.WorkspaceID,
		MeetingID:   meetingID,
		Objects:     s.objectsFor(meetingFromRow(row)),
	}
	if len(payload.Objects) == 0 {
		return nil
	}

	if _, err := s.jobs.Enqueue(ctx, KindPurge, payload, time.Time{}, purgeKey(meetingID)); err != nil {
		return PurgeObjects(ctx, s.storage, payload)
	}
	return nil
}

func (s *Service) Finalize(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) (Finalized, error) {
	meeting, err := s.manageable(ctx, actor, meetingID)
	if err != nil {
		return Finalized{}, err
	}

	var audioBytes, transcriptBytes *int64
	if meeting.AudioObject != "" {
		info, headErr := s.storage.Head(ctx, s.buckets.Audio, meeting.AudioObject)
		switch {
		case headErr == nil:
			if info.Size > s.maxAudioBytes {
				return Finalized{}, ErrObjectTooLarge
			}
			size := info.Size
			audioBytes = &size
		case errors.Is(headErr, storage.ErrNotFound):
		default:
			return Finalized{}, fmt.Errorf("head audio object: %w", headErr)
		}
	}
	if meeting.TranscriptObject != "" {
		info, headErr := s.storage.Head(ctx, s.buckets.Transcripts, meeting.TranscriptObject)
		switch {
		case headErr == nil:
			size := info.Size
			transcriptBytes = &size
		case errors.Is(headErr, storage.ErrNotFound):
		default:
			return Finalized{}, fmt.Errorf("head transcript object: %w", headErr)
		}
	}

	if audioBytes == nil && transcriptBytes == nil {
		return Finalized{}, ErrNoObjects
	}

	row, err := s.queries.FinalizeMeetingObjects(ctx, sqlcgen.FinalizeMeetingObjectsParams{
		ID:              meetingID,
		WorkspaceID:     actor.WorkspaceID,
		Status:          StatusReady,
		AudioBytes:      audioBytes,
		TranscriptBytes: transcriptBytes,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Finalized{}, ErrMeetingNotFound
		}
		return Finalized{}, fmt.Errorf("finalize meeting: %w", err)
	}

	finalized := Finalized{Meeting: meetingFromRow(row), Queued: []string{}}
	if audioBytes != nil && transcriptBytes == nil {
		payload := TranscodePayload{
			WorkspaceID: actor.WorkspaceID,
			MeetingID:   meetingID,
			Bucket:      s.buckets.Audio,
			Key:         finalized.Meeting.AudioObject,
		}
		if _, err := s.jobs.Enqueue(ctx, KindTranscribe, payload, time.Time{}, jobKey(KindTranscribe, meetingID)); err != nil {
			return Finalized{}, fmt.Errorf("enqueue transcribe: %w", err)
		}
		finalized.Queued = append(finalized.Queued, KindTranscribe)
	}
	if transcriptBytes != nil {
		payload := TranscodePayload{
			WorkspaceID: actor.WorkspaceID,
			MeetingID:   meetingID,
			Bucket:      s.buckets.Transcripts,
			Key:         finalized.Meeting.TranscriptObject,
		}
		if _, err := s.jobs.Enqueue(ctx, KindSummarise, payload, time.Time{}, jobKey(KindSummarise, meetingID)); err != nil {
			return Finalized{}, fmt.Errorf("enqueue summarise: %w", err)
		}
		finalized.Queued = append(finalized.Queued, KindSummarise)
	}
	return finalized, nil
}

func (s *Service) meeting(ctx context.Context, workspaceID, meetingID uuid.UUID) (Meeting, error) {
	row, err := s.queries.GetMeeting(ctx, sqlcgen.GetMeetingParams{ID: meetingID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meeting{}, ErrMeetingNotFound
		}
		return Meeting{}, fmt.Errorf("read meeting: %w", err)
	}
	return meetingFromRow(row), nil
}

func (s *Service) viewable(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) (Meeting, error) {
	meeting, err := s.meeting(ctx, actor.WorkspaceID, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	if !meeting.VisibleTo(actor) {
		return Meeting{}, ErrMeetingNotFound
	}
	return meeting, nil
}

func (s *Service) manageable(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) (Meeting, error) {
	meeting, err := s.viewable(ctx, actor, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	if !meeting.ManageableBy(actor) {
		return Meeting{}, ErrNotPermitted
	}
	return meeting, nil
}

func (s *Service) downloads(ctx context.Context, meeting Meeting) (DownloadTargets, error) {
	var targets DownloadTargets
	if meeting.AudioObject != "" && meeting.AudioBytes != nil {
		signed, err := s.storage.PresignDownload(ctx, s.buckets.Audio, meeting.AudioObject, s.downloadTTL)
		if err != nil {
			return DownloadTargets{}, fmt.Errorf("presign audio download: %w", err)
		}
		targets.Audio = &signed
	}
	if meeting.TranscriptObject != "" && meeting.TranscriptBytes != nil {
		signed, err := s.storage.PresignDownload(ctx, s.buckets.Transcripts, meeting.TranscriptObject, s.downloadTTL)
		if err != nil {
			return DownloadTargets{}, fmt.Errorf("presign transcript download: %w", err)
		}
		targets.Transcript = &signed
	}
	return targets, nil
}

func (s *Service) objectsFor(meeting Meeting) []ObjectRef {
	objects := make([]ObjectRef, 0, 2)
	if meeting.AudioObject != "" {
		objects = append(objects, ObjectRef{Bucket: s.buckets.Audio, Key: meeting.AudioObject})
	}
	if meeting.TranscriptObject != "" {
		objects = append(objects, ObjectRef{Bucket: s.buckets.Transcripts, Key: meeting.TranscriptObject})
	}
	return objects
}

func escapeLike(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(trimmed)
}

func jobKey(kind string, meetingID uuid.UUID) string {
	return kind + ":meeting:" + meetingID.String()
}

func purgeKey(meetingID uuid.UUID) string {
	return jobKey(KindPurge, meetingID)
}
