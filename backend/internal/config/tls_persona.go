// tls_persona.go — the CLI-persona consistency rules for TLS_FINGERPRINT.
//
// This gateway impersonates the official FreeBuff CLI, which speaks
// Bun/BoringSSL and sends NO browser headers. `bun` is the byte-accurate
// capture of that ClientHello (docs/operations/bun-1.3.14-clienthello.txt),
// so it is the only self-consistent choice. `auto` and `random` resolve to a
// BROWSER preset (stealth.GetProfileForConnection: chrome126/firefox128/
// safari18/edge126) carrying a browser User-Agent and Sec-CH-UA — the
// TLS/header persona then says "browser" while the request envelope says
// "CLI", and the upstream admission lane fingerprints both. Empty stays the
// plain-Go default (no utls persona at all).
//
// Two rules live here, not in the doctor, because both the -doctor diagnostic
// and the serving path need them: the doctor prints them, and Serve logs them
// at startup so a misconfigured deployment (the VPS ran `auto`) surfaces in
// the boot log and the dashboard ring instead of waiting for someone to run
// -doctor.
//
//  1. TLSPersonaWarning — the profile itself must not be a browser persona.
//  2. ALPNPersonaWarning — HTTP2_UPSTREAM must not rewrite the profile's own
//     ALPN list into one no real CLI sends.
package config

import "strings"

// CLIFaithfulProfile reports whether a TLS_FINGERPRINT value reproduces the
// official CLI's OWN ClientHello. Only `bun` does: it is the byte-accurate
// capture of Bun 1.3.14, whose ALPN list is http/1.1 alone. Every other value
// either resolves to a BROWSER preset (a different ClientHello entirely, see
// TLSPersonaWarning) or leaves plain Go TLS with no utls persona.
//
// A new CLI-faithful capture belongs here: both TLSPersonaWarning and
// ALPNPersonaWarning key off this predicate, so a profile added here is
// automatically exempt from the browser-persona warning and automatically
// subject to the ALPN rule.
func CLIFaithfulProfile(name string) bool {
	return strings.ToLower(strings.TrimSpace(name)) == "bun"
}

// TLSPersonaWarning classifies a TLS_FINGERPRINT value against the CLI
// request envelope, returning the message to show and whether it is a
// warning. A warning is raised for any BROWSER persona (auto, random, or a
// named browser preset) and never for `bun` or the empty plain-Go default.
func TLSPersonaWarning(name string) (string, bool) {
	norm := strings.ToLower(strings.TrimSpace(name))
	switch {
	case norm == "":
		return "TLS_FINGERPRINT unset: plain Go TLS on every dial (no utls persona)", false
	case CLIFaithfulProfile(norm):
		return "TLS_FINGERPRINT=" + norm + ": exact Bun 1.3.14 ClientHello, no browser headers (CLI-faithful)", false
	case norm == "auto" || norm == "random":
		return "TLS_FINGERPRINT=" + norm + " resolves to a BROWSER preset (chrome126/firefox128/safari18/edge126) with a browser User-Agent and Sec-CH-UA, but the request envelope impersonates the CLI -- a persona contradiction the upstream admission lane fingerprints. Prefer TLS_FINGERPRINT=bun for the CLI's own ClientHello.", true
	default:
		return "TLS_FINGERPRINT=" + norm + " is a browser TLS persona while the gateway impersonates the CLI (Bun, no browser headers). Prefer TLS_FINGERPRINT=bun unless browser evasion is deliberate.", true
	}
}

// ALPNPersonaWarning reports whether HTTP2_UPSTREAM rewrites the ALPN list of
// a CLI-faithful ClientHello into one no real CLI sends.
//
// stealth.Dialer pins ALPN per dial (stealth/tls.go:102, setALPN) by
// REPLACING the spec's own ALPN extension in place, and upstream/client.go
// passes ["h2","http/1.1"] whenever HTTP2_UPSTREAM is on. The Bun spec pins
// ["http/1.1"] (bun_spec_test.go), and the live CLI capture shows the same
// single entry -- so HTTP2_UPSTREAM=true makes the handshake match neither
// Bun nor Chrome: JA3 is unaffected (it hashes extension types, not ALPN
// values) but JA4 reads the ALPN list, and the CLI never offers h2.
//
// The knob's own rationale (issue #51, "real browsers advertise h2,http/1.1")
// is browser-specific and does not apply to the CLI persona: with
// TLS_FINGERPRINT=bun the faithful value is HTTP2_UPSTREAM=false.
//
// Returns ("", false) when the profile is not CLI-faithful, since there is no
// CLI hello for the ALPN list to contradict (a browser preset wants h2, and
// plain Go has no pinned ALPN at all).
func ALPNPersonaWarning(name string, http2Upstream bool) (string, bool) {
	if !CLIFaithfulProfile(name) {
		return "", false
	}
	if !http2Upstream {
		return "HTTP2_UPSTREAM=false: the CLI-faithful ALPN list [http/1.1] reaches the wire unchanged", false
	}
	return "HTTP2_UPSTREAM=true rewrites the ALPN list of the CLI-faithful ClientHello to [h2, http/1.1] (stealth.setALPN replaces the spec's ALPN in place), but the real CLI advertises [http/1.1] only -- the handshake then matches neither Bun nor Chrome at the JA4 ALPN component. Set HTTP2_UPSTREAM=false alongside TLS_FINGERPRINT=bun.", true
}
