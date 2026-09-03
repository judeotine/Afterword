package httpx

import (
	"context"
	"crypto/rand"
	"net/http"
	"regexp"
	"strings"
)

const (
	RequestIDHeader    = "X-Request-Id"
	requestIDMaxLength = 64
)

type requestIDContextKey struct{}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := acceptableRequestID(r.Header.Get(RequestIDHeader))
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id)))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}

func acceptableRequestID(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if len(candidate) > requestIDMaxLength || !requestIDPattern.MatchString(candidate) {
		return ""
	}
	return candidate
}

func newRequestID() string {
	return rand.Text()
}
