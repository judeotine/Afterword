package meetings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rs/zerolog"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

const (
	DefaultUploadTTL          = 30 * time.Minute
	DefaultDownloadTTL        = 15 * time.Minute
	DefaultMaxAudioBytes      = int64(2) << 30
	DefaultMaxTranscriptBytes = int64(64) << 20
	audioContentType          = "application/octet-stream"
	transcriptType            = "application/json"
)

type Enqueuer interface {
	Enqueue(ctx context.Context, kind string, payload any, runAt time.Time, idempotencyKey string) (*jobs.Job, error)
	EnqueueUnique(ctx context.Context, kind string, payload any, runAt time.Time, idempotencyKey string) (*jobs.Job, bool, error)
	GetByIdempotencyKey(ctx context.Context, kind string, idempotencyKey string) (*jobs.Job, error)
}

type ServiceOptions struct {
	Pool               *pgxpool.Pool
	Storage            storage.Client
	Buckets            storage.Buckets
	Jobs               Enqueuer
	Logger             zerolog.Logger
	UploadTTL          time.Duration
	DownloadTTL        time.Duration
	MaxAudioBytes      int64
	MaxTranscriptBytes int64
	ShareRateWindow    time.Duration
	ShareRateLimit     int64
	Clock              func() time.Time
}

