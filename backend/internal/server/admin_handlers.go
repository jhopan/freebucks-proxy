package server

// adminHandlers own the /admin surface (issue #250): every admin handler is
// a method on this struct instead of *Server, so the API/engine surface and
// the admin/dashboard surface no longer share one mutable god struct. The
// deps below are exactly what the admin handlers reach into; Server.New
// wires them once and server.adminHandler dispatches through s.admin.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"freebucks-proxy/backend/internal/config"
	"freebucks-proxy/backend/internal/dashboard"
	"freebucks-proxy/backend/internal/pool"
	"freebucks-proxy/backend/internal/ratelimit"
	"freebucks-proxy/backend/internal/registry"
	"freebucks-proxy/backend/internal/store"
	"freebucks-proxy/backend/internal/upstream"
)

type adminHandlers struct {
	dash *dashboard.Dashboard
	// logfunc reads the Server's CURRENT logger: tests replace it after
	// New (logging_wave2_test.newLoggingServer), so the admin surface must
	// follow it.
	logfunc    func() *slog.Logger
	pool       *pool.Pool
	reg        *registry.Registry
	cfgLoad    func() *config.Config
	cfgStore   func(*config.Config)
	configPath string

	adminAuth   *adminAuth
	adminSaveMu sync.Mutex
	// updateMu serializes POST /admin/update with itself; updateStateMu guards
	// the in-flight flag used by updateTry/updateDone.
	updateMu      sync.Mutex
	updateStateMu sync.Mutex
	updateRunning bool
	loginMu       sync.Mutex
	loginFlows    map[string]*loginFlow
	// authClientFunc reads the Server's current login-wizard client: options
	// may install it after construction.
	authClientFunc func() *upstream.Client
	// settings is the DB settings overlay store (ADR-0019): the same handle
	// as Server.hist (one SQLite file). Nil keeps the settings endpoints on
	// file/env/default with mutations 503.
	settings    *store.Store
	rateLimiter *ratelimit.Limiter

	// handleChat forwards the playground's synthetic chat request to the
	// normal chat pipeline (admin.go:176).
	handleChat func(w http.ResponseWriter, r *http.Request)
}

func (a *adminHandlers) handleAdminRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate configuration before restart to prevent exiting on broken settings
	if _, err := a.loadConfig(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dashboard.RestartResponse{
			Message: "Config validation failed — aborting restart: " + err.Error(),
		})
		return
	}

	a.logfunc().Info("admin restart initiated via dashboard", "remote", remoteHost(r))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dashboard.RestartResponse{
		Message: "Gateway process restart initiated.",
		OK:      true,
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		restartProcess()
	}()
}

// handleAdminUpdate runs the self-updater (the binary's own -update path) as a
// SUBPROCESS and reports the outcome. Design notes:
//   - upstream decision honored: the dashboard never swaps the binary in-process
//     (update.Run exits the process at its conclusion, so in-process reuse is a
//     dead end). A subprocess gets the full updater pipeline (download caps,
//     checksum verification, atomic swap, downgrade guard) and its own exit.
//   - the swap itself is the updater's atomic install; the caller then uses the
//     EXISTING POST /admin/restart to exec the new image (systemd raises it).
//   - serialized with updateMu; a second click while one runs is a 409.
//
// executablePath resolves the running binary for the update subprocess.
// A var so tests can point it at a fake updater.
var executablePath = os.Executable

func (a *adminHandlers) handleAdminUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	// One at a time: the updater replaces the binary under our feet.
	if !a.updateTry() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(dashboard.UpdateResponse{
			Message: "An update is already running.",
			OK:      false,
			Status:  "busy",
		})
		return
	}
	defer a.updateDone()

	exe, err := executablePath()
	if err != nil {
		a.updateJSON(w, http.StatusInternalServerError, "cannot resolve executable: "+err.Error(), "error", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	// Dedicated process group so the updater cannot inherit our signal set:
	// SIGINT to the dashboard would kill the half-installed update.
	cmd := exec.CommandContext(ctx, exe, "-update")
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	a.logfunc().Info("admin update attempt via dashboard", "remote", remoteHost(r), "exit", cmd.ProcessState.ExitCode(), "output", output)

	switch {
	case err == nil && strings.Contains(output, "Already up to date"):
		a.updateJSON(w, http.StatusOK, "Already up to date.", "up_to_date", output)
	case err == nil && strings.Contains(output, "SUCCESS:"):
		a.updateJSON(w, http.StatusOK,
			"Update installed. Use Restart to run the new version.", "updated", output)
	case strings.Contains(output, "Refusing to downgrade"):
		a.updateJSON(w, http.StatusConflict,
			"Updater refused a downgrade (fork version is newer than the release). Set FREEBUFF_UPDATE_ALLOW_DOWNGRADE=1 to override.", "refused_downgrade", output)
	default:
		code := http.StatusInternalServerError
		if err == nil {
			code = http.StatusOK
		}
		a.updateJSON(w, code, "Updater did not complete successfully.", "error", output)
	}
}

// updateTry reports whether the running update slot is free and takes it.
func (a *adminHandlers) updateTry() bool {
	a.updateStateMu.Lock()
	defer a.updateStateMu.Unlock()
	if a.updateRunning {
		return false
	}
	a.updateRunning = true
	return true
}

func (a *adminHandlers) updateDone() {
	a.updateStateMu.Lock()
	a.updateRunning = false
	a.updateStateMu.Unlock()
}

func (a *adminHandlers) updateJSON(w http.ResponseWriter, code int, msg, status, output string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(dashboard.UpdateResponse{Message: msg, OK: status == "updated" || status == "up_to_date", Status: status, Output: output})
}
