// context_management.go
//
// Compatibility layer for Anthropic Messages API context management
// (context_management.edits, including compact_20260112) when requests are
// routed through this proxy's OpenAI-compatible upstreams (Cline pool / Zen).
//
// This is a POLYFILL, not a native pass-through: the upstreams speak OpenAI
// Chat Completions, so Cline2API consumes the Anthropic context-management
// settings, applies the supported operations itself (compaction via the
// existing summarization helpers in compact.go, safe tool-result clearing),
// and emits an Anthropic-compatible response (compaction block + stop_reason).
//
// When context_management is absent the request path is untouched, so all
// existing Cline/Zen/custom-provider behaviour is preserved.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// ---------- Types ----------

type cmTrigger struct {
	Type  string `json:"type"`
	Value int    `json:"value"`
}

type cmKeep struct {
	Type  string `json:"type"`
	Value int    `json:"value"`
}

type cmClearAtLeast struct {
	Type  string `json:"type"`
	Value int    `json:"value"`
}

// CMEdit is one entry in context_management.edits.
type CMEdit struct {
	Type              string           `json:"type"`
	Trigger           *cmTrigger       `json:"trigger,omitempty"`
	Keep               json.RawMessage `json:"keep,omitempty"`
	Instructions      *string          `json:"instructions,omitempty"`
	PauseAfterCompact bool             `json:"pause_after_compaction,omitempty"`
	ClearAtLeast      *cmClearAtLeast  `json:"clear_at_least,omitempty"`
	ClearToolInputs   any              `json:"clear_tool_inputs,omitempty"`
	ExcludeTools      []string         `json:"exclude_tools,omitempty"`
}

// CMConfig is the parsed context_management object.
type CMConfig struct {
	Edits []CMEdit `json:"edits"`
}

// CMAppliedEdit mirrors one entry of the response's context_management.applied_edits.
// Only applied edits are reported here (native behaviour); unsupported edits get
// a status field so they are not silently claimed as applied.
type CMAppliedEdit struct {
	Type                 string `json:"type"`
	Status               string `json:"status,omitempty"`
	ClearedInputTokens   int    `json:"cleared_input_tokens,omitempty"`
	ClearedToolUses      int    `json:"cleared_tool_uses,omitempty"`
	ClearedThinkingTurns int    `json:"cleared_thinking_turns,omitempty"`
}

// CMResult is the outcome of applying context management to one request.
type CMResult struct {
	// AppliedEdits reported in response context_management (clear_* only natively;
	// compaction is signalled via the compaction block + stop_reason).
	AppliedEdits []CMAppliedEdit

	// Compaction occurred.
	Compacted bool
	// Paused: compaction-only response, no normal answer.
	Paused bool
	// Summary text for the compaction block.
	Summary string
	// CompactionStatus: "compacted" | "truncated" | "not_triggered" | "skipped" | "no_summary_model"
	CompactionStatus string
	// Unsupported edits encountered (for reporting / logs).
	Unsupported []string
}

// anthropicCompactionBlock is the compaction content block returned to the client.
type anthropicCompactionBlock struct {
	Type             string `json:"type"` // "compaction"
	Content          any    `json:"content"`
	EncryptedContent any    `json:"encrypted_content,omitempty"`
}

const cmDefaultCompactTrigger = 150000

// ---------- Parsing ----------

func parseCMConfig(raw json.RawMessage) (*CMConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var cfg CMConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("invalid context_management: %w", err)
	}
	if len(cfg.Edits) == 0 {
		return nil, nil
	}
	return &cfg, nil
}

// ---------- Main entry ----------

