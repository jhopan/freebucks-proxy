package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// withUpdater swaps updateRunner for a canned transcript and restores it.
func withUpdater(t *testing.T, fn func() (string, error)) {
	t.Helper()
	old := updateRunner
	updateRunner = fn
	t.Cleanup(func() { updateRunner = old })
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
	withUpdater(t, func() (string, error) {
		return "freebucks-proxy self-updater\nLatest release: v1.14.0.10\nAlready up to date!", nil
	})
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
	withUpdater(t, func() (string, error) {
		return "Checksum verified successfully [ok]\nSUCCESS: freebucks-proxy updated to v1.14.0.12!", nil
	})
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
		Output string `json:"output"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Status != "updated" || !res.OK || res.Output == "" {
		t.Errorf("status = %q ok = %v output empty=%v, want updated/true/false", res.Status, res.OK, res.Output == "")
	}
}

func TestAdminUpdateRefusesDowngrade(t *testing.T) {
	withUpdater(t, func() (string, error) {
		return "Refusing to downgrade: running 1.14.0.9 is newer than latest release v1.13.0.", nil
	})
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

func TestAdminUpdateError(t *testing.T) {
	withUpdater(t, func() (string, error) {
		return "download failed", errors.New("boom")
	})
	admin := &adminHandlers{logfunc: func() *slog.Logger { return slog.Default() }}
	req := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
	rec := httptest.NewRecorder()
	admin.handleAdminUpdate(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("POST /admin/update = %d, want 500: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminUpdateBusyWhileRunning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	withUpdater(t, func() (string, error) {
		// Signal that the first handler took the slot and is now
		// parked inside the runner — the 409 window is open.
		close(started)
		<-release
		return "SUCCESS: freebucks-proxy updated to v1.14.0.12!", nil
	})
	admin := &adminHandlers{logfunc: func() *slog.Logger { return slog.Default() }}

	// First call takes the slot and blocks inside the (fake) updater.
	firstDone := make(chan int, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
		rec := httptest.NewRecorder()
		admin.handleAdminUpdate(rec, req)
		firstDone <- rec.Code
	}()
	select {
	case <-started:
	case code := <-firstDone:
		t.Fatalf("first update finished without parking (%d); busy window untestable", code)
	}

	// Second call must get 409 while the first is parked in the runner.
	req := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
	rec := httptest.NewRecorder()
	admin.handleAdminUpdate(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("concurrent POST /admin/update = %d, want 409: %s", rec.Code, rec.Body.String())
	}

	// Release the first; it completes with 200 updated.
	close(release)
	if code := <-firstDone; code != http.StatusOK {
		t.Errorf("first update = %d, want 200", code)
	}

	// Slot freed: a third call works again.
	withUpdater(t, func() (string, error) {
		return "Already up to date!", nil
	})
	req3 := httptest.NewRequest(http.MethodPost, "/admin/update", nil)
	rec3 := httptest.NewRecorder()
	admin.handleAdminUpdate(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("POST after completion = %d, want 200", rec3.Code)
	}
}
