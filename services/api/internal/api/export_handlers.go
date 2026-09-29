package api

import (
	"errors"
	"net/http"

	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/integrations"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

type exportRequest struct {
	Target string `json:"target"`
}

func (s *Server) handleExportMeeting(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}
	meetingID, ok := pathUUID(r, meetingParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That meeting does not exist.")
		return
	}
	if s.integrations == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "integrations_disabled", "no integration providers are configured")
		return
	}
	var body exportRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	v := validation.New()
	target := v.RequiredString("target", body.Target, 64)
	if v.Write(w, r) {
		return
	}

	err := s.meetings.Export(r.Context(), membership, meetingID, target, s.integrations)
	if err != nil {
		s.writeExportError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, map[string]string{"status": "delivered", "target": target})
}

func (s *Server) writeExportError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, integrations.ErrUnknownProvider):
		httpx.WriteError(w, r, http.StatusBadRequest, "unknown_provider", "that integration is not available")
	case errors.Is(err, integrations.ErrNotConfigured):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "integration_not_configured", "that integration is not configured")
	case errors.Is(err, integrations.ErrInvalidDelivery):
		httpx.WriteError(w, r, http.StatusBadRequest, "invalid_export", "the meeting cannot be exported yet")
	case errors.Is(err, integrations.ErrDeliveryFailed):
		httpx.WriteError(w, r, http.StatusBadGateway, "delivery_failed", "the integration provider rejected the delivery")
	default:
		s.writeLibraryError(w, r, err)
	}
}
