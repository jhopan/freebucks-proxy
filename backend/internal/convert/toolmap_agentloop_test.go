package convert

import (
	"encoding/json"
	"testing"
)

// TestAgentLoopContinuationTurn pins the SECOND turn of an agent loop: the
// client sends back the assistant tool_calls it received (CLIENT names) plus
// the tool result, and the same tools[] array. Two properties matter:
//
//  1. the wire tools stay name-unique (dedupe still applies on every turn -
//     terminal/execute_code both map to run_terminal_command);
//  2. the historical tool_calls keep the CLIENT name in the request body.
//     That is deliberate: the names are only rewritten in tools[] (the offered
//     definitions upstream' foreign-toolset detector reads); the transcript is
//     forwarded verbatim so the client's own call_id/name correlation survives
//     and a strict upstream never sees a tool_call for a tool it was not
//     offered. Verified live against deepseek/deepseek-v4-flash (200).
func TestAgentLoopContinuationTurn(t *testing.T) {
	tools := []universalClientTool{
		{"terminal", map[string]any{"command": "string"}},
		{"execute_code", map[string]any{"code": "string"}},
		{"skills_list", nil},
		{"skill_view", map[string]any{"name": "string"}},
	}
	var toolsArr []any
	for _, ct := range tools {
		props := map[string]any{}
		for k, v := range ct.params {
			props[k] = map[string]any{"type": v}
		}
		toolsArr = append(toolsArr, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        ct.name,
				"description": "Tool " + ct.name,
				"parameters":  map[string]any{"type": "object", "properties": props},
			},
		})
	}

	body, err := json.Marshal(map[string]any{
		"model": "deepseek/deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "Run echo hi."},
			map[string]any{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []any{map[string]any{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":      "terminal",
						"arguments": `{"command":"echo hi"}`,
					},
				}},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "hi"},
		},
		"tools": toolsArr,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	norm, mapper, err := NormalizeRequestMapped(body, "")
	if err != nil {
		t.Fatalf("NormalizeRequestMapped: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(norm, &parsed); err != nil {
		t.Fatalf("unmarshal normalized: %v", err)
	}

	// 1. wire tools name-unique + no foreign signal, same contract as the
	// first turn.
	wireTools, ok := parsed["tools"].([]any)
	if !ok {
		t.Fatalf("wire tools not an array: %v", parsed["tools"])
	}
	v := ClassifyWireTools(wireTools)
	if sig := WireForeignSignal(v); sig != "" {
		t.Errorf("WireForeignSignal = %q, want empty (wire names %v)", sig, v.Names)
	}
	seen := map[string]bool{}
	for _, wt := range wireTools {
		fn, ok := wt.(map[string]any)["function"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "end_turn" || name == "decide" {
			continue
		}
		if seen[name] {
			t.Errorf("duplicate wire name %q on continuation turn (%v)", name, v.Names)
		}
		seen[name] = true
	}

	// 2. transcript forwarded verbatim: the client's tool_call name is intact.
	msgs, _ := parsed["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("wire messages = %d, want 3", len(msgs))
	}
	asst, _ := msgs[1].(map[string]any)
	tcs, _ := asst["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("assistant tool_calls = %v, want 1 entry", asst["tool_calls"])
	}
	fn, _ := tcs[0].(map[string]any)["function"].(map[string]any)
	if got, _ := fn["name"].(string); got != "terminal" {
		t.Errorf("historical tool_call name = %q, want terminal (client name preserved)", got)
	}
	toolMsg, _ := msgs[2].(map[string]any)
	if got, _ := toolMsg["tool_call_id"].(string); got != "call_1" {
		t.Errorf("tool_call_id = %q, want call_1", got)
	}

	// 3. response restore both shapes the model may now emit.
	if got := mapper.RestoreName("run_terminal_command"); got != "terminal" {
		t.Errorf("RestoreName(run_terminal_command) = %q, want terminal", got)
	}
	if got := mapper.RestoreName("mcp__execute_code"); got != "execute_code" {
		t.Errorf("RestoreName(mcp__execute_code) = %q, want execute_code", got)
	}
}
