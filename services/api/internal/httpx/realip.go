package httpx

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const (
	headerForwardedFor = "X-Forwarded-For"
	headerRealIP       = "X-Real-Ip"
)

func RealIP(trustedProxies []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(trustedProxies) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if client, ok := clientIP(r, trustedProxies); ok {
				r.RemoteAddr = client
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request, trustedProxies []netip.Prefix) (string, bool) {
	peer, err := peerAddr(r.RemoteAddr)
	if err != nil {
		return "", false
	}
	if !isTrusted(peer, trustedProxies) {
		return "", false
	}

	forwarded := candidateAddrs(r.Header.Values(headerForwardedFor))
	for i := len(forwarded) - 1; i >= 0; i-- {
		if !isTrusted(forwarded[i], trustedProxies) {
			return forwarded[i].String(), true
		}
	}
	if len(forwarded) > 0 {
		return forwarded[0].String(), true
	}

	if realIP := candidateAddrs(r.Header.Values(headerRealIP)); len(realIP) == 1 {
		return realIP[0].String(), true
	}
	return "", false
}

func peerAddr(remoteAddr string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return netip.ParseAddr(strings.TrimSpace(host))
}

func candidateAddrs(values []string) []netip.Addr {
	addrs := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			addr, err := netip.ParseAddr(strings.TrimSpace(part))
			if err != nil {
				continue
			}
			addrs = append(addrs, addr.Unmap())
		}
	}
	return addrs
}

func isTrusted(addr netip.Addr, trustedProxies []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, prefix := range trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			prefix, err := netip.ParsePrefix(part)
			if err != nil {
				return nil, err
			}
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(part)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()))
	}
	return prefixes, nil
}
