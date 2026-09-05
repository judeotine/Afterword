package api

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/httpx"
)

const maxRequestBody = 1 << 20

var errBadRequestBody = errors.New("api: request body is not valid json")

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	return decode(w, r, target, false)
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, target any) error {
	return decode(w, r, target, true)
}

func decode(w http.ResponseWriter, r *http.Request, target any, allowEmpty bool) error {
	if r.Body == nil {
		if allowEmpty {
			return nil
		}
		return errBadRequestBody
	}

	reader := http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return nil
		}
		return errBadRequestBody
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errBadRequestBody
	}
	return nil
}

func requestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func pathUUID(r *http.Request, name string) (uuid.UUID, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, name))
	if raw == "" {
		return uuid.Nil, false
	}
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, false
	}
	return parsed, true
}

func writeInvalidBody(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeInvalidRequest, "The request body could not be read as JSON.")
}

func writeValidationError(w http.ResponseWriter, r *http.Request, message string) {
	httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidationFailed, message)
}
