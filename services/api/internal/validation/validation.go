package validation

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/httpx"
)

const summaryLimit = 3

type Validator struct {
	fields []httpx.FieldError
	seen   map[string]struct{}
}

func New() *Validator {
	return &Validator{seen: map[string]struct{}{}}
}

func (v *Validator) Add(field, message string) {
	if v.seen == nil {
		v.seen = map[string]struct{}{}
	}
	if _, exists := v.seen[field]; exists {
		return
	}
	v.seen[field] = struct{}{}
	v.fields = append(v.fields, httpx.FieldError{Field: field, Message: message})
}

func (v *Validator) Failed() bool {
	return len(v.fields) > 0
}

func (v *Validator) Fields() []httpx.FieldError {
	return append([]httpx.FieldError(nil), v.fields...)
}

func (v *Validator) Message() string {
	if !v.Failed() {
		return ""
	}
	names := make([]string, 0, len(v.fields))
	for _, field := range v.fields {
		if len(names) == summaryLimit {
			names = append(names, "and others")
			break
		}
		names = append(names, field.Field)
	}
	return "The request could not be accepted: check " + strings.Join(names, ", ") + "."
}

func (v *Validator) RequiredString(field, value string, maxLength int) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		v.Add(field, "is required")
		return ""
	}
	if maxLength > 0 && len([]rune(trimmed)) > maxLength {
		v.Add(field, fmt.Sprintf("must be at most %d characters", maxLength))
		return ""
	}
	return trimmed
}

func (v *Validator) OptionalString(field, value string, maxLength int) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if maxLength > 0 && len([]rune(trimmed)) > maxLength {
		v.Add(field, fmt.Sprintf("must be at most %d characters", maxLength))
		return ""
	}
	return trimmed
}

func (v *Validator) OneOf(field, value string, allowed []string, fallback string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		if fallback != "" {
			return fallback
		}
		v.Add(field, "must be one of "+strings.Join(allowed, ", "))
		return ""
	}
	for _, candidate := range allowed {
		if trimmed == candidate {
			return trimmed
		}
	}
	v.Add(field, "must be one of "+strings.Join(allowed, ", "))
	return ""
}

func (v *Validator) UUID(field, value string) uuid.UUID {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		v.Add(field, "is required")
		return uuid.Nil
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil || parsed == uuid.Nil {
		v.Add(field, "is not a valid identifier")
		return uuid.Nil
	}
	return parsed
}

func (v *Validator) Min(field string, value, minimum int64) int64 {
	if value < minimum {
		v.Add(field, fmt.Sprintf("must be at least %d", minimum))
	}
	return value
}

func (v *Validator) Max(field string, value, maximum int64) int64 {
	if value > maximum {
		v.Add(field, fmt.Sprintf("must be at most %d", maximum))
	}
	return value
}

func (v *Validator) MinFloat(field string, value, minimum float64) float64 {
	if value < minimum {
		v.Add(field, fmt.Sprintf("must be at least %g", minimum))
	}
	return value
}

func (v *Validator) Ordered(field string, from, to time.Time) {
	if from.IsZero() || to.IsZero() {
		return
	}
	if to.Before(from) {
		v.Add(field, "must not be earlier than the start of the range")
	}
}

func (v *Validator) Write(w http.ResponseWriter, r *http.Request) bool {
	if !v.Failed() {
		return false
	}
	httpx.WriteFieldErrors(w, r, v.Message(), v.Fields())
	return true
}
