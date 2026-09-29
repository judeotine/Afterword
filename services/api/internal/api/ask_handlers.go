package api

import (
	"net/http"

	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

type askCitationView struct {
	MeetingID    string  `json:"meeting_id"`
	MeetingTitle string  `json:"meeting_title"`
	StartS       float64 `json:"start_s"`
	Text         string  `json:"text"`
}

type askResultView struct {
	Answer    string            `json:"answer"`
	Citations []askCitationView `json:"citations"`
}

type askRequest struct {
	Question string `json:"question"`
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}
	var body askRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	v := validation.New()
	question := v.RequiredString("question", body.Question, meetings.MaxAskQuestionLength)
	if v.Write(w, r) {
		return
	}

	result, err := s.meetings.Ask(r.Context(), membership, question)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	view := askResultView{Answer: result.Answer, Citations: make([]askCitationView, 0, len(result.Citations))}
	for _, citation := range result.Citations {
		view.Citations = append(view.Citations, askCitationView{
			MeetingID:    citation.MeetingID.String(),
			MeetingTitle: citation.MeetingTitle,
			StartS:       citation.StartS,
			Text:         citation.Text,
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}
