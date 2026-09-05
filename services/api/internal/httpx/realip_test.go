package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func seenRemoteAddr(t *testing.T, trusted []netip.Prefix, r *http.Request) string {
	t.Helper()
	var seen string
	handler := RealIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		seen = req.RemoteAddr
	}))
	handler.ServeHTTP(httptest.NewRecorder(), r)
	return seen
}

func mustParseTrusted(t *testing.T, raw string) []netip.Prefix {
	t.Helper()
	prefixes, err := ParseTrustedProxies(raw)
	if err != nil {
		t.Fatalf("ParseTrustedProxies(%q): %v", raw, err)
	}
	return prefixes
}

func TestRealIPIgnoresForwardedHeadersFromUntrustedPeers(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.9:41234"
	request.Header.Set(headerForwardedFor, "198.51.100.7")

	got := seenRemoteAddr(t, mustParseTrusted(t, "10.0.0.0/8"), request)

	if got != "203.0.113.9:41234" {
		t.Errorf("RemoteAddr = %q, want the untouched peer address", got)
	}
}

func TestRealIPUsesForwardedHeaderFromATrustedProxy(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.1.2.3:41234"
	request.Header.Set(headerForwardedFor, "198.51.100.7")

	got := seenRemoteAddr(t, mustParseTrusted(t, "10.0.0.0/8"), request)

	if got != "198.51.100.7" {
		t.Errorf("RemoteAddr = %q, want 198.51.100.7", got)
	}
}

func TestRealIPTakesTheRightmostUntrustedHop(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.1.2.3:41234"
	request.Header.Set(headerForwardedFor, "192.0.2.55, 198.51.100.7, 10.9.9.9")

	got := seenRemoteAddr(t, mustParseTrusted(t, "10.0.0.0/8"), request)

	if got != "198.51.100.7" {
		t.Errorf("RemoteAddr = %q, want the rightmost untrusted hop", got)
	}
}

func TestRealIPFallsBackToRealIPHeader(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.1.2.3:41234"
	request.Header.Set(headerRealIP, "198.51.100.7")

	got := seenRemoteAddr(t, mustParseTrusted(t, "10.0.0.0/8"), request)

	if got != "198.51.100.7" {
		t.Errorf("RemoteAddr = %q, want 198.51.100.7", got)
	}
}

func TestRealIPWithoutTrustedProxiesIsAPassThrough(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.1.2.3:41234"
	request.Header.Set(headerForwardedFor, "198.51.100.7")

	got := seenRemoteAddr(t, nil, request)

	if got != "10.1.2.3:41234" {
		t.Errorf("RemoteAddr = %q, want the untouched peer address", got)
	}
}

func TestParseTrustedProxiesAcceptsBareAddressesAndCIDRs(t *testing.T) {
	prefixes := mustParseTrusted(t, "10.0.0.0/8, 172.18.0.4 , 2001:db8::/32")
	if len(prefixes) != 3 {
		t.Fatalf("parsed %d prefixes, want 3", len(prefixes))
	}
	if !prefixes[1].Contains(netip.MustParseAddr("172.18.0.4")) {
		t.Errorf("bare address prefix %s does not contain its own address", prefixes[1])
	}
	if prefixes[1].Contains(netip.MustParseAddr("172.18.0.5")) {
		t.Errorf("bare address prefix %s is wider than a single host", prefixes[1])
	}
}

func TestParseTrustedProxiesRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"not-an-ip", "10.0.0.0/64", "10.0.0.0/"} {
		if _, err := ParseTrustedProxies(raw); err == nil {
			t.Errorf("ParseTrustedProxies(%q) succeeded, want an error", raw)
		}
	}
}

func TestParseTrustedProxiesTreatsBlankAsNone(t *testing.T) {
	prefixes, err := ParseTrustedProxies("   ")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	if len(prefixes) != 0 {
		t.Errorf("parsed %d prefixes, want none", len(prefixes))
	}
}
