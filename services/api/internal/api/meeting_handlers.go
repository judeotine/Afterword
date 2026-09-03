package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

type createMeetingRequest struct {
	Title          string `json:"title"`
	Source         string `json:"source"`
	Platform       string `json:"platform"`
	StartedAt      string `json:"started_at"`
	DurationS      int64  `json:"duration_s"`
	Visibility     string `json:"visibility"`
	FolderID       string `json:"folder_id"`
	AudioExtension string `json:"audio_extension"`
}

func (s *Server) handleCreateMeeting(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}

	var body createMeetingRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	v := validation.New()
	title := v.RequiredString("title", body.Title, meetings.MaxTitleLength)
	source := v.OneOf("source", body.Source, meetings.Sources, "")
	visibility := v.OneOf("visibility", body.Visibility, meetings.Visibilities, meetings.VisibilityPrivate)
	platform := ""
	if body.Platform != "" {
		platform = v.OneOf("platform", body.Platform, meetings.Platforms, "")
	}
	v.Min("duration_s", body.DurationS, 0)
	v.Max("duration_s", body.DurationS, int64(^uint32(0)>>1))
	startedAt := optionalTime(v, "started_at", body.StartedAt)
	var folderID *uuid.UUID
	if body.FolderID != "" {
		parsed := v.UUID("folder_id", body.FolderID)
		if parsed != uuid.Nil {
			folderID = &parsed
		}
	}
	if v.Write(w, r) {
		return
	}

	created, err := s.meetings.Create(r.Context(), membership, meetings.CreateParams{
		Title:          title,
		Source:         source,
		Platform:       platform,
		StartedAt:      startedAt,
		DurationS:      int32(body.DurationS),
		Visibility:     visibility,
		FolderID:       folderID,
		AudioExtension: body.AudioExtension,
	})
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}

	httpx.WriteJSON(w, r, http.StatusCreated, createMeetingView{
		Meeting: newMeetingView(created.Meeting),
		Upload:  newUploadView(created.Upload),
	})
}

func (s *Server) handleListMeetings(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}

	v := validation.New()
	filter := meetings.ListFilter{
		FolderID: queryUUID(v, r, "folder"),
		From:     queryTime(v, r, "from"),
		To:       queryTime(v, r, "to"),
		Query:    queryString(r, "q"),
		PageSize: queryInt32(v, r, "page_size", meetings.DefaultPageSize, meetings.MaxPageSize),
	}
	if source := queryString(r, "source"); source != "" {
		filter.Source = v.OneOf("source", source, meetings.Sources, "")
	}
	if filter.From != nil && filter.To != nil {
		v.Ordered("to", *filter.From, *filter.To)
	}
	if cursor := queryString(r, "cursor"); cursor != "" {
		at, id, err := meetings.DecodeCursor(cursor)
		if err != nil {
			v.Add("cursor", "is not a valid page cursor")
		} else {
			filter.CursorAt = &at
			filter.CursorID = &id
		}
	}
	if v.Write(w, r) {
		return
	}

	page, err := s.meetings.List(r.Context(), membership, filter)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}

	view := meetingListView{Meetings: make([]meetingView, 0, len(page.Meetings)), NextCursor: page.NextCursor}
	for _, meeting := range page.Meetings {
		view.Meetings = append(view.Meetings, newMeetingView(meeting))
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleGetMeeting(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}

	detail, err := s.meetings.Get(r.Context(), membership, meetingID)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}

	view := meetingDetailView{
		Meeting:  newMeetingView(detail.Meeting),
		Download: newDownloadView(detail.Downloads),
	}
	if detail.Folder != nil {
		folder := newFolderView(*detail.Folder)
		view.Folder = &folder
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}

type updateMeetingRequest struct {
	Title      *string `json:"title"`
	Visibility *string `json:"visibility"`
	FolderID   *string `json:"folder_id"`
}

