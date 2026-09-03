package api

import (
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

type meetingView struct {
	ID               uuid.UUID  `json:"id"`
	WorkspaceID      uuid.UUID  `json:"workspace_id"`
	OwnerUserID      *uuid.UUID `json:"owner_user_id,omitempty"`
	Title            string     `json:"title"`
	Source           string     `json:"source"`
	Platform         string     `json:"platform,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	DurationS        int32      `json:"duration_s"`
	ConsentState     string     `json:"consent_state"`
	Visibility       string     `json:"visibility"`
	FolderID         *uuid.UUID `json:"folder_id,omitempty"`
	Status           string     `json:"status"`
	AudioBytes       *int64     `json:"audio_bytes,omitempty"`
	TranscriptBytes  *int64     `json:"transcript_bytes,omitempty"`
	AudioObject      string     `json:"audio_object,omitempty"`
	TranscriptObject string     `json:"transcript_object,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type sharedMeetingView struct {
	ID         uuid.UUID  `json:"id"`
	Title      string     `json:"title"`
	Source     string     `json:"source"`
	Platform   string     `json:"platform,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	DurationS  int32      `json:"duration_s"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	AudioBytes *int64     `json:"audio_bytes,omitempty"`
}

type presignView struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type downloadView struct {
	Audio      *presignView `json:"audio,omitempty"`
	Transcript *presignView `json:"transcript,omitempty"`
}

type uploadView struct {
	AudioURL          string            `json:"audio_url"`
	TranscriptURL     string            `json:"transcript_url"`
	ExpiresAt         time.Time         `json:"expires_at"`
	MaxBytes          int64             `json:"max_bytes,omitempty"`
	AudioHeaders      map[string]string `json:"audio_headers,omitempty"`
	TranscriptHeaders map[string]string `json:"transcript_headers,omitempty"`
}

type createMeetingView struct {
	Meeting meetingView `json:"meeting"`
	Upload  uploadView  `json:"upload"`
}

type meetingDetailView struct {
	Meeting  meetingView  `json:"meeting"`
	Folder   *folderView  `json:"folder,omitempty"`
	Download downloadView `json:"download"`
}

type meetingListView struct {
	Meetings   []meetingView `json:"meetings"`
	NextCursor string        `json:"next_cursor"`
}

type finalizeView struct {
	Meeting meetingView `json:"meeting"`
	Queued  []string    `json:"queued"`
}

type folderView struct {
	ID          uuid.UUID  `json:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	Name        string     `json:"name"`
	ParentID    *uuid.UUID `json:"parent_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type folderListView struct {
	Folders []folderView `json:"folders"`
}

type segmentView struct {
	Seq     int32   `json:"seq"`
	Speaker string  `json:"speaker,omitempty"`
	StartS  float64 `json:"start_s"`
	EndS    float64 `json:"end_s"`
	Text    string  `json:"text"`
}

type segmentListView struct {
	Segments []segmentView `json:"segments"`
	Total    int64         `json:"total"`
	NextSeq  *int32        `json:"next_seq,omitempty"`
}

type segmentWriteView struct {
	Stored int `json:"stored"`
}

type shareLinkView struct {
	ID         uuid.UUID  `json:"id"`
	Token      string     `json:"token"`
	URL        string     `json:"url"`
	Permission string     `json:"permission"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type shareLinkListView struct {
	Shares []shareLinkView `json:"shares"`
}

type sharedView struct {
	Meeting    sharedMeetingView `json:"meeting"`
	Permission string            `json:"permission"`
	Download   downloadView      `json:"download"`
}

func newMeetingView(meeting meetings.Meeting) meetingView {
	return meetingView{
		ID:               meeting.ID,
		WorkspaceID:      meeting.WorkspaceID,
		OwnerUserID:      meeting.OwnerUserID,
		Title:            meeting.Title,
		Source:           meeting.Source,
		Platform:         meeting.Platform,
		StartedAt:        meeting.StartedAt,
		DurationS:        meeting.DurationS,
		ConsentState:     meeting.ConsentState,
		Visibility:       meeting.Visibility,
		FolderID:         meeting.FolderID,
		Status:           meeting.Status,
		AudioBytes:       meeting.AudioBytes,
		TranscriptBytes:  meeting.TranscriptBytes,
		AudioObject:      meeting.AudioObject,
		TranscriptObject: meeting.TranscriptObject,
		CreatedAt:        meeting.CreatedAt,
	}
}

func newSharedMeetingView(meeting meetings.Meeting) sharedMeetingView {
	return sharedMeetingView{
		ID:         meeting.ID,
		Title:      meeting.Title,
		Source:     meeting.Source,
		Platform:   meeting.Platform,
		StartedAt:  meeting.StartedAt,
		DurationS:  meeting.DurationS,
		Status:     meeting.Status,
		CreatedAt:  meeting.CreatedAt,
		AudioBytes: meeting.AudioBytes,
	}
}

func newFolderView(folder meetings.Folder) folderView {
	return folderView{
		ID:          folder.ID,
		WorkspaceID: folder.WorkspaceID,
		Name:        folder.Name,
		ParentID:    folder.ParentID,
		CreatedAt:   folder.CreatedAt,
	}
}

func newPresignView(request *storage.PresignedRequest) *presignView {
	if request == nil {
		return nil
	}
	return &presignView{
		Method:    request.Method,
		URL:       request.URL,
		Headers:   request.Headers,
		ExpiresAt: request.ExpiresAt,
	}
}

func newDownloadView(targets meetings.DownloadTargets) downloadView {
	return downloadView{
		Audio:      newPresignView(targets.Audio),
		Transcript: newPresignView(targets.Transcript),
	}
}

func newUploadView(targets meetings.UploadTargets) uploadView {
	return uploadView{
		AudioURL:          targets.Audio.URL,
		TranscriptURL:     targets.Transcript.URL,
		ExpiresAt:         targets.ExpiresAt,
		MaxBytes:          targets.Audio.MaxBytes,
		AudioHeaders:      targets.Audio.Headers,
		TranscriptHeaders: targets.Transcript.Headers,
	}
}

func newSegmentView(segment meetings.StoredSegment) segmentView {
	return segmentView{
		Seq:     segment.Seq,
		Speaker: segment.Speaker,
		StartS:  segment.StartS,
		EndS:    segment.EndS,
		Text:    segment.Text,
	}
}

func newSegmentListView(page meetings.SegmentPage) segmentListView {
	view := segmentListView{Segments: make([]segmentView, 0, len(page.Segments)), Total: page.Total, NextSeq: page.NextSeq}
	for _, segment := range page.Segments {
		view.Segments = append(view.Segments, newSegmentView(segment))
	}
	return view
}

func (s *Server) newShareLinkView(link meetings.ShareLink) shareLinkView {
	return shareLinkView{
		ID:         link.ID,
		Token:      link.Token,
		URL:        s.shareURL(link.Token),
		Permission: link.Permission,
		ExpiresAt:  link.ExpiresAt,
		CreatedAt:  link.CreatedAt,
	}
}
