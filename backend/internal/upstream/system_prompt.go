package upstream

// system_prompt.go — the pinned free-mode system prompt and its two fill
// points.
//
// SYSTEM_PROMPT_MODE=replace installs this prompt verbatim as the run's single
// system message, discarding whatever the caller sent. It is the only way to
// make the wire carry the prompt the shipped client actually sends: the
// free-mode gate is a byte-exact prefix test on ONE of five canonical openings
// (FREEBUFF_ROOT_SYSTEM_PROMPT_OPENINGS, see cliSystemGateOpenings in chat.go),
// and everything AFTER that opening — the general guidelines, the spawn_agents
// contract, the worked examples — is client-local text no gateway can
// reconstruct from an inbound request.
//
// "marker" is the revert path: prepend the canonical opening sentence and leave
// the caller's own system prompt in place. See ensureSystemPrompt in chat.go.

import (
	_ "embed"
	"strings"
	"time"
)

// systemPromptBase2Free is the free-mode base2 root prompt (static prefix
// only), pinned byte-for-byte from the shipped client.
//
// Provenance: createBase2('free') — agents/base2/base2.ts:258-379 at vendor
// 0065263 — with the free-mode arms selected (isFreebuff && isLean, i.e. the
// free/lite shape) and the default free model. Verified against the installed
// CLI (~/.config/manicode/freebuff.exe, 0.0.196) at byte offset 101839445,
// length 6686: identical after unescaping the 14 backslash-backticks the JS
// template literal needs and normalizing the two build-time fill points below.
// Pinned by TestPinnedSystemPromptMatchesShippedShape.
//
// What is deliberately NOT pinned: the template's tail, which the client fills
// with machine-local data the gateway never sees — the file-tree prompt, the
// knowledge-files contents, the system-info prompt and the "# Initial Git
// Changes" block (PLACEHOLDER.FILE_TREE_PROMPT_SMALL / KNOWLEDGE_FILES_CONTENTS
// / SYSTEM_INFO_PROMPT / GIT_CHANGES_PROMPT, rendered into the binary as
// {CODEBUFF_*} tokens). Sending those placeholders literally would be a louder
// tell than omitting them, so the pinned text stops at the last example block.
//
// Re-verify on every vendor re-pin (scripts/check-upstream.sh).
//
//go:embed system_prompt_base2_free.txt
var systemPromptBase2Free string

// The two fill points the shipped client resolves at run time.
const (
	// systemPromptDateToken is PLACEHOLDER.CURRENT_DATE. The client renders it
	// through formatCurrentDate → Intl.DateTimeFormat('en-US', {year:
	// 'numeric', month: 'long', day: 'numeric'}) → "September 27, 2026".
	systemPromptDateToken = "{CODEBUFF_CURRENT_DATE}"
	// systemPromptModelToken stands in for the model name the client bakes into
	// its own prompt at build time ("You are running on the X model."). The
	// shipped CLI carries one pre-rendered variant per agent file, so the name
	// is a build-time constant there; the gateway fills it from the request.
	systemPromptModelToken = "{CODEBUFF_MODEL}"
)

// defaultFreeModel is the model the shipped free-mode base2 agent pins when its
// caller gives no override (base2-free.ts: createBase2('free') with no model,
// i.e. MODEL_BY_MODE.free). Used only when the request carries no model string.
const defaultFreeModel = "deepseek/deepseek-v4-flash"

// renderBase2FreePrompt returns the pinned prompt with both fill points
// resolved. model is the request's resolved model id — what the shipped client
// would have baked into its own variant; empty falls back to defaultFreeModel.
//
// The date is the GATEWAY's local date. The client uses its own machine's clock
// and a gateway cannot see it, so the two can differ by a day across
// timezones. Cosmetic only: the gate reads the opening sentence, never the date.
func renderBase2FreePrompt(model string) string {
	if model = strings.TrimSpace(model); model == "" {
		model = defaultFreeModel
	}
	out := strings.ReplaceAll(systemPromptBase2Free, systemPromptModelToken, model)
	return strings.ReplaceAll(out, systemPromptDateToken, promptDate(time.Now()))
}

// promptDate renders t the way the client's CURRENT_DATE placeholder does
// (en-US long date, day without a leading zero).
func promptDate(t time.Time) string {
	return t.Format("January 2, 2006")
}
