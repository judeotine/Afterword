package api

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

type createShareRequest struct {
	Permission string `json:"permission"`
	ExpiresAt  string `json:"expires_at"`
}

func (s *Server) handleCreateShareLink(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}

	var body createShareRequest
	if err := decodeOptionalJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	v := validation.New()
	permission := v.OneOf("permission", body.Permission, meetings.Permissions, meetings.PermissionView)
	expiresAt := optionalTime(v, "expires_at", body.ExpiresAt)
	if v.Write(w, r) {
		return
	}

	link, meeting, err := s.meetings.CreateShareLink(r.Context(), membership, meetingID, meetings.ShareParams{
		Permission: permission,
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, createShareView{
		Share:   s.newShareLinkView(link),
		Meeting: newMeetingView(meeting),
	})
}

func (s *Server) handleListShareLinks(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}

	links, err := s.meetings.ListShareLinks(r.Context(), membership, meetingID)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}

	view := shareLinkListView{Shares: make([]shareLinkView, 0, len(links))}
	for _, link := range links {
		view.Shares = append(view.Shares, s.newShareLinkView(link))
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}

func (s *Server) handleRevokeShareLinks(w http.ResponseWriter, r *http.Request) {
	membership, meetingID, ok := s.libraryTarget(w, r)
	if !ok {
		return
	}
	if _, err := s.meetings.RevokeShareLinks(r.Context(), membership, meetingID); err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSharedMeeting(w http.ResponseWriter, r *http.Request) {
	token, ok := s.shareToken(w, r)
	if !ok {
		return
	}
	if !s.allowSharedRequest(w, r) {
		return
	}

	shared, err := s.meetings.Shared(r.Context(), token)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, sharedView{
		Meeting:    newSharedMeetingView(shared.Meeting),
		Permission: shared.Permission,
		Download:   newDownloadView(shared.Downloads),
	})
}

func (s *Server) handleSharedSegments(w http.ResponseWriter, r *http.Request) {
	token, ok := s.shareToken(w, r)
	if !ok {
		return
	}
	if !s.allowSharedRequest(w, r) {
		return
	}

	v := validation.New()
	pageSize := queryInt32(v, r, "page_size", meetings.DefaultPageSize, meetings.MaxPageSize)
	afterSeq := queryOptionalInt32(v, r, "after_seq")
	if v.Write(w, r) {
		return
	}

	page, err := s.meetings.SharedSegments(r.Context(), token, afterSeq, pageSize)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newSegmentListView(page))
}

func (s *Server) shareToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.meetings == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, codeNotConfigured, "The meeting library is not configured.")
		return "", false
	}
	token := strings.TrimSpace(chi.URLParam(r, shareTokenParam))
	if token == "" || len(token) > 128 {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That share link does not exist.")
		return "", false
	}
	return token, true
}

func (s *Server) allowSharedRequest(w http.ResponseWriter, r *http.Request) bool {
	err := s.meetings.AllowSharedRequest(r.Context(), sharedClientIP(r))
	if err == nil {
		return true
	}
	if errors.Is(err, meetings.ErrRateLimited) {
		writeRateLimited(w, r, time.Minute, "Too many requests for that link. Try again shortly.")
		return false
	}
	s.writeLibraryError(w, r, err)
	return false
}

func sharedClientIP(r *http.Request) string {
	host := strings.TrimSpace(r.RemoteAddr)
	if bare, _, err := net.SplitHostPort(host); err == nil {
		host = strings.TrimSpace(bare)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	return addr.Unmap().String()
}

func (s *Server) shareURL(token string) string {
	if s.appBaseURL == "" {
		return "/shared/" + token
	}
	return s.appBaseURL + "/shared/" + token
}
