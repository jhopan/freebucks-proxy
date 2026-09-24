package convert

// injectEndTurnTool appends the end_turn pseudo-tool and the genuine custom
// signature tool decide to pass Codebuff foreign_toolset validation.
// Existing tools are never duplicated. hasGenuine signals that the toolset
// already carries at least one genuine signature tool (a canonical name with
// a non-empty subset of its canonical parameter keys) — when true the glob
// injection is skipped. The injected glob is the foreign_toolset clearing
// member for toolsets (Hermes et al.) that offer no canonical tool: without
// one genuine member upstream downgrades the whole request, which surfaces
// to the client as 404 "No endpoints found for <model>" (issue #630/#729).
// Its description steers the model away from calling it.
func injectEndTurnTool(payload map[string]any, tools []any, hasEndTurn bool, hasDecide bool, hasGenuine bool) {
	raw, ok := payload["tools"].([]any)
	if !ok {
		raw = tools
	}
	if !hasGenuine && !hasName(raw, genuineSignatureInjectName) {
		raw = append(raw, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        genuineSignatureInjectName,
				"description": "Internal bookkeeping tool. Do not call.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"pattern": map[string]any{"type": "string"},
					},
					"required": []any{"pattern"},
				},
			},
		})
	}
	if !hasEndTurn {
		raw = append(raw, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "end_turn",
				"description": "Only use this tool to hand control back to the user.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		})
	}
	if !hasDecide {
		raw = append(raw, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "decide",
				"description": "Decide next step or action.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"choice": map[string]any{"type": "string"},
					},
				},
			},
		})
	}
	payload["tools"] = raw
}

// hasName reports whether a wire tools array already offers the function name.
func hasName(tools []any, name string) bool {
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		if n, _ := fn["name"].(string); n == name {
			return true
		}
	}
	return false
}
