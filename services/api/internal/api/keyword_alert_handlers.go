package api

import (
	"net/http"
	"time"

	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

const alertParam = "alertID"

type keywordAlertView struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Phrase    string    `json:"phrase"`
	Channel   string    `json:"channel"`
	CreatedAt time.Time `json:"created_at"`
}

type keywordAlertListView struct {
	Alerts     []keywordAlertView `json:"alerts"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

func newKeywordAlertView(alert meetings.KeywordAlert) keywordAlertView {
	return keywordAlertView{
		ID:        alert.ID.String(),
		UserID:    alert.UserID.String(),
		Phrase:    alert.Phrase,
		Channel:   alert.Channel,
		CreatedAt: alert.CreatedAt,
	}
}

type createKeywordAlertRequest struct {
	Phrase  string `json:"phrase"`
	Channel string `json:"channel"`
}

type updateKeywordAlertRequest struct {
	Phrase  *string `json:"phrase"`
	Channel *string `json:"channel"`
}

func (s *Server) handleCreateKeywordAlert(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}
	var body createKeywordAlertRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	v := validation.New()
	phrase := v.RequiredString("phrase", body.Phrase, meetings.MaxKeywordPhraseLength)
	channel := v.OneOf("channel", body.Channel, meetings.AlertChannels, "")
	if v.Write(w, r) {
		return
	}

	alert, err := s.meetings.CreateKeywordAlert(r.Context(), membership, phrase, channel)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, newKeywordAlertView(alert))
}

func (s *Server) handleListKeywordAlerts(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}
	v := validation.New()
	pageSize := queryInt32(v, r, "page_size", meetings.DefaultPageSize, meetings.MaxPageSize)
	cursor := queryString(r, "cursor")
	if v.Write(w, r) {
		return
	}

	page, err := s.meetings.ListKeywordAlerts(r.Context(), membership, cursor, pageSize)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	view := keywordAlertListView{Alerts: make([]keywordAlertView, 0, len(page.Alerts)), NextCursor: page.NextCursor}
	for _, alert := range page.Alerts {
		view.Alerts = append(view.Alerts, newKeywordAlertView(alert))
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleUpdateKeywordAlert(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}
	alertID, ok := pathUUID(r, alertParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That keyword alert does not exist.")
		return
	}
	var body updateKeywordAlertRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}

	alert, err := s.meetings.UpdateKeywordAlert(r.Context(), membership, alertID, body.Phrase, body.Channel)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newKeywordAlertView(alert))
}

func (s *Server) handleDeleteKeywordAlert(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}
	alertID, ok := pathUUID(r, alertParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That keyword alert does not exist.")
		return
	}

	if err := s.meetings.DeleteKeywordAlert(r.Context(), membership, alertID); err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
