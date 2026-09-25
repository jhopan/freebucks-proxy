package config

import (
	"strings"
	"testing"
)

// TestTLSPersonaWarning pins the CLI-persona rule: only `bun` and the empty
// plain-Go default are consistent with the CLI request envelope; every
// browser persona (auto, random, or a named preset) warns and points the
// operator at the CLI-faithful knob.
func TestTLSPersonaWarning(t *testing.T) {
	cases := []struct {
		in       string
		wantWarn bool
	}{
		{"", false},
		{"bun", false},
		{"BUN", false},
		{"  bun  ", false},
		{"auto", true},
		{"random", true},
		{"AUTO", true},
		{"chrome120", true},
		{"chrome126", true},
		{"safari18", true},
		{"firefox128", true},
		{"edge126", true},
	}
	for _, tc := range cases {
		msg, warn := TLSPersonaWarning(tc.in)
		if warn != tc.wantWarn {
			t.Errorf("TLSPersonaWarning(%q) warn = %v, want %v", tc.in, warn, tc.wantWarn)
		}
		if msg == "" {
			t.Errorf("TLSPersonaWarning(%q) returned an empty message", tc.in)
		}
		// Every warning must name the CLI-faithful remediation.
		if warn && !strings.Contains(msg, "TLS_FINGERPRINT=bun") {
			t.Errorf("TLSPersonaWarning(%q) = %q, want the bun remediation", tc.in, msg)
		}
	}
}

// TestCLIFaithfulProfile pins the single source of truth for "this profile
// reproduces the official CLI's OWN ClientHello". Only `bun` does, so only
// `bun` is exempt from the browser-persona warning and subject to the ALPN
// rule; a new capture added here inherits both.
func TestCLIFaithfulProfile(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"bun", true},
		{"BUN", true},
		{"  Bun  ", true},
		{"", false},
		{"   ", false},
		{"auto", false},
		{"random", false},
		{"chrome126", false},
		{"safari18", false},
		{"bun1", false},
	}
	for _, tc := range cases {
		if got := CLIFaithfulProfile(tc.in); got != tc.want {
			t.Errorf("CLIFaithfulProfile(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestALPNPersonaWarning pins the ALPN rule found on the VPS: with
// TLS_FINGERPRINT=bun, HTTP2_UPSTREAM=true makes stealth.setALPN replace the
// spec's pinned [http/1.1] with [h2, http/1.1], which no real CLI ever sends
// (the live CLI 0.0.194 MITM capture shows the single entry). JA3 hashes
// extension types so it is unaffected; JA4 reads the ALPN list. Profiles that
// are not CLI-faithful are out of scope.
func TestALPNPersonaWarning(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		http2     bool
		wantEmpty bool
		wantWarn  bool
	}{
		{"bun + h2 contradicts the capture", "bun", true, false, true},
		{"bun + h1 is faithful", "bun", false, false, false},
		{"bun is case-insensitive", "BUN", true, false, true},
		{"unset is out of scope", "", true, true, false},
		{"browser preset wants h2", "chrome126", true, true, false},
		{"auto is a browser preset", "auto", true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, warn := ALPNPersonaWarning(tc.in, tc.http2)
			if (msg == "") != tc.wantEmpty {
				t.Errorf("ALPNPersonaWarning(%q, %v) msg = %q, wantEmpty = %v", tc.in, tc.http2, msg, tc.wantEmpty)
			}
			if warn != tc.wantWarn {
				t.Errorf("ALPNPersonaWarning(%q, %v) warn = %v, want %v", tc.in, tc.http2, warn, tc.wantWarn)
			}
			if warn && !strings.Contains(msg, "HTTP2_UPSTREAM=false") {
				t.Errorf("ALPNPersonaWarning(%q, %v) = %q, want the HTTP2_UPSTREAM=false remediation", tc.in, tc.http2, msg)
			}
		})
	}
}