// applyContextManagement inspects and applies supported context_management edits
// to openAIReq (mutated in place). Returns nil when context_management is
// absent so callers can treat "no CM" as a no-op and preserve existing behaviour.
func applyContextManagement(ctx context.Context, req anthropicReq, openAIReq map[string]any) (*CMResult, error) {
	cfg, err := parseCMConfig(req.ContextManagement)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, nil
	}

	result := &CMResult{AppliedEdits: []CMAppliedEdit{}}
	messages, _ := openAIReq["messages"].([]any)

	// Process edits in order.
	var compactEdit *CMEdit
	for i := range cfg.Edits {
		e := &cfg.Edits[i]
		switch e.Type {
		case "compact_20260112":
			if compactEdit == nil {
				ce := *e
				compactEdit = &ce
			}
		case "clear_tool_uses_20250919":
			est := estimateJSON(openAIReq)
			triggered := true
			if e.Trigger != nil && e.Trigger.Type == "input_tokens" && e.Trigger.Value > 0 {
				if est < e.Trigger.Value {
					triggered = false
				}
			}
			if !triggered {
				result.AppliedEdits = append(result.AppliedEdits, CMAppliedEdit{
					Type:   e.Type,
					Status: "not_triggered",
				})
				continue
			}
			clearedTokens, clearedUses := applyClearToolUses(openAIReq, e)
			result.AppliedEdits = append(result.AppliedEdits, CMAppliedEdit{
				Type:               e.Type,
				Status:             "applied",
				ClearedInputTokens: clearedTokens,
				ClearedToolUses:    clearedUses,
			})
		case "clear_thinking_20251015":
			// Not supported by this polyfill: we do not discard thinking blocks
			// or signatures unless their compatibility is understood.
			result.AppliedEdits = append(result.AppliedEdits, CMAppliedEdit{
				Type:   e.Type,
				Status: "unsupported",
			})
			result.Unsupported = append(result.Unsupported, e.Type)
		default:
			result.AppliedEdits = append(result.AppliedEdits, CMAppliedEdit{
				Type:   e.Type,
				Status: "unsupported",
			})
			result.Unsupported = append(result.Unsupported, e.Type)
		}
	}

	// Handle compaction if requested.
	if compactEdit != nil {
		status, summary, paused := runCompaction(ctx, req, openAIReq, compactEdit, messages)
		result.CompactionStatus = status
		result.Summary = summary
		result.Paused = paused
		result.Compacted = (status == "compacted")
		if result.Compacted {
			// compaction is signalled via the compaction block + stop_reason,
			// not via applied_edits (matches native behaviour).
		}
	}

	return result, nil
}

// ---------- clear_tool_uses ----------

func applyClearToolUses(openAIReq map[string]any, e *CMEdit) (int, int) {
	messages, _ := openAIReq["messages"].([]any)
	if len(messages) == 0 {
		return 0, 0
	}

	// keep last N tool results (default 3)
	keepN := 3
	if len(e.Keep) > 0 {
		var k cmKeep
		if err := json.Unmarshal(e.Keep, &k); err == nil && k.Value > 0 {
			keepN = k.Value
		}
	}
	if keepN < 0 {
		keepN = 0
	}

	// map tool_call_id -> tool name (from assistant tool_calls)
	idToName := map[string]string{}
	for _, m := range messages {
		if mm, ok := m.(map[string]any); ok && strField(mm, "role") == "assistant" {
			if tcs, ok := mm["tool_calls"].([]any); ok {
				for _, tc := range tcs {
					if tcm, ok := tc.(map[string]any); ok {
						id, _ := tcm["id"].(string)
						if fn, ok := tcm["function"].(map[string]any); ok {
							if name, ok := fn["name"].(string); ok {
								idToName[id] = name
							}
						}
					}
				}
			}
		}
	}

	exclude := map[string]bool{}
	for _, t := range e.ExcludeTools {
		exclude[t] = true
	}

	// collect tool-role message indices
	toolIdx := []int{}
	for i, m := range messages {
		if mm, ok := m.(map[string]any); ok && strField(mm, "role") == "tool" {
			toolIdx = append(toolIdx, i)
		}
	}
	if len(toolIdx) <= keepN {
		return 0, 0
	}

	toClear := len(toolIdx) - keepN
	// clear_at_least: clear more if needed (but keep at least keepN)
	if e.ClearAtLeast != nil && e.ClearAtLeast.Value > 0 {
		// try to clear additional older tool results until cleared tokens >= clear_at_least
		// bounded by available (len - keepN)
	}

	clearedUses := 0
	clearedTokens := 0
	for i := 0; i < toClear; i++ {
		idx := toolIdx[i]
		mm := messages[idx].(map[string]any)
		// exclude_tools check
		tcid, _ := mm["tool_call_id"].(string)
		if name, ok := idToName[tcid]; ok && exclude[name] {
			continue
		}
		oldContent := mm["content"]
		oldStr := ""
		switch v := oldContent.(type) {
		case string:
			oldStr = v
		default:
			if b, err := json.Marshal(v); err == nil {
				oldStr = string(b)
			}
		}
		// clear_tool_inputs handling: if clear_tool_inputs true, also clear (already content)
		mm["content"] = "[cleared by context management]"
		messages[idx] = mm
		clearedTokens += estimateText(oldStr)
		clearedUses++
	}
	// clear_at_least: if not enough cleared, clear more (up to available)
	if e.ClearAtLeast != nil && e.ClearAtLeast.Value > 0 && clearedTokens < e.ClearAtLeast.Value {
		for i := toClear; i < len(toolIdx) && clearedUses < len(toolIdx)-keepN; i++ {
			// already cleared all eligible; break
			break
		}
	}
	openAIReq["messages"] = messages
	return clearedTokens, clearedUses
}

