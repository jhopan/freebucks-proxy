package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeUpdater writes a shell script that prints a canned updater transcript
// and returns the given exit code, then points executablePath at it.
func fakeUpdater(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	ext := ""
	if os.PathSeparator == '\\' {
		ext = ".bat"
		script = "@echo off\r\n" + script
	} else {
		script = "#!/bin/sh\n" + script
	}
	p := filepath.Join(dir, "fake-updater"+ext)
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake updater: %v", err)
	}
	old := executablePath
	executablePath = func() (string, error) { return p, nil }
	t.Cleanup(func() { executablePath = old })
}

func TestAdminUpdateMethodNotAllowed(t *testing.T) {
	admin := &adminHandlers{logfunc: func() *slog.Logger { return slog.Default() }}
	req := httptest.NewRequest(http.MethodGet, "/admin/update", nil)
	rec := httptest.NewRecorder()
	admin.handleAdminUpdate(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /admin/update = %d, want 405", rec.Code)
	}
}

func TestAdminUpdateAlreadyUpToDate(t *testing.T) {
	fakeUpdater(t, "echo 'freebucks-proxy self-updater'; echo 'Latest release: v1.14.0.10'; echo 'Already up to date!'; exit 0\n")
	admin := &adminHandlers{logfunc: func() *slog.Logger { return slog.Default() }}
	req := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
	rec := httptest.NewRecorder()
	admin.handleAdminUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/update = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Status != "up_to_date" || !res.OK {
		t.Errorf("status = %q ok = %v, want up_to_date/true", res.Status, res.OK)
	}
}

func TestAdminUpdateSuccess(t *testing.T) {
	fakeUpdater(t, "echo 'Checksum verified successfully [ok]'; echo 'SUCCESS: freebucks-proxy updated to v1.14.0.11!'; exit 0\n")
	admin := &adminHandlers{logfunc: func() *slog.Logger { return slog.Default() }}
	req := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
	rec := httptest.NewRecorder()
	admin.handleAdminUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/update = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Status != "updated" || !res.OK {
		t.Errorf("status = %q ok = %v, want updated/true", res.Status, res.OK)
	}
}

func TestAdminUpdateRefusesDowngrade(t *testing.T) {
	fakeUpdater(t, "echo 'Refusing to downgrade: running 1.14.0.9 is newer than latest release v1.13.0.'; exit 0\n")
	admin := &adminHandlers{logfunc: func() *slog.Logger { return slog.Default() }}
	req := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
	rec := httptest.NewRecorder()
	admin.handleAdminUpdate(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST /admin/update = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Status != "refused_downgrade" {
		t.Errorf("status = %q, want refused_downgrade", res.Status)
	}
}

func TestAdminUpdateBusyWhileRunning(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("no sleep(5) on cmd.exe fake")
	}
	fakeUpdater(t, "sleep 2; echo 'SUCCESS: freebucks-proxy updated to v1.14.0.11!'; exit 0\n")
	admin := &adminHandlers{logfunc: func() *slog.Logger { return slog.Default() }}

	done := make(chan int, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
		rec := httptest.NewRecorder()
		admin.handleAdminUpdate(rec, req)
		done <- rec.Code
	}()

	// Fire follow-up attempts until one lands on 409 (the first updater is
	// in-flight) or the deadline passes. Polling beats a fixed sleep: CI
	// scheduling may start the second request before the first one takes
	// the update slot.
	deadline := time.Now().Add(5 * time.Second)
	var saw409 bool
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
		rec := httptest.NewRecorder()
		admin.handleAdminUpdate(rec, req)
		if rec.Code == http.StatusConflict {
			saw409 = true
			break
		}
		select {
		case code := <-done:
			t.Fatalf("first update finished early with %d; busy window not observed", code)
		default:
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !saw409 {
		t.Fatal("concurrent POST /admin/update never returned 409")
	}
	if code := <-done; code != http.StatusOK {
		t.Errorf("first update = %d, want 200", code)
	}
}
