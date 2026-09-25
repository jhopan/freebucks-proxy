//go:build windows

package cli

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// findOfficialCLI scans the process table for a live official CLI and
// returns its pid and normalized base name. Windows uses a Toolhelp32
// snapshot — the same source Task Manager reads — so it sees a process
// regardless of how it was started, unlike a pid taken from the CLI's own
// owner file.
//
// Failure to take the snapshot is not an error for the caller: the guard is
// a safety net, and a gateway that cannot enumerate processes must still
// boot. It reports "none running" (0, "", false) in that case.
func findOfficialCLI() (int, string, bool) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, "", false
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	self := uint32(os.Getpid())

	for err := windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if entry.ProcessID == self {
			continue
		}
		name := windows.UTF16ToString(entry.ExeFile[:])
		if !isOfficialCLIName(name) {
			continue
		}
		return int(entry.ProcessID), normalizeProcName(name), true
	}
	return 0, "", false
}
