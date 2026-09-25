// Package bootcfg loads the effective configuration for a CLI entry point
// that must see exactly what Serve sees.
//
// Serve reads the ADR-0019 settings overlay out of the history store before
// its first config.Load, so a knob persisted from the dashboard beats the
// .env file (env > db > file > default). -doctor used to skip that step and
// call config.Load directly, which meant it could report a configuration the
// running server does not use. Observed live on the VPS: the .env held one
// token while the DB overlay emptied AUTH_TOKENS, so -doctor probed a token
// the server never touched and reported "upstream account banned" while
// /healthz said bridge mode.
//
// The wiring lives here once so both entry points share it instead of
// drifting: adding a LoadOption for Serve alone would silently reopen the
// same class of gap.
package bootcfg

import (
	"fmt"

	"freebucks-proxy/backend/internal/clicreds"
	"freebucks-proxy/backend/internal/config"
	"freebucks-proxy/backend/internal/store"
)

// Notice is a non-fatal message raised while resolving the configuration.
// Serve prints every one to stderr; -doctor renders Warn as a warning and the
// rest as an informational check, so "applying N DB setting override(s)" does
// not read as a problem.
type Notice struct {
	Warn bool
	Msg  string
}

// Settings is the opened settings store plus the overlay it contributes.
type Settings struct {
	// Store is the open history/settings handle, nil when the store could
	// not be opened (the caller then runs live-only). Serve keeps it for the
	// history wiring; a caller that only needs the config should Close it.
	Store *store.Store
	// Overlay holds the store's `config:` rows, ready for
	// config.LoadOptions.Overlay. Nil when the store is unavailable or
	// contributes nothing.
	Overlay map[string]string
	// Migrate is the boot migration status; Serve reports it on the dashboard.
	Migrate store.MigrateStatus
	// Notices are non-fatal messages the caller surfaces.
	Notices []Notice
}

// Open opens the settings store and reads its configuration overlay.
//
// Every failure is non-fatal by design: Serve keeps running live-only, and
// -doctor keeps diagnosing on file/env. The caller gets whatever succeeded
// plus a notice explaining what was skipped.
func Open() Settings {
	s := Settings{Migrate: store.MigrateStatus{Applied: []int{}}}
	st, ms, err := store.OpenWithStatus(store.DBPathFromEnv())
	if err != nil {
		s.Notices = append(s.Notices, Notice{Warn: true, Msg: fmt.Sprintf("settings store unavailable; running live-only: %v", err)})
		return s
	}
	s.Store = st
	s.Migrate = ms
	rows, err := st.ListSettings()
	if err != nil {
		s.Notices = append(s.Notices, Notice{Warn: true, Msg: fmt.Sprintf("settings overlay unreadable; running on file/env: %v", err)})
		return s
	}
	if ov := config.OverlayFromRows(rows); len(ov) > 0 {
		s.Overlay = ov
		s.Notices = append(s.Notices, Notice{Msg: fmt.Sprintf("applying %d DB setting override(s)", len(ov))})
	}
	return s
}

// Close releases the store handle when one was opened. A Settings whose store
// never opened (live-only) closes cleanly.
func (s Settings) Close() error {
	if s.Store == nil {
		return nil
	}
	return s.Store.Close()
}

// Load returns the effective configuration exactly as Serve resolves it:
// env > db overlay > file > default, with the same CLI-token auto-discovery
// (issue #283). It is the single definition of "what the server will actually
// run with", so a diagnostic cannot report a configuration the server ignores.
func Load(configPath string, overlay map[string]string) (config.Config, error) {
	return config.LoadOpts(configPath, config.LoadOptions{
		DiscoverCLIToken: clicreds.DiscoverToken,
		Overlay:          overlay,
	})
}
