package cli

import (
	"strings"
	"testing"
)

// TestIsOfficialCLIName pins the exact-match rule: the official clients are
// recognised with or without an .exe suffix and in any case, while this
// gateway's own binary and lookalike names must NOT match (a substring test
// would make "freebucks-proxy" or "freebuff-helper" trip the guard).
func TestIsOfficialCLIName(t *testing.T) {
	yes := []string{"freebuff", "freebuff.exe", "FREEBUFF.EXE", "FreeBuff", "codebuff", "codebuff.exe"}
	for _, n := range yes {
		if !isOfficialCLIName(n) {
			t.Errorf("isOfficialCLIName(%q) = false, want true", n)
		}
	}
	no := []string{"", "   ", "freebucks-proxy", "freebucks-proxy.exe", "freebuff-helper",
		"freebuffd", "codebuffs", "node", "notfreebuff", "freebuffx"}
	for _, n := range no {
		if isOfficialCLIName(n) {
			t.Errorf("isOfficialCLIName(%q) = true, want false", n)
		}
	}
}

// TestNormalizeProcName pins the shared normaliser: /proc/comm and the
// Windows process table must reduce to the same token.
func TestNormalizeProcName(t *testing.T) {
	cases := map[string]string{
		"freebuff.exe": "freebuff",
		"FREEBUFF.EXE": "freebuff",
		"  freebuff  ": "freebuff",
		"freebuff":     "freebuff",
	}
	for in, want := range cases {
		if got := normalizeProcName(in); got != want {
			t.Errorf("normalizeProcName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSingleClientRefusal covers the guard's decision table: a live CLI
// refuses the boot, SINGLE_CLIENT_GUARD=false disables the scan, and
// ADOPT_CLI_SESSION makes it moot; no CLI is a clean boot. The scan is
// injected so this stays hermetic.
func TestSingleClientRefusal(t *testing.T) {
	hit := func() (int, string, bool) { return 4242, "freebuff", true }
	miss := func() (int, string, bool) { return 0, "", false }

	t.Run("live CLI refuses the boot", func(t *testing.T) {
		msg := singleClientRefusalWith(true, false, hit)
		if msg == "" {
			t.Fatal("singleClientRefusalWith(true, false, hit) = \"\", want a refusal")
		}
		// The message must name the offending process and both ways out.
		for _, want := range []string{"freebuff", "4242", "ADOPT_CLI_SESSION=true"} {
			if !strings.Contains(msg, want) {
				t.Errorf("refusal %q does not mention %q", msg, want)
			}
		}
	})

	t.Run("SINGLE_CLIENT_GUARD=false disables the scan", func(t *testing.T) {
		if msg := singleClientRefusalWith(false, false, hit); msg != "" {
			t.Errorf("singleClientRefusalWith(false, false, hit) = %q, want \"\"", msg)
		}
	})

	t.Run("ADOPT_CLI_SESSION makes the guard moot", func(t *testing.T) {
		if msg := singleClientRefusalWith(true, true, hit); msg != "" {
			t.Errorf("singleClientRefusalWith(true, true, hit) = %q, want \"\"", msg)
		}
	})

	t.Run("no CLI is a clean boot", func(t *testing.T) {
		if msg := singleClientRefusalWith(true, false, miss); msg != "" {
			t.Errorf("singleClientRefusalWith(true, false, miss) = %q, want \"\"", msg)
		}
	})
}
