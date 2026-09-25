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
