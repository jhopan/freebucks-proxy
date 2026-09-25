package convert

import "testing"

// Issue #630 bisection record (mock-level, no live keys, no network).
//
// The report: POST /v1/chat/completions with tools[] on
// deepseek/deepseek-v4-flash answers upstream 404 "No endpoints found
// for deepseek/deepseek-v4-flash.", while the same body without tools
// succeeds. These tests pin what OUR wire emits for each shape and how
// the vendor gate (3420c99 foreign-client-signals.ts) reads it, so the
// trigger attribution stays evidence instead of lore:
//
//   - no tools: normalize STILL emits the first-party pin (glob + end_turn
//     + decide). It used to early-return on empty and leave the wire bare
//     ("the gate's tool leg never runs"), but the live server-side
//     tool-schema check keys on the wire carrying a GENUINE signature tool,
//     and a bare wire is the third_party_client shape — reproduced live
//     2026-09-25 (tool-less chat admitted, then banned on the third queued
//     retry). See normalizeToolSchemas.
//   - tools=[test_tool]: wire carries test_tool verbatim plus the
//     injected hollow end_turn. The tool leg reads foreign_toolset
//     (enforced) with the injection logged as hollow — but test_tool
//     ALONE already trips foreign_toolset, so the hollow injection is
//     not the differentiator for the gate verdict.
//   - mapped tools with subset schemas (pi powershell -> run_terminal_
//     command{command}) read GENUINE and clear the tool leg: the escape
//     hatch that keeps renamed harness traffic first-party.
//
// What this does NOT claim: the 404's layer. The enforced gate
// downgrades to the tiny model; the observed refusal names the
// requested model in OpenRouter routing phrasing ("No endpoints found
// for ...", same family as the max_price fence's failed_routing_step
// precedent in the registry), which points at the routing/capability
// layer rather than hollow/unrecognised-tool enforcement. That split
// needs a live dump and is recorded here so a live-keys follow-up can
// settle it; the distinct 404 surfacing lives in upstream/classify.go.

func wireToolsOf(t *testing.T, body map[string]any) []any {
	t.Helper()
	out, err := NormalizeRequest(mustJSON(t, body), "")
	if err != nil {
		t.Fatalf("NormalizeRequest: %v", err)
	}
	got := decode(t, out)
	raw, ok := got["tools"]
	if !ok {
		return nil
	}
	tools, ok := raw.([]any)
	if !ok {
		t.Fatalf("tools = %T, want array", raw)
	}
	return tools
}

func toolNamesOf(tools []any) []string {
	var names []string
	for _, tv := range tools {
		tm, ok := tv.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tm["function"].(map[string]any)
		if !ok {
			continue
		}
		if n, _ := fn["name"].(string); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// TestIssue630NoToolsWireCarriesSignaturePin pins the post-fix shape: a
// tool-less request must NOT go upstream bare. The injection used to
// early-return when the client offered no tools, so the wire carried no
// genuine signature member at all — which is the shape the live server-side
// tool-schema check reads as a third-party client (downgrade + sticky cap).
// Reproduced live 2026-09-25 against a real account.
func TestIssue630NoToolsWireCarriesSignaturePin(t *testing.T) {
	tools := wireToolsOf(t, map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "Say hello in one sentence."}},
	})
	names := toolNamesOf(tools)
	want := []string{"glob", "end_turn", "decide"}
	if len(names) != len(want) {
		t.Fatalf("no-tools request emitted %d wire tools %q, want %q", len(names), names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("wire tools = %q, want %q", names, want)
		}
	}
	// glob must read GENUINE (canonical name + canonical parameter key), so
	// the tool leg of the foreign-client check sees a first-party toolset.
	if s := WireForeignSignal(ClassifyWireTools(tools)); s != "" {
		t.Errorf("signal = %q, want clear (genuine signature member present)", s)
	}
}

func TestIssue630TestToolWireVerdict(t *testing.T) {
	tools := wireToolsOf(t, map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "Say hello in one sentence."}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "test_tool",
				"description": "A test tool",
				"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}},
	})
	names := toolNamesOf(tools)
	if len(names) != 4 || names[0] != "test_tool" || names[1] != "glob" || names[2] != "end_turn" || names[3] != "decide" {
		t.Fatalf("wire tools = %v, want [test_tool glob end_turn decide]", names)
	}
	v := ClassifyWireTools(tools)
	if len(v.Genuine) != 2 || v.Genuine[0] != "glob" || v.Genuine[1] != "decide" {
		t.Errorf("genuine = %v, want [glob decide]", v.Genuine)
	}
	if len(v.Hollow) != 0 {
		t.Errorf("hollow = %v, want empty", v.Hollow)
	}
	if len(v.Unrecognised) != 1 || v.Unrecognised[0] != "test_tool" {
		t.Errorf("unrecognised = %v, want [test_tool]", v.Unrecognised)
	}
	// Injected decide is a genuine signature tool, so the wire clears foreign_toolset.
	if s := WireForeignSignal(v); s != "" {
		t.Errorf("signal = %q, want empty (cleared)", s)
	}
	// The injection is not the differentiator: test_tool alone, with no
	// injected definition at all, trips the same enforced signal.
	bare := ClassifyWireTools([]any{map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "test_tool",
			"description": "A test tool",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}})
	if s := WireForeignSignal(bare); s != ForeignToolset {
		t.Errorf("bare test_tool signal = %q, want foreign_toolset", s)
	}
}

func TestIssue630MappedSubsetClearsToolLeg(t *testing.T) {
	// pi powershell renamed to run_terminal_command keeps its {command}
	// schema, which is a non-empty subset of the canonical keys: genuine
	// under the vendor rule, so the tool leg clears despite the hollow
	// injection riding alongside.
	out, _, err := NormalizeRequestMapped(mustJSON(t, map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "dir"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "powershell",
				"description": "run a command",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"command": map[string]any{"type": "string"}},
					"required":   []any{"command"},
				},
			},
		}},
	}), "")
	if err != nil {
		t.Fatalf("NormalizeRequestMapped: %v", err)
	}
	tools, _ := decode(t, out)["tools"].([]any)
	v := ClassifyWireTools(tools)
	if len(v.Genuine) != 3 || v.Genuine[0] != "run_terminal_command" || v.Genuine[1] != "glob" || v.Genuine[2] != "decide" {
		t.Errorf("genuine = %v, want [run_terminal_command decide]", v.Genuine)
	}
	if s := WireForeignSignal(v); s != "" {
		t.Errorf("signal = %q, want clear", s)
	}
}
