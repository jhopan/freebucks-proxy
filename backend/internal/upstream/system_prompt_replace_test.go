package upstream

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"freebucks-proxy/backend/internal/config"
)

// TestPinnedSystemPromptShape pins the invariants of the embedded free-mode
// prompt. It is the drift guard for a vendor re-pin: the asset is copied by
// hand from the shipped client, so a refresh that silently drops a section, or
// that forgets to re-introduce the two fill points, must fail here rather than
// on the wire.
func TestPinnedSystemPromptShape(t *testing.T) {
	if systemPromptBase2Free == "" {
		t.Fatal("embedded system prompt is empty")
	}
	// The gate reads the opening sentence at byte 0; the asset must carry it
	// verbatim, trailing period included.
	if !strings.HasPrefix(systemPromptBase2Free, cliSystemMarkerPhrase) {
		t.Errorf("pinned prompt does not open with the canonical phrase %q: %.80q",
			cliSystemMarkerPhrase, systemPromptBase2Free)
	}
	// Both fill points must be present exactly once, or rendering is a no-op.
	for _, tok := range []string{systemPromptDateToken, systemPromptModelToken} {
		if n := strings.Count(systemPromptBase2Free, tok); n != 1 {
			t.Errorf("token %s appears %d time(s) in the pinned prompt, want exactly 1", tok, n)
		}
	}
	// The client-local tail is deliberately excluded: shipping those
	// placeholders literally would be a louder tell than omitting them.
	for _, tok := range []string{
		"{CODEBUFF_FILE_TREE_PROMPT_SMALL}",
		"{CODEBUFF_KNOWLEDGE_FILES_CONTENTS}",
		"{CODEBUFF_SYSTEM_INFO_PROMPT}",
		"{CODEBUFF_GIT_CHANGES_PROMPT}",
	} {
		if strings.Contains(systemPromptBase2Free, tok) {
			t.Errorf("pinned prompt must not carry the client-local placeholder %s", tok)
		}
	}
	// Section anchors, one per block of the shipped template.
	for _, anchor := range []string{
		"# General guidelines",
		"# Spawning agents guidelines",
		"# Freebuff Meta-information",
		"# Response examples",
	} {
		if !strings.Contains(systemPromptBase2Free, anchor) {
			t.Errorf("pinned prompt is missing section %q", anchor)
		}
	}
	// Free-mode branding, not the Codebuff arm (the template branches on
	// isFreebuff; the shipped free CLI carries the Freebuff variant).
	if !strings.Contains(systemPromptBase2Free, "See freebuff.com for more information about the product.") {
		t.Error("pinned prompt is not the free-mode arm (missing the freebuff.com meta line)")
	}
	if strings.Contains(systemPromptBase2Free, "Every prompt sent consumes the user's credits") {
		t.Error("pinned prompt carries the Codebuff (paid) meta-information block")
	}
}

// TestRenderBase2FreePromptFillsBothTokens proves rendering resolves every
// placeholder: an unresolved {CODEBUFF_*} left on the wire would be an
// immediate tell, since no shipped client ever sends one.
func TestRenderBase2FreePromptFillsBothTokens(t *testing.T) {
	got := renderBase2FreePrompt("z-ai/glm-5.2")
	if strings.Contains(got, "{CODEBUFF_") {
		t.Errorf("rendered prompt still carries a placeholder: %s",
			firstPlaceholder(got))
	}
	if !strings.Contains(got, "You are running on the z-ai/glm-5.2 model.") {
		t.Error("rendered prompt did not fill the model line from the request")
	}
	wantDate := time.Now().Format("January 2, 2006")
	if !strings.Contains(got, "Current date: "+wantDate+".") {
		t.Errorf("rendered prompt does not carry the en-US long date %q", wantDate)
	}
	if !hasCanonicalOpening(got) {
		t.Error("rendered prompt does not pass the byte-0 canonical-opening gate")
	}

	// An empty model falls back to the free-mode default rather than rendering
	// an empty line.
	fallback := renderBase2FreePrompt("   ")
	if !strings.Contains(fallback, "You are running on the "+defaultFreeModel+" model.") {
		t.Errorf("empty model did not fall back to %s", defaultFreeModel)
	}
}

