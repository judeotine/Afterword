package api

import (
	"errors"
	"net/http"

	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/meetings"
)

const (
	codeNoObjects       = "no_objects"
	codeObjectTooLarge  = "object_too_large"
	codeEmptyObject     = "empty_object"
	codeTooManySegments = "too_many_segments"
	codeFolderNotEmpty  = "folder_not_empty"
	codeFolderCycle     = "folder_cycle"
	codeShareExpired    = "share_link_expired"
	codeShareClosed     = "share_link_closed"
	codeFinalized       = "meeting_finalized"
	codeEmptyComment    = "empty_comment"
	codeEmptyPhrase     = "empty_phrase"
	codeInvalidChannel  = "invalid_channel"
)

var libraryErrors = []struct {
	target error
	result statusError
}{
	{meetings.ErrMeetingNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That meeting does not exist."}},
	{meetings.ErrFolderNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That folder does not exist."}},
	{meetings.ErrShareLinkNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That share link does not exist."}},
	{meetings.ErrShareLinkExpired, statusError{http.StatusGone, codeShareExpired, "That share link has expired."}},
	{meetings.ErrShareLinkClosed, statusError{http.StatusGone, codeShareClosed, "Link sharing has been turned off for that meeting."}},
	{meetings.ErrNotPermitted, statusError{http.StatusForbidden, httpx.CodeForbidden, "Your role does not allow that action."}},
	{meetings.ErrNoObjects, statusError{http.StatusConflict, codeNoObjects, "Upload the audio or the transcript before finalising the meeting."}},
	{meetings.ErrMeetingFinalized, statusError{http.StatusConflict, codeFinalized, "That meeting has already been finalised: create a new meeting to upload again."}},
	{meetings.ErrFolderHasChildren, statusError{http.StatusConflict, codeFolderNotEmpty, "Delete the folders inside this one first."}},
	{meetings.ErrFolderCycle, statusError{http.StatusConflict, codeFolderCycle, "A folder cannot be moved inside itself."}},
	{meetings.ErrObjectTooLarge, statusError{http.StatusRequestEntityTooLarge, codeObjectTooLarge, "The uploaded file is larger than the agreed limit."}},
	{meetings.ErrDeclaredSize, statusError{http.StatusRequestEntityTooLarge, codeObjectTooLarge, "That upload size is larger than the agreed limit."}},
	{meetings.ErrEmptyObject, statusError{http.StatusUnprocessableEntity, codeEmptyObject, "The uploaded audio file is empty: upload it again before finalising the meeting."}},
	{meetings.ErrTooManySegments, statusError{http.StatusRequestEntityTooLarge, codeTooManySegments, "A transcript may hold at most 20000 segments."}},
	{meetings.ErrDuplicateSequence, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "Transcript segment sequence numbers must be unique."}},
	{meetings.ErrInvalidCursor, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That page cursor is not valid."}},
	{meetings.ErrCommentNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That comment does not exist."}},
	{meetings.ErrEmptyComment, statusError{http.StatusBadRequest, codeEmptyComment, "A comment cannot be empty."}},
	{meetings.ErrKeywordAlertNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That keyword alert does not exist."}},
	{meetings.ErrEmptyPhrase, statusError{http.StatusBadRequest, codeEmptyPhrase, "A keyword alert phrase cannot be empty."}},
	{meetings.ErrInvalidChannel, statusError{http.StatusBadRequest, codeInvalidChannel, "That alert channel is not supported."}},
}

func (s *Server) writeLibraryError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, credits.ErrInsufficientCredits) {
		writeInsufficientCredits(w, r, topUpURLFor(err, s.topUpURL()))
		return
	}
	for _, candidate := range libraryErrors {
		if errors.Is(err, candidate.target) {
			httpx.WriteError(w, r, candidate.result.status, candidate.result.code, candidate.result.message)
			return
		}
	}
	s.writeServiceError(w, r, err)
}
