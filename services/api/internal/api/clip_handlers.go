package api

import (
	"net/http"
	"time"

	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

const clipParam = "clipID"

type clipView struct {
	ID         string    `json:"id"`
	MeetingID  string    `json:"meeting_id"`
	StartS     float64   `json:"start_s"`
	EndS       float64   `json:"end_s"`
	Title      string    `json:"title"`
	Object     string    `json:"object,omitempty"`
	ShareToken string    `json:"share_token,omitempty"`
	Ready      bool      `json:"ready"`
	CreatedAt  time.Time `json:"created_at"`
}

type clipListView struct {
	Clips      []clipView `json:"clips"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

func newClipView(clip meetings.Clip) clipView {
	return clipView{
		ID:         clip.ID.String(),
		MeetingID:  clip.MeetingID.String(),
		StartS:     clip.StartS,
		EndS:       clip.EndS,
		Title:      clip.Title,
		Object:     clip.Object,
		ShareToken: clip.ShareToken,
		Ready:      clip.Object != "",
		CreatedAt:  clip.CreatedAt,
	}
}

type createClipRequest struct {
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
	Title  string  `json:"title"`
}

func (s *Server) handleCreateClip(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}
	var body createClipRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	v := validation.New()
	title := v.OptionalString("title", body.Title, meetings.MaxClipTitleLength)
	if v.Write(w, r) {
		return
	}

	clip, err := s.meetings.CreateClip(r.Context(), membership, meetingID, body.StartS, body.EndS, title)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, newClipView(clip))
}

func (s *Server) handleListClips(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}
	v := validation.New()
	pageSize := queryInt32(v, r, "page_size", meetings.DefaultPageSize, meetings.MaxPageSize)
	cursor := queryString(r, "cursor")
	if v.Write(w, r) {
		return
	}

	page, err := s.meetings.ListClips(r.Context(), membership, meetingID, cursor, pageSize)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	view := clipListView{Clips: make([]clipView, 0, len(page.Clips)), NextCursor: page.NextCursor}
	for _, clip := range page.Clips {
		view.Clips = append(view.Clips, newClipView(clip))
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleDeleteClip(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}
	clipID, ok := pathUUID(r, clipParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That clip does not exist.")
		return
	}

	if err := s.meetings.DeleteClip(r.Context(), membership, meetingID, clipID); err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
