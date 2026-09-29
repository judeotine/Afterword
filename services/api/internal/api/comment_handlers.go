package api

import (
	"net/http"
	"time"

	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

const commentParam = "commentID"

type commentView struct {
	ID        string    `json:"id"`
	MeetingID string    `json:"meeting_id"`
	UserID    string    `json:"user_id,omitempty"`
	AtS       float64   `json:"at_s"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type commentListView struct {
	Comments   []commentView `json:"comments"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func newCommentView(comment meetings.Comment) commentView {
	view := commentView{
		ID:        comment.ID.String(),
		MeetingID: comment.MeetingID.String(),
		AtS:       comment.AtS,
		Body:      comment.Body,
		CreatedAt: comment.CreatedAt,
	}
	if comment.UserID != nil {
		view.UserID = comment.UserID.String()
	}
	return view
}

type createCommentRequest struct {
	AtS  float64 `json:"at_s"`
	Body string  `json:"body"`
}

type updateCommentRequest struct {
	AtS  *float64 `json:"at_s"`
	Body *string  `json:"body"`
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}

	var body createCommentRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	v := validation.New()
	text := v.RequiredString("body", body.Body, meetings.MaxCommentLength)
	if v.Write(w, r) {
		return
	}

	comment, err := s.meetings.CreateComment(r.Context(), membership, meetingID, body.AtS, text)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, newCommentView(comment))
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
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

	page, err := s.meetings.ListComments(r.Context(), membership, meetingID, cursor, pageSize)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	view := commentListView{Comments: make([]commentView, 0, len(page.Comments)), NextCursor: page.NextCursor}
	for _, comment := range page.Comments {
		view.Comments = append(view.Comments, newCommentView(comment))
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}
	commentID, ok := pathUUID(r, commentParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That comment does not exist.")
		return
	}

	var body updateCommentRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}

	comment, err := s.meetings.UpdateComment(r.Context(), membership, meetingID, commentID, body.Body, body.AtS)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newCommentView(comment))
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}
	commentID, ok := pathUUID(r, commentParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That comment does not exist.")
		return
	}

	if err := s.meetings.DeleteComment(r.Context(), membership, meetingID, commentID); err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
