//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// findOfficialCLI scans the process table for a live official CLI and
// returns its pid and normalized base name.
//
// Linux: /proc/<pid>/comm carries the process base name, so the scan is a
// directory walk with no external command (a pgrep shell-out would add a
// PATH dependency to a startup path). On platforms without /proc (macOS,
// BSD) the read fails and the guard degrades to "none running" — it is a
// safety net, and a gateway that cannot enumerate processes must still boot.
func findOfficialCLI() (int, string, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, "", false
	}
	self := os.Getpid()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(raw))
		if !isOfficialCLIName(name) {
			continue
		}
		return pid, normalizeProcName(name), true
	}
	return 0, "", false
}
