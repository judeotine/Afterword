package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveWithRequestID(t *testing.T, inbound string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if inbound != "" {
		request.Header.Set(RequestIDHeader, inbound)
	}
	recorder := httptest.NewRecorder()

	var seen string
	RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	})).ServeHTTP(recorder, request)

	return recorder, seen
}

func TestRequestIDKeepsAWellFormedInboundID(t *testing.T) {
	recorder, seen := serveWithRequestID(t, "edge-7f3a.b2_9")

	if seen != "edge-7f3a.b2_9" {
		t.Errorf("context id = %q, want the inbound id", seen)
	}
	if got := recorder.Header().Get(RequestIDHeader); got != "edge-7f3a.b2_9" {
		t.Errorf("response header = %q, want the inbound id", got)
	}
}

func TestRequestIDReplacesUnacceptableInboundIDs(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"spaces":           "has spaces",
		"newline":          "abc\ndef",
		"carriage return":  "abc\rdef",
		"semicolon":        "abc;def",
		"slash":            "abc/def",
		"unicode":          "abcdéf",
		"too long":         strings.Repeat("a", 65),
		"null byte":        "abc\x00def",
		"header injection": "id\r\nSet-Cookie: a=b",
	}
	for name, inbound := range cases {
		t.Run(name, func(t *testing.T) {
			recorder, seen := serveWithRequestID(t, inbound)

			if seen == inbound {
				t.Fatalf("context id = %q, want a generated replacement", seen)
			}
			if !requestIDPattern.MatchString(seen) {
				t.Errorf("generated id %q does not match the accepted pattern", seen)
			}
			if got := recorder.Header().Get(RequestIDHeader); got != seen {
				t.Errorf("response header = %q, want the generated id %q", got, seen)
			}
		})
	}
}

func TestRequestIDTrimsSurroundingWhitespace(t *testing.T) {
	_, seen := serveWithRequestID(t, "  trace-42  ")

	if seen != "trace-42" {
		t.Errorf("context id = %q, want the trimmed inbound id", seen)
	}
}

func TestRequestIDAcceptsTheLongestAllowedID(t *testing.T) {
	longest := strings.Repeat("a", 64)
	_, seen := serveWithRequestID(t, longest)

	if seen != longest {
		t.Errorf("context id = %q, want the 64 character inbound id", seen)
	}
}

func TestRequestIDGeneratesDistinctIDs(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		_, id := serveWithRequestID(t, "")
		if seen[id] {
			t.Fatalf("generated id %q twice", id)
		}
		seen[id] = true
	}
}

func TestRequestIDFromContextIsEmptyWithoutTheMiddleware(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if id := RequestIDFromContext(request.Context()); id != "" {
		t.Errorf("id = %q, want empty", id)
	}
}
