package api

import (
	"net/http"

	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

type searchHitView struct {
	MeetingID    string  `json:"meeting_id"`
	MeetingTitle string  `json:"meeting_title"`
	Seq          int32   `json:"seq"`
	Speaker      string  `json:"speaker,omitempty"`
	StartS       float64 `json:"start_s"`
	EndS         float64 `json:"end_s"`
	Text         string  `json:"text"`
	Rank         float32 `json:"rank"`
}

type searchResultsView struct {
	Query string          `json:"query"`
	Hits  []searchHitView `json:"hits"`
}

func newSearchResultsView(results meetings.SearchResults) searchResultsView {
	view := searchResultsView{Query: results.Query, Hits: make([]searchHitView, 0, len(results.Hits))}
	for _, hit := range results.Hits {
		view.Hits = append(view.Hits, searchHitView{
			MeetingID:    hit.MeetingID.String(),
			MeetingTitle: hit.MeetingTitle,
			Seq:          hit.Seq,
			Speaker:      hit.Speaker,
			StartS:       hit.StartS,
			EndS:         hit.EndS,
			Text:         hit.Text,
			Rank:         hit.Rank,
		})
	}
	return view
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}

	v := validation.New()
	query := v.RequiredString("q", queryString(r, "q"), meetings.MaxSearchQueryLength)
	folderID := queryUUID(v, r, "folder")
	pageSize := queryInt32(v, r, "page_size", meetings.DefaultPageSize, meetings.MaxPageSize)
	if v.Write(w, r) {
		return
	}

	results, err := s.meetings.SearchSegments(r.Context(), membership, query, folderID, pageSize)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newSearchResultsView(results))
}
