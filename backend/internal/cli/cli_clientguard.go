// cli_clientguard.go — the single-client startup guard.
//
// The official CLI and this gateway are two clients for ONE account: both
// read the same credential file (~/.config/manicode/credentials.json) and
// both create an upstream session for it. Upstream admits one seat per
// account — every admission rewrites active_instance_id and refuses the
// disowned instance's next completion with 409 session_superseded
// (upstream/errors.go ErrSessionSuperseded, session_admission.go
// SetReAdmitGate). Running the two side by side therefore produces the
// duplicate-client pattern the fork's own ban post-mortem (fb986b48) flags.
//
// ADOPT_CLI_SESSION is the designed remedy, but it is not sufficient on its
// own: adoptOrCreate trusts the pid recorded in freebuff-instance-owner.json,
// and that file is written when the CLI's session changes — so a CLI that
// has since restarted leaves a stale pid, adoptOwner's liveness check fails
// open, and the gateway creates the competing session it was configured to
// avoid. The live process scan below closes exactly that hole.
package cli

import (
	"fmt"
	"strings"
)

// officialCLINames lists the base names (compared case-insensitively and
// without a trailing .exe) of the official upstream clients. Both ship in
// the same manicode config dir and share the credential file this gateway
// reads. Note "freebucks-proxy" (this binary) does NOT match either name —
// the comparison is exact, never a substring test.
var officialCLINames = []string{"freebuff", "codebuff"}

// normalizeProcName lower-cases a process base name and drops a trailing
// .exe, so a name read from the process table and a name read from /proc
// compare identically.
func normalizeProcName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.TrimSuffix(n, ".exe")
}

// isOfficialCLIName reports whether a process base name is an official
// upstream client.
func isOfficialCLIName(name string) bool {
	n := normalizeProcName(name)
	for _, want := range officialCLINames {
		if n == want {
			return true
		}
	}
	return false
}

// singleClientRefusal returns the fatal startup message when the official CLI
// is running alongside this gateway, or "" when the boot may proceed.
//
// Two knobs short-circuit it. SINGLE_CLIENT_GUARD=false (guardEnabled)
// disables the scan entirely — for hosts where the process table is
// unreadable or meaningless (CI, restricted containers). ADOPT_CLI_SESSION
// (adoptCLISession) keeps the guard on but makes it moot: the gateway adopts
// the CLI's session instead of competing, which is the supported way to run
// both. The guard covers the default case, where nothing stops the gateway
// from claiming the account's seat out from under a live CLI.
func singleClientRefusal(guardEnabled, adoptCLISession bool) string {
	return singleClientRefusalWith(guardEnabled, adoptCLISession, findOfficialCLI)
}

// singleClientRefusalWith is singleClientRefusal with the process scan
// injected, so the decision is testable without a live CLI on the host.
func singleClientRefusalWith(guardEnabled, adoptCLISession bool, find func() (int, string, bool)) string {
	if !guardEnabled || adoptCLISession {
		return ""
	}
	pid, name, ok := find()
	if !ok {
		return ""
	}
	return fmt.Sprintf(
		"the official FreeBuff CLI is already running (process %q, pid %d). "+
			"Both it and this gateway create an upstream session for the SAME account credentials, "+
			"and upstream admits one seat per account: the two clients supersede each other "+
			"(409 session_superseded) and the duplicate-client pattern is a documented ban vector. "+
			"Close the CLI and start the gateway again — or, to run both, set ADOPT_CLI_SESSION=true "+
			"so the gateway adopts the CLI's session instead of competing.",
		name, pid)
}
