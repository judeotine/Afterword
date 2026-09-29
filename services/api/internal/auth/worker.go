package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/judeotine/afterword/services/api/internal/httpx"
)

const WorkerTokenHeader = "X-Afterword-Worker-Token"

type WorkerAuth struct {
	token string
}

func NewWorkerAuth(token string) *WorkerAuth {
	return &WorkerAuth{token: strings.TrimSpace(token)}
}

func (a *WorkerAuth) Enabled() bool {
	return a.token != ""
}

func (a *WorkerAuth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Enabled() {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, httpx.CodeInternalError, "Worker access is not configured on this deployment.")
			return
		}
		provided := strings.TrimSpace(r.Header.Get(WorkerTokenHeader))
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(a.token)) != 1 {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.CodeUnauthorized, "A valid worker token is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
