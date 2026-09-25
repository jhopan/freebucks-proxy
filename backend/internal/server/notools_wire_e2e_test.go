package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"freebucks-proxy/backend/internal/testutil"
)

// TestToolLessChatWireCarriesFirstPartyPin is the end-to-end guard for the
// 2026-09-25 ban post-mortem.
//
// A client that offers NO tools (a plain OpenAI-compatible caller: no `tools`
// key at all, or `tools: []`) used to reach upstream with a BARE tools array,
// because the tool-schema injection early-returned on an empty incoming
// toolset. That bare wire is the third_party_client shape: the upstream
// free-mode gate classifies a request with no genuine signature member as
// third-party, downgrades it, and the trust system sticky-caps the account
// (convert/toolmap_request.go:14-19, "the trust system permanently caps any
// account seen sending a foreign tool schema").
//
// Reproduced live: a tool-less chat on a real account was admitted (session
// 200, agent-runs 200), queued on a 503 waiting room, and banned on the third
// retry — "Your account has been suspended for accessing Freebuff with a
// third-party client or proxy."
//
// This test drives the REAL handler against the mock upstream, so it asserts
// the bytes actually put on the wire rather than the converter's return value
// in isolation.
func TestToolLessChatWireCarriesFirstPartyPin(t *testing.T) {
	mock := testutil.NewMock()
	defer mock.Close()
	mock.ChatBody = chatSSE(modelA)
	srv, _ := newTestServer(t, nil, mock)

	resp, data := doJSON(t, http.MethodPost, srv.URL+"/v1/chat/completions",
		[]byte(`{"model":"`+modelA+`","messages":[{"role":"user","content":"ping"}],"stream":false}`), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, data)
	}

	wire := mock.LastChatBody()
	if wire == "" {
		t.Fatal("mock upstream recorded no chat body")
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(wire), &sent); err != nil {
		t.Fatalf("recorded wire body is not JSON: %v", err)
	}
	raw, ok := sent["tools"].([]any)
	if !ok {
		t.Fatalf("wire carries no tools array at all (bare wire = third_party_client shape): %s", wire)
	}
	names := make([]string, 0, len(raw))
	for _, item := range raw {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		if n, _ := fn["name"].(string); n != "" {
			names = append(names, n)
		}
	}
	if got := strings.Join(names, ","); got != "glob,end_turn,decide" {
		t.Fatalf("wire tools = %q, want glob,end_turn,decide (genuine signature pin + stripped pseudo-tools)", got)
	}
}
