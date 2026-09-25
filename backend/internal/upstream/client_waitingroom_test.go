package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"freebucks-proxy/backend/internal/config"
	"freebucks-proxy/backend/internal/testutil"
)

const waitingRoomBody = `{"error":{"message":"The model is temporarily unavailable. Please try again later.","code":503}}`

// fastWaitingRoomSeam replaces the vendor poll backoff with a negligible sleep
// so retry-shape tests do not sit out the real 10-20s window. It records every
// (attempt, retryAfter) pair it is asked for, which is how the tests assert the
// parsed Retry-After actually reaches the backoff.
func fastWaitingRoomSeam(client *Client, seen *[]waitingRoomCall) {
	client.waitingRoomBackoffFn = func(attempt int, retryAfter time.Duration) time.Duration {
		if seen != nil {
			*seen = append(*seen, waitingRoomCall{attempt: attempt, retryAfter: retryAfter})
		}
		return time.Millisecond
	}
}

type waitingRoomCall struct {
	attempt    int
	retryAfter time.Duration
}

// TestChatCompletionsRetriesWaitingRoomSameSession: the proxy must wait out
// the upstream waiting room in-request (CLI parity: the session loop keeps
// retrying 503s, and send-message.ts:610-619 treats waiting_room_queued as
// "we'll wait") instead of surfacing an instant 503.
func TestChatCompletionsRetriesWaitingRoomSameSession(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)

	t.Run("503 then 200 returns stream", func(t *testing.T) {
		mock := testutil.NewMock()
		defer mock.Close()
		calls := 0
		mock.ChatHandler = func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, waitingRoomBody)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			_, _ = io.WriteString(w, testutil.SSEEvent(`{"id":"x","object":"chat.completion.chunk","choices":[]}`))
		}
		client, err := New("tok-a", testConfig(mock.URL(), func(c *config.Config) {
			c.TransientRetries = 1
			c.WaitingRoomRetries = 1
		}))
		if err != nil {
			t.Fatal(err)
		}
		var seen []waitingRoomCall
		fastWaitingRoomSeam(client, &seen)

		rc, err := client.ChatCompletions(context.Background(), ChatOptions{Model: "m", RunID: "r", SessionInstanceID: "inst-1"}, body)
		if err != nil {
			t.Fatalf("ChatCompletions after waiting-room retry: %v", err)
		}
		_ = rc.Close()
		if calls != 2 {
			t.Errorf("upstream chat calls = %d, want 2 (original + same-session retry)", calls)
		}
		if got := client.WaitingRoomRetries(); got != 1 {
			t.Errorf("WaitingRoomRetries = %d, want 1", got)
		}
		if got := client.CapacityDeferredRetries(); got != 0 {
			t.Errorf("CapacityDeferredRetries = %d, want 0 (waiting room must not pollute the capacity counter)", got)
		}
		if len(seen) != 1 || seen[0].attempt != 1 {
			t.Fatalf("backoff calls = %+v, want one call with attempt 1", seen)
		}
		if seen[0].retryAfter != 0 {
			t.Errorf("retryAfter = %v, want 0 (no Retry-After header on the refusal)", seen[0].retryAfter)
		}
		if len(mock.RecordedChatBodies) != 2 {
			t.Fatalf("recorded %d chat requests, want 2", len(mock.RecordedChatBodies))
		}
		if mock.RecordedChatBodies[0] != mock.RecordedChatBodies[1] {
			t.Error("retried body differs from original (must be byte-identical)")
		}
	})

	t.Run("429 queued then 200 returns stream", func(t *testing.T) {
		mock := testutil.NewMock()
		defer mock.Close()
		calls := 0
		mock.ChatHandler = func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"error":{"code":"waiting_room_queued","message":"row caught mid-admit"}}`)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			_, _ = io.WriteString(w, testutil.SSEEvent(`{"id":"x","object":"chat.completion.chunk","choices":[]}`))
		}
		client, err := New("tok-d", testConfig(mock.URL(), func(c *config.Config) {
			c.TransientRetries = 1
			c.WaitingRoomRetries = 1
		}))
		if err != nil {
			t.Fatal(err)
		}
		fastWaitingRoomSeam(client, nil)

		rc, err := client.ChatCompletions(context.Background(), ChatOptions{Model: "m", RunID: "r"}, body)
		if err != nil {
			t.Fatalf("ChatCompletions after queued retry: %v", err)
		}
		_ = rc.Close()
		if calls != 2 {
			t.Errorf("upstream chat calls = %d, want 2 (original + same-session retry)", calls)
		}
		if got := client.WaitingRoomRetries(); got != 1 {
			t.Errorf("WaitingRoomRetries = %d, want 1", got)
		}
	})

	t.Run("Retry-After reaches the backoff as the floor", func(t *testing.T) {
		mock := testutil.NewMock()
		defer mock.Close()
		mock.ChatHandler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "11")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, waitingRoomBody)
		}
		client, err := New("tok-b", testConfig(mock.URL(), func(c *config.Config) {
			c.TransientRetries = 1
			c.WaitingRoomRetries = 1
		}))
		if err != nil {
			t.Fatal(err)
		}
		var seen []waitingRoomCall
		fastWaitingRoomSeam(client, &seen)

		_, err = client.ChatCompletions(context.Background(), ChatOptions{Model: "m", RunID: "r"}, body)
		if !errors.Is(err, ErrWaitingRoom) {
			t.Fatalf("err = %v, want ErrWaitingRoom", err)
		}
		if len(seen) != 1 {
			t.Fatalf("backoff calls = %+v, want 1", seen)
		}
		if seen[0].retryAfter != 11*time.Second {
			t.Errorf("retryAfter = %v, want 11s parsed from the Retry-After header", seen[0].retryAfter)
		}
	})

	t.Run("zero budget never retries", func(t *testing.T) {
		mock := testutil.NewMock()
		defer mock.Close()
		calls := 0
		mock.ChatHandler = func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, waitingRoomBody)
		}
		client, err := New("tok-c", testConfig(mock.URL(), func(c *config.Config) {
			c.TransientRetries = 1
			c.WaitingRoomRetries = 0
		}))
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ChatCompletions(context.Background(), ChatOptions{Model: "m", RunID: "r"}, body)
		if !errors.Is(err, ErrWaitingRoom) {
			t.Fatalf("err = %v, want ErrWaitingRoom", err)
		}
		if calls != 1 {
			t.Errorf("upstream chat calls = %d, want 1 (WAITING_ROOM_RETRIES=0 disables)", calls)
		}
	})
}

// TestWaitingRoomBudgetIndependentOfTransientRetries pins the split: a
// capacity deferral and a waiting room are different queue classes, so one
// must never consume the other's allowance. Before the split they shared
// TRANSIENT_RETRIES, so a single deferral (or a single waiting-room hit)
// left the other class with no budget at all.
func TestWaitingRoomBudgetIndependentOfTransientRetries(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	deferred := `{"error":{"code":"free_mode_capacity_deferred","message":"at capacity"}}`

	t.Run("deferred then queued then 200", func(t *testing.T) {
		mock := testutil.NewMock()
		defer mock.Close()
		calls := 0
		mock.ChatHandler = func(w http.ResponseWriter, r *http.Request) {
			calls++
			switch calls {
			case 1:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, deferred)
			case 2:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, waitingRoomBody)
			default:
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				_, _ = io.WriteString(w, testutil.SSEEvent(`{"id":"x","object":"chat.completion.chunk","choices":[]}`))
			}
		}
		client, err := New("tok-e", testConfig(mock.URL(), func(c *config.Config) {
			c.TransientRetries = 1
			c.WaitingRoomRetries = 1
		}))
		if err != nil {
			t.Fatal(err)
		}
		fastWaitingRoomSeam(client, nil)

		rc, err := client.ChatCompletions(context.Background(), ChatOptions{Model: "m", RunID: "r"}, body)
		if err != nil {
			t.Fatalf("ChatCompletions: %v", err)
		}
		_ = rc.Close()
		if calls != 3 {
			t.Errorf("upstream chat calls = %d, want 3 (deferred retry + queued retry)", calls)
		}
		if got := client.CapacityDeferredRetries(); got != 1 {
			t.Errorf("CapacityDeferredRetries = %d, want 1", got)
		}
		if got := client.WaitingRoomRetries(); got != 1 {
			t.Errorf("WaitingRoomRetries = %d, want 1", got)
		}
	})

	t.Run("queued budget exhausted does not spend the deferred budget", func(t *testing.T) {
		mock := testutil.NewMock()
		defer mock.Close()
		calls := 0
		mock.ChatHandler = func(w http.ResponseWriter, r *http.Request) {
			calls++
			switch calls {
			case 1, 2:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, waitingRoomBody)
			default:
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				_, _ = io.WriteString(w, testutil.SSEEvent(`{"id":"x","object":"chat.completion.chunk","choices":[]}`))
			}
		}
		client, err := New("tok-f", testConfig(mock.URL(), func(c *config.Config) {
			c.TransientRetries = 0
			c.WaitingRoomRetries = 2
		}))
		if err != nil {
			t.Fatal(err)
		}
		fastWaitingRoomSeam(client, nil)

		rc, err := client.ChatCompletions(context.Background(), ChatOptions{Model: "m", RunID: "r"}, body)
		if err != nil {
			t.Fatalf("ChatCompletions: %v", err)
		}
		_ = rc.Close()
		if calls != 3 {
			t.Errorf("upstream chat calls = %d, want 3 (WAITING_ROOM_RETRIES=2 with TRANSIENT_RETRIES=0)", calls)
		}
		if got := client.WaitingRoomRetries(); got != 2 {
			t.Errorf("WaitingRoomRetries = %d, want 2", got)
		}
		if got := client.CapacityDeferredRetries(); got != 0 {
			t.Errorf("CapacityDeferredRetries = %d, want 0", got)
		}
	})
}

// TestWaitingRoomBackoffShape pins the vendor poll-backoff shape
// (cli/src/utils/polling-backoff.ts failedPollDelayMs) without sleeping:
// 20s doubling to a 5m cap, equal jitter over the LOWER half of the window,
// and Retry-After as a jittered-UP-only floor.
func TestWaitingRoomBackoffShape(t *testing.T) {
	window := func(attempt int) time.Duration {
		d := waitingRoomBackoffBase << min(attempt-1, 5)
		if d > waitingRoomBackoffMax {
			d = waitingRoomBackoffMax
		}
		return d
	}

	t.Run("window doubles and caps", func(t *testing.T) {
		for attempt := 1; attempt <= 8; attempt++ {
			w := window(attempt)
			for i := 0; i < 200; i++ {
				got := waitingRoomBackoff(attempt, 0)
				if got < w/2 || got > w {
					t.Fatalf("attempt %d: backoff = %v, want within [%v, %v] (equal jitter over the lower half)", attempt, got, w/2, w)
				}
			}
		}
		if window(6) != waitingRoomBackoffMax {
			t.Errorf("window(6) = %v, want the 5m cap", window(6))
		}
	})

	t.Run("Retry-After is a floor jittered up only", func(t *testing.T) {
		const ra = 30 * time.Second
		for i := 0; i < 200; i++ {
			got := waitingRoomBackoff(1, ra)
			if got < ra {
				t.Fatalf("backoff = %v, want >= the %v Retry-After floor (never re-hit early)", got, ra)
			}
			if got > ra+ra/5 {
				t.Fatalf("backoff = %v, want <= %v (Retry-After jitter is up-only, max 1.2x)", got, ra+ra/5)
			}
		}
	})

	t.Run("cap wins over an oversized Retry-After", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			if got := waitingRoomBackoff(1, 10*time.Minute); got != waitingRoomBackoffMax {
				t.Fatalf("backoff = %v, want the 5m cap", got)
			}
		}
	})

	t.Run("tiny Retry-After does not panic the modulo", func(t *testing.T) {
		if got := waitingRoomBackoff(1, time.Nanosecond); got < waitingRoomBackoffBase/2 {
			t.Errorf("backoff = %v, want the window floor when Retry-After is negligible", got)
		}
	})

	t.Run("attempt below one is treated as the first", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			got := waitingRoomBackoff(0, 0)
			if got < waitingRoomBackoffBase/2 || got > waitingRoomBackoffBase {
				t.Fatalf("backoff(0) = %v, want within the first window [%v, %v]", got, waitingRoomBackoffBase/2, waitingRoomBackoffBase)
			}
		}
	})
}

// TestQueueRetryAfterWindows pins the honor-window extraction: parsed
// Retry-After rides to the sleep, absent means the caller's default.
func TestQueueRetryAfterWindows(t *testing.T) {
	mk := func(status int, body string, retryAfter string) error {
		hdr := http.Header{}
		if retryAfter != "" {
			hdr.Set("Retry-After", retryAfter)
		}
		return classifyError(status, body, hdr)
	}
	if got := queueRetryAfter(mk(503, `{"error":{"message":"busy","code":503}}`, "")); got != 0 {
		t.Errorf("bare 503 window = %v, want 0 (caller floors to 10s)", got)
	}
	if got := queueRetryAfter(mk(503, `{"error":{"message":"busy","code":503}}`, "7")); got != 7*time.Second {
		t.Errorf("503 window = %v, want 7s", got)
	}
	def := mk(429, `{"error":{"code":"free_mode_capacity_deferred","message":"at capacity"}}`, "5")
	if got := queueRetryAfter(def); got != 5*time.Second {
		t.Errorf("deferred window = %v, want 5s", got)
	}
	if !isWaitingRoom(mk(503, `x`, "")) {
		t.Error("bare 503 must classify as waiting room")
	}
	if !isWaitingRoom(mk(429, `{"error":{"code":"waiting_room_queued"}}`, "")) {
		t.Error("429 queued must classify as waiting room")
	}
	if isWaitingRoom(mk(429, `{"error":{"code":"free_mode_capacity_deferred"}}`, "")) {
		t.Error("capacity-deferred must NOT classify as waiting room (own counter)")
	}
}
