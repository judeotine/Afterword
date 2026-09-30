package api

import "testing"

func TestDesktopCallbackPort(t *testing.T) {
	cases := []struct {
		name       string
		redirectTo string
		wantPort   string
		wantOK     bool
	}{
		{"desktop", "/desktop-callback/54321", "54321", true},
		{"web path", "/meetings", "", false},
		{"empty", "", "", false},
		{"prefix only", "/desktop-callback/", "", false},
		{"injection", "/desktop-callback/1/evil", "", false},
		{"query injection", "/desktop-callback/1?x=y", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port, ok := desktopCallbackPort(tc.redirectTo)
			if ok != tc.wantOK || port != tc.wantPort {
				t.Fatalf("desktopCallbackPort(%q) = (%q, %v), want (%q, %v)", tc.redirectTo, port, ok, tc.wantPort, tc.wantOK)
			}
		})
	}
}
