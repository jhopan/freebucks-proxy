package bootcfg

import (
	"os"
	"path/filepath"
	"testing"
)

// writeEnv drops a .env into a fresh temp dir and chdirs there, so
// config.ResolveEnvFile picks it up (the cwd .env wins).
//
// It also disables CLI token auto-discovery. bootcfg.Load wires the same
// DiscoverCLIToken hook Serve uses, so without this the test would read the
// developer's real ~/.config/manicode/credentials.json and stop being
// hermetic (observed: it filled AUTH_TOKENS from a live CLI login).
func writeEnv(t *testing.T, body string) {
	t.Helper()
	t.Setenv("AUTO_DISCOVER_TOKEN", "false")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(body), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Chdir(dir)
}

// unsetEnv removes a variable for the duration of the test. t.Setenv cannot
// express "unset", and for most knobs an empty value behaves like unset
// (override skips empty) -- but AUTH_TOKENS is presence-sensitive, so an
// empty value there would silently mean bridge mode and mask the case under
// test.
func unsetEnv(t *testing.T, name string) {
	t.Helper()
	old, had := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(name, old)
			return
		}
		_ = os.Unsetenv(name)
	})
}

// TestLoadAppliesOverlayOverFile pins the ADR-0019 precedence that -doctor
// used to miss: the DB overlay beats the .env file, and the real process
// environment beats both (config_load.go:59-61).
func TestLoadAppliesOverlayOverFile(t *testing.T) {
	writeEnv(t, "TLS_FINGERPRINT=auto\n")
	unsetEnv(t, "TLS_FINGERPRINT")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatalf("Load(nil overlay): %v", err)
	}
	if cfg.TLSFingerprint != "auto" {
		t.Errorf("no overlay: TLSFingerprint = %q, want the .env value %q", cfg.TLSFingerprint, "auto")
	}

	cfg, err = Load("", map[string]string{"TLS_FINGERPRINT": "bun"})
	if err != nil {
		t.Fatalf("Load(with overlay): %v", err)
	}
	if cfg.TLSFingerprint != "bun" {
		t.Errorf("overlay: TLSFingerprint = %q, want %q -- the DB overlay must beat the .env file", cfg.TLSFingerprint, "bun")
	}
}

// TestLoadOverlayEmptiesAuthTokens pins the VPS case that motivated this
// package. The .env carried one token while the DB overlay held AUTH_TOKENS
// empty. AUTH_TOKENS is presence-sensitive, so the empty overlay value must
// win and force bridge mode. -doctor read the .env alone, probed that token
// and reported "upstream account banned" while /healthz said bridge mode.
func TestLoadOverlayEmptiesAuthTokens(t *testing.T) {
	writeEnv(t, "AUTH_TOKENS=11111111-2222-3333-4444-555555555555\n")
	unsetEnv(t, "AUTH_TOKENS")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatalf("Load(nil overlay): %v", err)
	}
	if cfg.BridgeMode() {
		t.Errorf("no overlay: BridgeMode() = true, want false -- the .env configures one token (got %v)", cfg.AuthTokens)
	}

	cfg, err = Load("", map[string]string{"AUTH_TOKENS": ""})
	if err != nil {
		t.Fatalf("Load(with empty overlay): %v", err)
	}
	if !cfg.BridgeMode() {
		t.Errorf("empty overlay: BridgeMode() = false, want true; tokens = %v", cfg.AuthTokens)
	}
}

// TestSettingsCloseWithoutStore keeps Close safe on the live-only path, where
// the store never opened.
func TestSettingsCloseWithoutStore(t *testing.T) {
	if err := (Settings{}).Close(); err != nil {
		t.Errorf("Close() with no store = %v, want nil", err)
	}
}
