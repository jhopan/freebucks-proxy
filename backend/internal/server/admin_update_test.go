package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
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
	// An updater that sleeps forces the in-flight flag to stay set; the
	// second concurrent call must get 409 without running anything.
	if os.PathSeparator == '\\' {
		t.Skip("no sleep(5) on cmd.exe fake")
	}
	fakeUpdater(t, "sleep 2; echo 'SUCCESS: freebucks-proxy updated to v1.14.0.11!'; exit 0\n")
	admin := &adminHandlers{logfunc: func() *slog.Logger { return slog.Default() }}

	var firstCode int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
		rec := httptest.NewRecorder()
		admin.handleAdminUpdate(rec, req)
		atomic.StoreInt32(&firstCode, int32(rec.Code))
	}()
	// Wait until the first updater is actually running, then fire the second.
	time.Sleep(300 * time.Millisecond)
	req := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
	rec := httptest.NewRecorder()
	admin.handleAdminUpdate(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("concurrent POST /admin/update = %d, want 409", rec.Code)
	}
	<-done
	if atomic.LoadInt32(&firstCode) != http.StatusOK {
		t.Errorf("first update = %d, want 200", atomic.LoadInt32(&firstCode))
	}
}