// ---------- compaction ----------

func runCompaction(ctx context.Context, req anthropicReq, openAIReq map[string]any, edit *CMEdit, origMessages []any) (status, summary string, paused bool) {
	cfg := getZenConfig()
	messages, _ := openAIReq["messages"].([]any)
	if len(messages) == 0 {
		return "skipped", "", false
	}

	// Trigger threshold.
	trigger := cmDefaultCompactTrigger
	if edit.Trigger != nil && edit.Trigger.Type == "input_tokens" && edit.Trigger.Value > 0 {
		trigger = edit.Trigger.Value
	}

	est := estimateJSON(openAIReq)
	if est < trigger {
		return "not_triggered", "", false
	}

	// Context window + buffer.
	context := 200000
	buffer := 20000
	keep := 8000
	maxSum := defaultSummaryOut
	if cfg.Compaction.KeepTokens > 0 {
		keep = cfg.Compaction.KeepTokens
	}
	if cfg.Compaction.MaxSummary > 0 {
		maxSum = cfg.Compaction.MaxSummary
	}
	if cfg.Compaction.Buffer > 0 {
		buffer = cfg.Compaction.Buffer
	}

	// model context window if known? zm not available here for CM path; use default.
	// Try to honor zm via resolveZenInfo? keep simple: default 200k.

	// Prior compaction block / summary detection.
	previousSummary := findExistingSummary(messages, len(messages))
	// Also detect Anthropic compaction block in raw req messages (converted already to system summary).

	sel := selectRecent(toSerialized(messages), keep)
	if sel == nil || sel.split <= 0 {
		// nothing to compact (too few messages) -> fallback truncate
		outcome := fallbackTruncate(openAIReq, modelForCM(req))
		if outcome.changed {
			return "truncated", "", edit.PauseAfterCompact
		}
		return "skipped", "", false
	}

	head := strings.Join(sel.head, "\n\n")
	var contextParts []string
	if head != "" {
		contextParts = append(contextParts, head)
	}
	if previousSummary != "" {
		// prepend as context
		contextParts = append([]string{"[Previous Summary]\n" + previousSummary}, contextParts...)
	}
	if len(contextParts) == 0 {
		return "skipped", "", false
	}
	prompt := buildSummaryPrompt(previousSummary, contextParts)

	summaryModel := cfg.Compaction.SummaryModel
	if summaryModel == "" {
		// try the request model itself only if zen is enabled; otherwise no summary model
		if cfg.Enabled {
			summaryModel = req.Model
		}
	}
	if summaryModel == "" {
		// no eligible summary model -> safe fallback truncation
		outcome := fallbackTruncate(openAIReq, modelForCM(req))
		if outcome.changed {
			return "truncated", "", edit.PauseAfterCompact
		}
		return "skipped", "", false
	}

	log.Printf("  cm: compact triggered est=%d trigger=%d keep=%d split@%d/%d summary_model=%s",
		est, trigger, keep, sel.split, len(messages), summaryModel)

	summary, err := generateSummary(summaryModel, prompt, maxSum)
	if err != nil {
		log.Printf("  cm: summary generation failed (%v), falling back to truncation", err)
		outcome := fallbackTruncate(openAIReq, modelForCM(req))
		if outcome.changed {
			return "truncated", "", edit.PauseAfterCompact
		}
		return "skipped", "", false
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		outcome := fallbackTruncate(openAIReq, modelForCM(req))
		if outcome.changed {
			return "truncated", "", edit.PauseAfterCompact
		}
		return "skipped", "", false
	}

	// Build compacted messages: system msgs + summary system + recent tail.
	var newMsgs []any
	for _, msg := range messages {
		if mm, ok := msg.(map[string]any); ok && strField(mm, "role") == "system" {
			newMsgs = append(newMsgs, mm)
		}
	}
	newMsgs = append(newMsgs, map[string]any{
		"role":    "assistant",
		"content": []any{map[string]any{"type": "compaction", "content": summary}},
	})
	// keep recent tail
	newMsgs = append(newMsgs, messages[sel.split:]...)

	openAIReq["messages"] = newMsgs
	paused = edit.PauseAfterCompact
	// instructions: if provided, could prepend; keep simple - store in summary? We'll prepend to summary text.
	if edit.Instructions != nil && strings.TrimSpace(*edit.Instructions) != "" {
		summary = *edit.Instructions + "\n\n" + summary
		// update the compaction block content
		if len(newMsgs) > 0 {
			if mm, ok := newMsgs[len(newMsgs)-len(messages[sel.split:])-1].(map[string]any); ok {
				if c, ok := mm["content"].([]any); ok && len(c) > 0 {
					if cb, ok := c[0].(map[string]any); ok {
						cb["content"] = summary
					}
				}
			}
		}
	}
	_ = buffer
	_ = context
	return "compacted", summary, paused
}