// firstPlaceholder returns the first unresolved {CODEBUFF_*} token, for a
// readable failure message.
func firstPlaceholder(s string) string {
	i := strings.Index(s, "{CODEBUFF_")
	if i < 0 {
		return ""
	}
	if j := strings.IndexByte(s[i:], '}'); j >= 0 {
		return s[i : i+j+1]
	}
	return s[i:]
}

// TestEnsureSystemPromptReplace covers the destructive arm: the run's system
// messages are dropped wholesale and the pinned prompt is installed as the
// single system message at index 0, in the shape the shipped client sends.
func TestEnsureSystemPromptReplace(t *testing.T) {
	foreign := "You are Claude Code, Anthropic's official CLI for Claude.\ncc_version=1.2.3"
	p := map[string]any{
		"model": "deepseek/deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "system", "content": foreign},
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "content": "hello"},
			map[string]any{"role": "system", "content": "second system block"},
			map[string]any{"role": "developer", "content": "developer block"},
		},
	}
	ensureSystemPrompt(p, "base2-free", config.SystemPromptModeReplace)

	msgs, ok := p["messages"].([]any)
	if !ok {
		t.Fatalf("messages = %T, want []any", p["messages"])
	}
	// One pinned system message + the two non-system messages. The input's
	// second system block and its developer block are dropped too: the shipped
	// client sends exactly one system message, so "replace" must not leave a
	// second one behind to contradict the pinned identity.
	if len(msgs) != 3 {
		t.Fatalf("messages length = %d, want 3 (system + user + assistant)", len(msgs))
	}
	sys := msgs[0].(map[string]any)
	if sys["role"] != "system" {
		t.Fatalf("messages[0] role = %v, want system", sys["role"])
	}
	content, ok := sys["content"].(string)
	if !ok {
		t.Fatalf("system content = %T, want a plain string (the shape the wire carries)", sys["content"])
	}
	if !hasCanonicalOpening(content) {
		t.Error("installed system prompt does not open with a canonical identity")
	}
	if !strings.HasPrefix(content, cliSystemMarker) {
		t.Errorf("installed prompt is not the pinned free-mode prompt: %.90q", content)
	}
	// The caller's own instructions must not survive anywhere.
	for i, m := range msgs {
		raw, _ := json.Marshal(m)
		if strings.Contains(string(raw), "Claude Code") || strings.Contains(string(raw), "second system block") {
			t.Errorf("messages[%d] still carries the caller's system prompt: %s", i, raw)
		}
	}
	// Order of the surviving non-system messages is preserved.
	wantRoles := []string{"system", "user", "assistant"}
	for i, want := range wantRoles {
		if got := msgs[i].(map[string]any)["role"]; got != want {
			t.Errorf("messages[%d] role = %v, want %v", i, got, want)
		}
	}
}

// TestEnsureSystemPromptReplaceEmptyAndOpaqueMessages covers the degenerate
// inputs: no messages key at all, and a messages value that is not a list.
func TestEnsureSystemPromptReplaceEmptyAndOpaqueMessages(t *testing.T) {
	for name, payload := range map[string]map[string]any{
		"missing":    {},
		"not-a-list": {"messages": "nope"},
	} {
		t.Run(name, func(t *testing.T) {
			ensureSystemPrompt(payload, "base2-free", config.SystemPromptModeReplace)
			msgs, ok := payload["messages"].([]any)
			if !ok || len(msgs) != 1 {
				t.Fatalf("messages = %v, want a single system message", payload["messages"])
			}
			sys := msgs[0].(map[string]any)
			if sys["role"] != "system" {
				t.Errorf("role = %v, want system", sys["role"])
			}
			content, _ := sys["content"].(string)
			if !strings.HasPrefix(content, cliSystemMarker) {
				t.Errorf("content = %.60q, want the pinned prompt", content)
			}
		})
	}
}

