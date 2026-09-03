package httpx

import "net/http"

const (
	CodeNotFound           = "not_found"
	CodeMethodNotAllowed   = "method_not_allowed"
	CodeInternalError      = "internal_error"
	CodeRequestTimeout     = "request_timeout"
	CodeServiceUnavailable = "service_unavailable"
	CodeUnauthorized       = "unauthorized"
	CodeTokenExpired       = "token_expired"
	CodeForbidden          = "forbidden"
	CodeInvalidRequest     = "invalid_request"
	CodeValidationFailed   = "validation_failed"
	CodeRateLimited        = "rate_limited"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorBody struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

func NewErrorEnvelope(code, message string) ErrorEnvelope {
	return ErrorEnvelope{Error: ErrorBody{Code: code, Message: message}}
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteJSON(w, r, status, NewErrorEnvelope(code, message))
}

func WriteFieldErrors(w http.ResponseWriter, r *http.Request, message string, fields []FieldError) {
	envelope := NewErrorEnvelope(CodeValidationFailed, message)
	envelope.Error.Fields = fields
	WriteJSON(w, r, http.StatusBadRequest, envelope)
}

func NotFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusNotFound, CodeNotFound, "The requested resource does not exist.")
	}
}

func MethodNotAllowedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "That method is not allowed on this resource.")
	}
}
