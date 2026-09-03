package httpx

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog/hlog"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type healthResponse struct {
	Status string `json:"status"`
	DB     string `json:"db"`
}

func Health(pinger Pinger, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pinger == nil {
			WriteJSON(w, r, http.StatusServiceUnavailable, healthResponse{Status: "degraded", DB: "unknown"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		if err := pinger.Ping(ctx); err != nil {
			hlog.FromRequest(r).Warn().Err(err).Msg("health check database ping failed")
			WriteJSON(w, r, http.StatusServiceUnavailable, healthResponse{Status: "degraded", DB: "down"})
			return
		}

		WriteJSON(w, r, http.StatusOK, healthResponse{Status: "ok", DB: "up"})
	}
}