// TestEnsureSystemPromptMarkerArmKeepsCallerPrompt proves the revert path:
// mode "marker" (and the zero value) must behave exactly as before — prepend
// the opening, keep the caller's instructions.
func TestEnsureSystemPromptMarkerArmKeepsCallerPrompt(t *testing.T) {
	for _, mode := range []string{"", config.SystemPromptModeMarker} {
		p := map[string]any{"messages": []any{
			map[string]any{"role": "system", "content": "Custom persona. Be terse."},
			map[string]any{"role": "user", "content": "hi"},
		}}
		ensureSystemPrompt(p, "base2-free", mode)
		msgs := p["messages"].([]any)
		if len(msgs) != 2 {
			t.Fatalf("mode %q: messages = %v, want unchanged length", mode, msgs)
		}
		content := msgs[0].(map[string]any)["content"].(string)
		if !strings.HasPrefix(content, cliSystemMarker) {
			t.Errorf("mode %q: marker not prepended: %.60q", mode, content)
		}
		if !strings.Contains(content, "Custom persona. Be terse.") {
			t.Errorf("mode %q: caller prompt was dropped: %.120q", mode, content)
		}
	}
}

// TestInjectEnvelopeSystemPromptModes drives the real entry point so the mode
// is proven to reach the wire through ChatOptions, not just through the
// helper: replace swaps the system prompt, marker prepends to it.
func TestInjectEnvelopeSystemPromptModes(t *testing.T) {
	body := `{"model":"deepseek/deepseek-v4-flash","messages":[{"role":"system","content":"You are Claude Code."},{"role":"user","content":"hi"}]}`
	// The marker arm keeps the caller's own prompt but still scrubs foreign
	// harness markers, so it needs a benign prompt to prove preservation.
	benign := `{"model":"deepseek/deepseek-v4-flash","messages":[{"role":"system","content":"Custom persona. Be terse."},{"role":"user","content":"hi"}]}`

	out, err := injectEnvelope([]byte(body), "free", ChatOptions{
		RunID:            "run-1",
		SystemPromptMode: config.SystemPromptModeReplace,
	})
	if err != nil {
		t.Fatalf("injectEnvelope (replace): %v", err)
	}
	var replaced struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &replaced); err != nil {
		t.Fatalf("unmarshal (replace): %v", err)
	}
	if len(replaced.Messages) != 2 {
		t.Fatalf("replace: messages = %d, want 2", len(replaced.Messages))
	}
	text, _ := replaced.Messages[0].Content.(string)
	if !strings.HasPrefix(text, cliSystemMarker) {
		t.Errorf("replace: system prompt is not the pinned one: %.90q", text)
	}
	if strings.Contains(text, "Claude Code") {
		t.Error("replace: the caller's system prompt survived")
	}
	if !strings.Contains(text, "You are running on the deepseek/deepseek-v4-flash model.") {
		t.Error("replace: the model line was not filled from the request")
	}

	out, err = injectEnvelope([]byte(benign), "free", ChatOptions{
		RunID:            "run-1",
		SystemPromptMode: config.SystemPromptModeMarker,
	})
	if err != nil {
		t.Fatalf("injectEnvelope (marker): %v", err)
	}
	var marked struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &marked); err != nil {
		t.Fatalf("unmarshal (marker): %v", err)
	}
	if len(marked.Messages) != 2 {
		t.Fatalf("marker: messages = %d, want 2", len(marked.Messages))
	}
	markerText, _ := marked.Messages[0].Content.(string)
	if !strings.HasPrefix(markerText, cliSystemMarker) {
		t.Errorf("marker: canonical opening not prepended: %.60q", markerText)
	}
	if !strings.Contains(markerText, "Custom persona. Be terse.") {
		t.Errorf("marker: the caller's prompt was dropped: %.120q", markerText)
	}
	if strings.Contains(markerText, "# Response examples") {
		t.Error("marker: the pinned prompt was installed, want prepend-only")
	}
}