func (s *Server) handleUpdateMeeting(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}

	var body updateMeetingRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	v := validation.New()
	var params meetings.UpdateParams
	if body.Title != nil {
		title := v.RequiredString("title", *body.Title, meetings.MaxTitleLength)
		params.Title = &title
	}
	if body.Visibility != nil {
		visibility := v.OneOf("visibility", *body.Visibility, meetings.Visibilities, "")
		params.Visibility = &visibility
	}
	if body.FolderID != nil {
		if *body.FolderID == "" {
			params.ClearFolder = true
		} else {
			parsed := v.UUID("folder_id", *body.FolderID)
			if parsed != uuid.Nil {
				params.FolderID = &parsed
			}
		}
	}
	if v.Write(w, r) {
		return
	}

	updated, err := s.meetings.Update(r.Context(), membership, meetingID, params)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newMeetingView(updated))
}

func (s *Server) handleDeleteMeeting(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}
	if err := s.meetings.Delete(r.Context(), membership, meetingID); err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFinalizeMeeting(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}

	finalized, err := s.meetings.Finalize(r.Context(), membership, meetingID)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, finalizeView{
		Meeting: newMeetingView(finalized.Meeting),
		Queued:  finalized.Queued,
	})
}

type segmentRequest struct {
	Seq     int64   `json:"seq"`
	Speaker string  `json:"speaker"`
	StartS  float64 `json:"start_s"`
	EndS    float64 `json:"end_s"`
	Text    string  `json:"text"`
}

type replaceSegmentsRequest struct {
	Segments []segmentRequest `json:"segments"`
}

func (s *Server) handleReplaceSegments(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}

	var body replaceSegmentsRequest
	if err := decodeTranscriptJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}
	if len(body.Segments) > meetings.MaxSegments {
		s.writeLibraryError(w, r, meetings.ErrTooManySegments)
		return
	}

	v := validation.New()
	segments := make([]meetings.Segment, 0, len(body.Segments))
	for _, segment := range body.Segments {
		v.Min("segments.seq", segment.Seq, 0)
		v.Max("segments.seq", segment.Seq, meetings.MaxSegments)
		if segment.StartS < 0 {
			v.Add("segments.start_s", "must be at least 0")
		}
		if segment.EndS < segment.StartS {
			v.Add("segments.end_s", "must not be earlier than start_s")
		}
		text := v.RequiredString("segments.text", segment.Text, meetings.MaxTextLength)
		speaker := v.OptionalString("segments.speaker", segment.Speaker, meetings.MaxSpeakerLength)
		if v.Failed() {
			break
		}
		segments = append(segments, meetings.Segment{
			Seq:     int32(segment.Seq),
			Speaker: speaker,
			StartS:  segment.StartS,
			EndS:    segment.EndS,
			Text:    text,
		})
	}
	if v.Write(w, r) {
		return
	}

	stored, err := s.meetings.ReplaceSegments(r.Context(), membership, meetingID, segments)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, segmentWriteView{Stored: stored})
}

func (s *Server) handleListSegments(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}

	v := validation.New()
	pageSize := queryInt32(v, r, "page_size", meetings.DefaultPageSize, meetings.MaxPageSize)
	afterSeq := queryOptionalInt32(v, r, "after_seq")
	if v.Write(w, r) {
		return
	}

	page, err := s.meetings.ListSegments(r.Context(), membership, meetingID, afterSeq, pageSize)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newSegmentListView(page))
}

func (s *Server) libraryActor(w http.ResponseWriter, r *http.Request) (auth.Membership, bool) {
	membership, ok := auth.MembershipFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
		return auth.Membership{}, false
	}
	if s.meetings == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, codeNotConfigured, "The meeting library is not configured.")
		return auth.Membership{}, false
	}
	return membership, true
}

func (s *Server) libraryTarget(w http.ResponseWriter, r *http.Request) (auth.Membership, uuid.UUID, bool) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return auth.Membership{}, uuid.Nil, false
	}
	meetingID, ok := pathUUID(r, meetingParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That meeting does not exist.")
		return auth.Membership{}, uuid.Nil, false
	}
	return membership, meetingID, true
}