type Service struct {
	pool               *pgxpool.Pool
	queries            *sqlcgen.Queries
	storage            storage.Client
	buckets            storage.Buckets
	jobs               Enqueuer
	logger             zerolog.Logger
	uploadTTL          time.Duration
	downloadTTL        time.Duration
	maxAudioBytes      int64
	maxTranscriptBytes int64
	shareWindow        time.Duration
	shareLimit         int64
	clock              func() time.Time
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
		pool:               options.Pool,
		queries:            sqlcgen.New(options.Pool),
		storage:            options.Storage,
		buckets:            options.Buckets.WithDefaults(),
		jobs:               options.Jobs,
		logger:             options.Logger,
		uploadTTL:          options.UploadTTL,
		downloadTTL:        options.DownloadTTL,
		maxAudioBytes:      options.MaxAudioBytes,
		maxTranscriptBytes: options.MaxTranscriptBytes,
		shareWindow:        options.ShareRateWindow,
		shareLimit:         options.ShareRateLimit,
		clock:              options.Clock,
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
	if service.maxTranscriptBytes <= 0 {
		service.maxTranscriptBytes = DefaultMaxTranscriptBytes
	}
	if service.shareWindow <= 0 {
		service.shareWindow = DefaultShareRateWindow
	}
	if service.shareLimit <= 0 {
		service.shareLimit = DefaultShareRateLimit
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Created{}, fmt.Errorf("begin create transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := s.queries.WithTx(tx)
	row, err := queries.CreateMeeting(ctx, sqlcgen.CreateMeetingParams{
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

	updated, err := queries.UpdateMeeting(ctx, sqlcgen.UpdateMeetingParams{
		ID:               row.ID,
		WorkspaceID:      actor.WorkspaceID,
		AudioObject:      &audioKey,
		TranscriptObject: &transcriptKey,
	})
	if err != nil {
		return Created{}, fmt.Errorf("record meeting objects: %w", err)
	}

	meeting := meetingFromRow(updated)
	upload, err := s.uploadTargets(ctx, meeting)
	if err != nil {
		return Created{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Created{}, fmt.Errorf("commit create transaction: %w", err)
	}
	return Created{Meeting: meeting, Upload: upload}, nil
}

func (s *Service) UploadTargets(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) (Created, error) {
	meeting, err := s.manageable(ctx, actor, meetingID)
	if err != nil {
		return Created{}, err
	}
	if meeting.AudioObject == "" || meeting.TranscriptObject == "" {
		return Created{}, ErrNoObjects
	}
	upload, err := s.uploadTargets(ctx, meeting)
	if err != nil {
		return Created{}, err
	}
	return Created{Meeting: meeting, Upload: upload}, nil
}

func (s *Service) uploadTargets(ctx context.Context, meeting Meeting) (UploadTargets, error) {
	audio, err := s.storage.PresignUpload(ctx, s.buckets.Audio, meeting.AudioObject, audioContentType, s.maxAudioBytes, s.uploadTTL)
	if err != nil {
		return UploadTargets{}, fmt.Errorf("presign audio upload: %w", err)
	}
	transcript, err := s.storage.PresignUpload(ctx, s.buckets.Transcripts, meeting.TranscriptObject, transcriptType, s.maxTranscriptBytes, s.uploadTTL)
	if err != nil {
		return UploadTargets{}, fmt.Errorf("presign transcript upload: %w", err)
	}
	return UploadTargets{Audio: audio, Transcript: transcript, ExpiresAt: s.clock().Add(s.uploadTTL)}, nil
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

	clipObjects, err := s.queries.ListClipObjectsForMeeting(ctx, sqlcgen.ListClipObjectsForMeetingParams{
		MeetingID:   meetingID,
		WorkspaceID: actor.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("read clip objects: %w", err)
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
		Objects:     s.objectsFor(meetingFromRow(row), clipObjects),
	}
	if len(payload.Objects) == 0 {
		return nil
	}
	s.schedulePurge(ctx, payload)
	return nil
}

func (s *Service) schedulePurge(ctx context.Context, payload PurgePayload) {
	key := purgeKey(payload.MeetingID)
	if _, err := s.jobs.Enqueue(ctx, KindPurge, payload, time.Time{}, key); err == nil {
		return
	}
	if err := PurgeObjects(ctx, s.storage, payload); err == nil {
		return
	}
	if _, err := s.jobs.Enqueue(ctx, KindPurge, payload, time.Time{}, key); err != nil {
		s.logger.Error().
			Err(err).
			Str("meeting_id", payload.MeetingID.String()).
			Str("workspace_id", payload.WorkspaceID.String()).
			Int("objects", len(payload.Objects)).
			Msg("meeting objects were left behind: the purge job could not be queued and the inline purge failed")
	}
}

func (s *Service) Finalize(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) (Finalized, error) {
	meeting, err := s.manageable(ctx, actor, meetingID)
	if err != nil {
		return Finalized{}, err
	}

	audioBytes, err := s.objectSize(ctx, s.buckets.Audio, meeting.AudioObject, s.maxAudioBytes)
	if err != nil {
		return Finalized{}, err
	}
	transcriptBytes, err := s.objectSize(ctx, s.buckets.Transcripts, meeting.TranscriptObject, s.maxTranscriptBytes)
	if err != nil {
		return Finalized{}, err
	}
	if audioBytes == nil && transcriptBytes == nil {
		return Finalized{}, ErrNoObjects
	}

	kinds := make([]string, 0, 2)
	if audioBytes != nil && transcriptBytes == nil {
		kinds = append(kinds, KindTranscribe)
	}
	if transcriptBytes != nil {
		kinds = append(kinds, KindSummarise)
	}

	generation, err := s.nextGeneration(ctx, meeting, kinds)
	if err != nil {
		return Finalized{}, err
	}

	row, err := s.queries.FinalizeMeetingObjects(ctx, sqlcgen.FinalizeMeetingObjectsParams{
		ID:                 meetingID,
		WorkspaceID:        actor.WorkspaceID,
		Status:             StatusReady,
		AudioBytes:         audioBytes,
		TranscriptBytes:    transcriptBytes,
		FinalizeGeneration: generation,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Finalized{}, ErrMeetingNotFound
		}
		return Finalized{}, fmt.Errorf("finalize meeting: %w", err)
	}

	finalized := Finalized{Meeting: meetingFromRow(row), Queued: []string{}}
	for _, kind := range kinds {
		bucket := s.buckets.Audio
		key := finalized.Meeting.AudioObject
		if kind == KindSummarise {
			bucket = s.buckets.Transcripts
			key = finalized.Meeting.TranscriptObject
		}
		payload := TranscodePayload{
			WorkspaceID: actor.WorkspaceID,
			MeetingID:   meetingID,
			Bucket:      bucket,
			Key:         key,
			Generation:  generation,
		}
		_, created, err := s.jobs.EnqueueUnique(ctx, kind, payload, time.Time{}, jobKey(kind, meetingID, generation))
		if err != nil {
			return Finalized{}, fmt.Errorf("enqueue %s: %w", kind, err)
		}
		if created {
			finalized.Queued = append(finalized.Queued, kind)
		}
	}
	return finalized, nil
}

func (s *Service) objectSize(ctx context.Context, bucket, key string, maximum int64) (*int64, error) {
	if key == "" {
		return nil, nil
	}
	info, err := s.storage.Head(ctx, bucket, key)
	switch {
	case err == nil:
		if info.Size > maximum {
			return nil, ErrObjectTooLarge
		}
		size := info.Size
		return &size, nil
	case errors.Is(err, storage.ErrNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("head object %s/%s: %w", bucket, key, err)
	}
}

func (s *Service) nextGeneration(ctx context.Context, meeting Meeting, kinds []string) (int32, error) {
	if meeting.Generation <= 0 {
		return 1, nil
	}
	for _, kind := range kinds {
		job, err := s.jobs.GetByIdempotencyKey(ctx, kind, jobKey(kind, meeting.ID, meeting.Generation))
		if err != nil {
			if errors.Is(err, jobs.ErrNotFound) {
				continue
			}
			return 0, fmt.Errorf("read %s job: %w", kind, err)
		}
		if job.Status == jobs.StatusPending || job.Status == jobs.StatusRunning {
			return meeting.Generation, nil
		}
	}
	return meeting.Generation + 1, nil
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

func (s *Service) EnsureManageable(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) error {
	_, err := s.manageable(ctx, actor, meetingID)
	return err
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

func (s *Service) objectsFor(meeting Meeting, clipObjects []*string) []ObjectRef {
	objects := make([]ObjectRef, 0, 2+len(clipObjects))
	if meeting.AudioObject != "" {
		objects = append(objects, ObjectRef{Bucket: s.buckets.Audio, Key: meeting.AudioObject})
	}
	if meeting.TranscriptObject != "" {
		objects = append(objects, ObjectRef{Bucket: s.buckets.Transcripts, Key: meeting.TranscriptObject})
	}
	seen := map[string]struct{}{}
	for _, clip := range clipObjects {
		if clip == nil || *clip == "" {
			continue
		}
		if _, exists := seen[*clip]; exists {
			continue
		}
		seen[*clip] = struct{}{}
		objects = append(objects, ObjectRef{Bucket: s.buckets.Clips, Key: *clip})
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

func jobKey(kind string, meetingID uuid.UUID, generation int32) string {
	return kind + ":meeting:" + meetingID.String() + ":g" + strconv.FormatInt(int64(generation), 10)
}

func purgeKey(meetingID uuid.UUID) string {
	return KindPurge + ":meeting:" + meetingID.String()
}
