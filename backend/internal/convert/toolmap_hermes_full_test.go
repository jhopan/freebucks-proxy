package convert

import "testing"

// Pin the REAL Hermes toolset (32 core tools, request order as Hermes sends
// it) through the ingress->classify->egress simulation. Complements the
// "Hermes" (8-tool) and "Hermes-extended" (6-tool) rows in
// toolmap_universal_test.go / toolmap_test.go: those sample the toolset,
// this one carries every core tool in one body - the shape that tripped
// "Tool names must be unique" (502) on DeepSeek before #655, since
// terminal+execute_code both map to run_terminal_command and
// skills_list/skill_view/skill_manage all map to skill.
//
// Wire contract under test:
//   - wire tools[] name-unique (strict upstreams reject duplicates)
//   - zero enforced foreign signals (blacklisted harness names like
//     delegate_task/computer_use and cron* virtualize to mcp__<name>)
//   - at least one genuine signature tool on the wire
//   - every wire name restores to the client's original downstream

func TestHermesFullToolsetWireClean(t *testing.T) {
	tools := []universalClientTool{
		// browser toolset (10)
		{"browser_back", nil},
		{"browser_click", map[string]any{"ref": "string"}},
		{"browser_console", map[string]any{"expression": "string"}},
		{"browser_get_images", nil},
		{"browser_navigate", map[string]any{"url": "string"}},
		{"browser_press", map[string]any{"key": "string"}},
		{"browser_scroll", map[string]any{"direction": "string"}},
		{"browser_snapshot", nil},
		{"browser_type", map[string]any{"ref": "string", "text": "string"}},
		{"browser_vision", map[string]any{"question": "string"}},
		// orchestration
		{"clarify", map[string]any{"questions": "array"}},
		{"cronjob", map[string]any{"action": "string", "schedule": "string", "prompt": "string"}},
		{"delegate_task", map[string]any{"tasks": "array"}},
		{"execute_code", map[string]any{"code": "string"}},
		// media
		{"image_generate", map[string]any{"prompt": "string"}},
		{"text_to_speech", map[string]any{"text": "string"}},
		{"vision_analyze", map[string]any{"image_url": "string", "question": "string"}},
		// memory
		{"memory", map[string]any{"action": "string", "content": "string"}},
		// files
		{"patch", map[string]any{"path": "string", "old_string": "string", "new_string": "string"}},
		{"read_file", map[string]any{"path": "string"}},
		{"search_files", map[string]any{"pattern": "string"}},
		{"write_file", map[string]any{"path": "string", "content": "string"}},
		// processes
		{"process", map[string]any{"action": "string", "session_id": "string"}},
		{"terminal", map[string]any{"command": "string"}},
		// sessions & skills
		{"session_search", map[string]any{"query": "string"}},
		{"skills_list", nil},
		{"skill_view", map[string]any{"name": "string"}},
		{"skill_manage", map[string]any{"operations": "array"}},
		{"todo", map[string]any{"todos": "array"}},
		// web
		{"web_extract", map[string]any{"urls": "array"}},
		{"web_search", map[string]any{"query": "string"}},
		// desktop (cua)
		{"computer_use", map[string]any{"action": "string"}},
	}
	assertHarnessWireClean(t, "Hermes-full", tools)
}
