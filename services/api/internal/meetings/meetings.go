package meetings

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

const (
	VisibilityPrivate   = "private"
	VisibilityWorkspace = "workspace"

	SourceDesktop = "desktop"
	SourceBot     = "bot"
	SourceImport  = "import"

	StatusPending    = "pending"
	StatusUploading  = "uploading"
	StatusProcessing = "processing"
	StatusReady      = "ready"
	StatusFailed     = "failed"

	PermissionView    = "view"
	PermissionComment = "comment"

	KindTranscribe = "transcribe"
	KindSummarise  = "summarise"
	KindPurge      = "purge"

	MaxSegments      = 20000
	MaxTitleLength   = 300
	MaxSpeakerLength = 200
	MaxTextLength    = 10000
	MaxFolderName    = 200
	DefaultPageSize  = 50
	MaxPageSize      = 200
	ShareTokenBytes  = 32
)

var (
	Visibilities = []string{VisibilityPrivate, VisibilityWorkspace}
	Sources      = []string{SourceDesktop, SourceBot, SourceImport}
	Platforms    = []string{"meet", "zoom", "teams"}
	Permissions  = []string{PermissionView, PermissionComment}
)

var (
	ErrMeetingNotFound   = errors.New("meetings: meeting not found")
	ErrFolderNotFound    = errors.New("meetings: folder not found")
	ErrFolderHasChildren = errors.New("meetings: folder still has child folders")
	ErrFolderCycle       = errors.New("meetings: a folder cannot be its own parent")
	ErrNotPermitted      = errors.New("meetings: your role does not allow that action")
	ErrNoObjects         = errors.New("meetings: neither the audio nor the transcript object was uploaded")
	ErrObjectTooLarge    = errors.New("meetings: the uploaded object is larger than the agreed limit")
	ErrDeclaredSize      = errors.New("meetings: the declared upload size is larger than the agreed limit")
	ErrEmptyObject       = errors.New("meetings: the uploaded audio object is empty")
	ErrMeetingFinalized  = errors.New("meetings: the meeting has already been finalized")
	ErrTooManySegments   = errors.New("meetings: too many transcript segments")
	ErrDuplicateSequence = errors.New("meetings: transcript segment sequence numbers must be unique")
	ErrShareLinkNotFound = errors.New("meetings: share link not found")
	ErrShareLinkExpired  = errors.New("meetings: share link has expired")
	ErrShareLinkClosed   = errors.New("meetings: link sharing is turned off for that meeting")
	ErrRateLimited       = errors.New("meetings: too many requests for that share link")
)

type Meeting struct {
	ID               uuid.UUID
	WorkspaceID      uuid.UUID
	OwnerUserID      *uuid.UUID
	Title            string
	Source           string
	Platform         string
	StartedAt        *time.Time
	DurationS        int32
	ConsentState     string
	Visibility       string
	FolderID         *uuid.UUID
	AudioObject      string
	TranscriptObject string
	AudioBytes       *int64
	TranscriptBytes  *int64
	AudioETag        string
	TranscriptETag   string
	Status           string
	LinkSharing      bool
	Generation       int32
	CreatedAt        time.Time
}

type Folder struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Name        string
	ParentID    *uuid.UUID
	CreatedAt   time.Time
}

type Segment struct {
	Seq     int32
	Speaker string
	StartS  float64
	EndS    float64
	Text    string
}

type StoredSegment struct {
	ID        uuid.UUID
	MeetingID uuid.UUID
	Segment
	CreatedAt time.Time
}

type ShareLink struct {
	ID         uuid.UUID
	MeetingID  uuid.UUID
	Token      string
	Permission string
	ExpiresAt  *time.Time
	CreatedAt  time.Time
}

func HashShareToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type UploadTargets struct {
	Audio      storage.PresignedRequest
	Transcript storage.PresignedRequest
	ExpiresAt  time.Time
}

type DownloadTargets struct {
	Audio      *storage.PresignedRequest
	Transcript *storage.PresignedRequest
}

type Created struct {
	Meeting Meeting
	Upload  UploadTargets
}

type Detail struct {
	Meeting   Meeting
	Folder    *Folder
	Downloads DownloadTargets
}

type Finalized struct {
	Meeting Meeting
	Queued  []string
}

type Page struct {
	Meetings   []Meeting
	NextCursor string
}

type SegmentPage struct {
	Segments []StoredSegment
	NextSeq  *int32
	Total    int64
}

type Shared struct {
	Meeting    Meeting
	Link       ShareLink
	Downloads  DownloadTargets
	Permission string
}

type ObjectRef struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

type PurgePayload struct {
	WorkspaceID uuid.UUID   `json:"workspace_id"`
	MeetingID   uuid.UUID   `json:"meeting_id"`
	Objects     []ObjectRef `json:"objects"`
}

type TranscodePayload struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	MeetingID   uuid.UUID `json:"meeting_id"`
	Bucket      string    `json:"bucket"`
	Key         string    `json:"key"`
	Generation  int32     `json:"generation"`
}

func meetingFromRow(row sqlcgen.Meeting) Meeting {
	return Meeting{
		ID:               row.ID,
		WorkspaceID:      row.WorkspaceID,
		OwnerUserID:      row.OwnerUserID,
		Title:            row.Title,
		Source:           row.Source,
		Platform:         text(row.Platform),
		StartedAt:        optionalMoment(row.StartedAt),
		DurationS:        row.DurationS,
		ConsentState:     row.ConsentState,
		Visibility:       row.Visibility,
		FolderID:         row.FolderID,
		AudioObject:      text(row.AudioObject),
		TranscriptObject: text(row.TranscriptObject),
		AudioBytes:       row.AudioBytes,
		TranscriptBytes:  row.TranscriptBytes,
		AudioETag:        text(row.AudioEtag),
		TranscriptETag:   text(row.TranscriptEtag),
		Status:           row.Status,
		LinkSharing:      row.LinkSharingEnabled,
		Generation:       row.FinalizeGeneration,
		CreatedAt:        moment(row.CreatedAt),
	}
}

func folderFromRow(row sqlcgen.Folder) Folder {
	return Folder{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		ParentID:    row.ParentID,
		CreatedAt:   moment(row.CreatedAt),
	}
}

func shareLinkFromRow(row sqlcgen.ShareLink) ShareLink {
	return ShareLink{
		ID:         row.ID,
		MeetingID:  row.MeetingID,
		Permission: row.Permission,
		ExpiresAt:  optionalMoment(row.ExpiresAt),
		CreatedAt:  moment(row.CreatedAt),
	}
}

func (m Meeting) VisibleTo(actor auth.Membership) bool {
	if m.WorkspaceID != actor.WorkspaceID {
		return false
	}
	if m.Visibility == VisibilityWorkspace {
		return true
	}
	return m.OwnerUserID != nil && *m.OwnerUserID == actor.UserID
}

func (m Meeting) ManageableBy(actor auth.Membership) bool {
	if !m.VisibleTo(actor) {
		return false
	}
	if m.OwnerUserID != nil && *m.OwnerUserID == actor.UserID {
		return true
	}
	return actor.Role.AtLeast(auth.RoleAdmin)
}

func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func moment(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func optionalMoment(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time.UTC()
	return &at
}

func optionalTimestamp(at *time.Time) pgtype.Timestamptz {
	if at == nil || at.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: at.UTC(), Valid: true}
}
