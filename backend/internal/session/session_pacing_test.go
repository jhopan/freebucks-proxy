package session

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"freebucks-proxy/backend/internal/testutil"
)

// setPacingForTest shortens the burst-mitigation delays so tests do not
// sleep for seconds. Restores the originals on cleanup.
func setPacingForTest(t *testing.T, pace, backoff time.Duration) {
	t.Helper()
	oldPace, oldBackoff := ReAdmitPacingDelay, modelLockUnresolvedBackoff
	ReAdmitPacingDelay, modelLockUnresolvedBackoff = pace, backoff
	t.Cleanup(func() { ReAdmitPacingDelay, modelLockUnresolvedBackoff = oldPace, oldBackoff })
}

// TestModelLockReleaseResolvesInstanceID pins the fork fix for the
// 2026-09-24 ban sequence: a model_locked refusal that carries NO instanceId
// (fresh manager, slot admitted out of band) must resolve the live instance
// id with ONE instance-free GET before releasing, so the DELETE carries the
// id upstream expects — not the 400 instance_required + re-admit burst that
// burned the account.
func TestModelLockReleaseResolvesInstanceID(t *testing.T) {
	setPacingForTest(t, time.Millisecond, time.Millisecond)

	mock := testutil.NewMock()
	defer mock.Close()

	var creates, gets, ends atomic.Int32
	var mu sync.Mutex
	var deleteIDs []string
	bAttempts := 0
	mock.SessionHandler = func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			creates.Add(1)
			w.Header().Set("Content-Type", "application/json")
			model := r.Header.Get("x-freebuff-model")
			if model == "model/B" {
				mu.Lock()
				bAttempts++
				first := bAttempts == 1
				mu.Unlock()
				if first {
					// No instanceId in the refusal body (the bug shape).
					_, _ = io.WriteString(w, `{"status":"model_locked","currentModel":"model/A","requestedModel":"model/B"}`)
					return
				}
			}
			_, _ = io.WriteString(w, `{"status":"active","instanceId":"inst-B","model":"model/B","expiresAt":"2030-01-01T00:00:00Z"}`)
		case http.MethodGet:
			// The instance-free poll: answer with the live locked session.
			gets.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"active","instanceId":"inst-A","model":"model/A","expiresAt":"2030-01-01T00:00:00Z"}`)
		case http.MethodDelete:
			ends.Add(1)
			mu.Lock()
			deleteIDs = append(deleteIDs, r.Header.Get("x-freebuff-instance-id"))
			mu.Unlock()
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"status":"ended"}`)
		default:
			http.NotFound(w, r)
		}
	}

	mgr := newTestManager(t, mock)
	if _, err := mgr.EnsureSessionForModel(context.Background(), "model/B"); err != nil {
		t.Fatal(err)
	}

	if gets.Load() == 0 {
		t.Errorf("instance-free GET = 0, want >= 1 (the resolver poll)")
	}
	mu.Lock()
	ids := append([]string(nil), deleteIDs...)
	mu.Unlock()
	found := false
	for _, id := range ids {
		if id == "inst-A" {
			found = true
		}
	}
	if !found {
		t.Errorf("DELETE carried ids %v, want inst-A (resolved via poll)", ids)
	}
}

// TestModelLockUnresolvedParksBeforeRetry pins the other half: when the
// resolver poll yields nothing either, the refresh must NOT re-admit at
// machine speed — it parks for modelLockUnresolvedBackoff first.
func TestModelLockUnresolvedParksBeforeRetry(t *testing.T) {
	setPacingForTest(t, time.Millisecond, 750*time.Millisecond)

	mock := testutil.NewMock()
	defer mock.Close()

	var creates atomic.Int32
	start := time.Now()
	mock.SessionHandler = func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			creates.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"model_locked","currentModel":"model/A","requestedModel":"model/B"}`)
		case http.MethodGet:
			// Resolver poll finds nothing (empty instance id).
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"ended"}`)
		default:
			http.NotFound(w, r)
		}
	}

	mgr := newTestManager(t, mock)
	_, err := mgr.EnsureSessionForModel(context.Background(), "model/B")
	if err == nil {
		t.Fatal("err = nil, want refresh budget exhaustion")
	}
	elapsed := time.Since(start)
	if elapsed < 700*time.Millisecond {
		t.Errorf("refresh finished in %s, want >= the park backoff (750ms) — re-admit burst not paced", elapsed)
	}
	if got := creates.Load(); got < 2 {
		t.Errorf("creates = %d, want >= 2 (initial + at least one parked retry)", got)
	}
}

// TestReAdmitPacingSpreadsCreates pins the pacing delay between consecutive
// admissions inside one refresh loop (the observed 4-creates-in-629ms burst).
func TestReAdmitPacingSpreadsCreates(t *testing.T) {
	setPacingForTest(t, 300*time.Millisecond, time.Millisecond)

	mock := testutil.NewMock()
	defer mock.Close()

	var creates atomic.Int32
	var first, last time.Time
	var mu sync.Mutex
	mock.SessionHandler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creates.Add(1)
			mu.Lock()
			if first.IsZero() {
				first = time.Now()
			}
			last = time.Now()
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"model_locked","currentModel":"model/A","requestedModel":"model/B"}`)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"ended"}`)
			return
		}
		http.NotFound(w, r)
	}

	mgr := newTestManager(t, mock)
	_, _ = mgr.EnsureSessionForModel(context.Background(), "model/B") // budget-exhaust error expected

	mu.Lock()
	gap := last.Sub(first)
	mu.Unlock()
	if creates.Load() < 2 {
		t.Fatalf("creates = %d, want >= 2", creates.Load())
	}
	if gap < 250*time.Millisecond {
		t.Errorf("create spread = %s, want >= ~pace (300ms)", gap)
	}
	_ = strings.TrimSpace("")
}
