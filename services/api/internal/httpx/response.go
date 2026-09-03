package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/hlog"
)

func WriteJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		hlog.FromRequest(r).Error().Err(err).Msg("response encoding failed")
		http.Error(w, `{"error":{"code":"internal_error","message":"Something went wrong."}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		hlog.FromRequest(r).Debug().Err(err).Msg("response write failed")
	}
}