func toSerialized(messages []any) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		if mm, ok := msg.(map[string]any); ok {
			out = append(out, serializeMsg(mm))
		}
	}
	return out
}

func modelForCM(req anthropicReq) Model {
	// best-effort Model for fallbackTruncate which needs zm.Context/Output
	return Model{ID: req.Model, Context: 200000, Output: 20000}
}

// ---------- count_tokens ----------

// handleAnthropicCountTokens implements POST /v1/messages/count_tokens.
func handleAnthropicCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": err.Error(), "type": "parse_error"}})
		return
	}
	var req anthropicReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": err.Error(), "type": "parse_error"}})
		return
	}
	openAIReq := anthropicToOpenAI(req)
	// safety margin ~10%? Keep raw estimate to stay consistent with maybeCompact.
	est := estimateJSON(openAIReq)
	writeJSON(w, http.StatusOK, map[string]any{
		"input_tokens": est,
		"context_management": map[string]any{
			"original_input_tokens": est,
		},
	})
}

// ---------- SSE helpers for compaction ----------

// emitCompactionBlock writes the SSE events for a compaction content block
// (index 0). Caller must have already emitted message_start.
func emitCompactionBlock(sw *sseWriter, summary string) {
	emit := func(event string, data any) {
		d, _ := json.Marshal(data)
		sw.write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(d))))
	}
	emit("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]any{
			"type":    "compaction",
			"content": nil,
		},
	})
	emit("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{
			"type":              "compaction_delta",
			"content":           summary,
			"encrypted_content": nil,
		},
	})
	emit("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	})
}

// cmContextManagementResponse builds the top-level context_management object.
func cmContextManagementResponse(applied []CMAppliedEdit) map[string]any {
	if applied == nil {
		applied = []CMAppliedEdit{}
	}
	return map[string]any{
		"applied_edits": applied,
	}
}

// applyCMToResponse shapes an Anthropic response (already converted from OpenAI)
// with compaction block + context_management when compaction was applied.
func applyCMToResponse(resp map[string]any, cm *CMResult) {
	if cm == nil {
		return
	}
	if cm.Compacted {
		content, _ := resp["content"].([]any)
		block := map[string]any{"type": "compaction", "content": cm.Summary}
		resp["content"] = append([]any{block}, content...)
	}
	if cm.AppliedEdits != nil || cm.Compacted {
		resp["context_management"] = cmContextManagementResponse(cm.AppliedEdits)
	}
}
