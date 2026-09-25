// tls_persona.go — the CLI-persona consistency rule for TLS_FINGERPRINT.
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
// The rule lives here, not in the doctor, because both the -doctor
// diagnostic and the serving path need it: the doctor prints it, and
// Serve logs it at startup so a misconfigured deployment (the VPS ran
// `auto`) surfaces in the boot log and the dashboard ring instead of
// waiting for someone to run -doctor.
package config

import "strings"

// TLSPersonaWarning classifies a TLS_FINGERPRINT value against the CLI
// request envelope, returning the message to show and whether it is a
// warning. A warning is raised for any BROWSER persona (auto, random, or a
// named browser preset) and never for `bun` or the empty plain-Go default.
func TLSPersonaWarning(name string) (string, bool) {
	norm := strings.ToLower(strings.TrimSpace(name))
	switch norm {
	case "":
		return "TLS_FINGERPRINT unset: plain Go TLS on every dial (no utls persona)", false
	case "bun":
		return "TLS_FINGERPRINT=bun: exact Bun 1.3.14 ClientHello, no browser headers (CLI-faithful)", false
	case "auto", "random":
		return "TLS_FINGERPRINT=" + norm + " resolves to a BROWSER preset (chrome126/firefox128/safari18/edge126) with a browser User-Agent and Sec-CH-UA, but the request envelope impersonates the CLI -- a persona contradiction the upstream admission lane fingerprints. Prefer TLS_FINGERPRINT=bun for the CLI's own ClientHello.", true
	default:
		return "TLS_FINGERPRINT=" + norm + " is a browser TLS persona while the gateway impersonates the CLI (Bun, no browser headers). Prefer TLS_FINGERPRINT=bun unless browser evasion is deliberate.", true
	}
}
