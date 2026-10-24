// Package openaichat contains protocol helpers for OpenAI-compatible chat
// requests and responses. Functions here are pure transforms: they never write
// HTTP responses and never call upstream services.
package openaichat

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// MaxToolNameLength is the upstream limit for a tool function name: OpenRouter's
// Meta provider caps names at 64 characters, and longer requests intermittently
// fail depending on the backend that handles them.
const MaxToolNameLength = 64

// CleanMessages returns the message slice unchanged in shape (non-object
// entries are preserved). It exists so request builders can normalize the
// container before further processing.
func CleanMessages(messages []any) []any {
	cleaned := make([]any, 0, len(messages))
	for _, m := range messages {
		cleaned = append(cleaned, m)
	}
	return cleaned
}

// ShortenToolName deterministically truncates an over-long tool name to at most
// MaxToolNameLength bytes: the first 55 bytes, an underscore, and 8 hex
// characters of the SHA-256 digest. Determinism keeps historical tool_call
// names stable across requests.
func ShortenToolName(orig string) string {
	sum := sha256.Sum256([]byte(orig))
	return fmt.Sprintf("%.55s_%x", orig, sum[:4])
}

// ClampToolNames shortens tool names longer than MaxToolNameLength in tool
// definitions, historical assistant tool_calls, and an explicit tool_choice.
// It returns the short-to-original mapping for restoring names on responses,
// or nil when nothing was shortened. Tool_call_id pairing is untouched.
func ClampToolNames(params map[string]any) map[string]string {
	shortToOrig := map[string]string{}
	clamped := false
	ensure := func(orig string) string {
		if len(orig) <= MaxToolNameLength {
			return orig
		}
		short := ShortenToolName(orig)
		shortToOrig[short] = orig
		clamped = true
		return short
	}
	renameToolCalls := func(tcs []any) {
		for _, tc := range tcs {
			tcm, ok := tc.(map[string]any)
			if !ok {
				continue
			}
			fn, ok := tcm["function"].(map[string]any)
			if !ok {
				continue
			}
			if name, _ := fn["name"].(string); name != "" {
				fn["name"] = ensure(name)
			}
		}
	}
	if tools, ok := params["tools"].([]any); ok {
		for _, t := range tools {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			fn, ok := tm["function"].(map[string]any)
			if !ok {
				continue
			}
			if name, _ := fn["name"].(string); name != "" {
				fn["name"] = ensure(name)
			}
		}
	}
	if msgs, ok := params["messages"].([]any); ok {
		for _, mv := range msgs {
			mm, ok := mv.(map[string]any)
			if !ok {
				continue
			}
			if tcs, ok := mm["tool_calls"].([]any); ok {
				renameToolCalls(tcs)
			}
		}
	}
	// tool_choice names are subject to the same limit; responses never echo
	// this field, so no restore mapping is needed for it.
	if tcChoice, ok := params["tool_choice"].(map[string]any); ok {
		if fn, ok := tcChoice["function"].(map[string]any); ok {
			if name, _ := fn["name"].(string); name != "" {
				fn["name"] = ensure(name)
			}
		}
	}
	if !clamped {
		return nil
	}
	return shortToOrig
}

// RestoreToolCalls replaces shortened tool names with their originals in both
// streaming delta and non-streaming message forms. A nil or empty map is a
// no-op.
func RestoreToolCalls(obj map[string]any, shortToOrig map[string]string) {
	if obj == nil || len(shortToOrig) == 0 {
		return
	}
	choices, ok := obj["choices"].([]any)
	if !ok {
		return
	}
	for _, c := range choices {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"delta", "message"} {
			d, ok := cm[key].(map[string]any)
			if !ok {
				continue
			}
			tcs, ok := d["tool_calls"].([]any)
			if !ok {
				continue
			}
			for _, tc := range tcs {
				tcm, ok := tc.(map[string]any)
				if !ok {
					continue
				}
				fn, ok := tcm["function"].(map[string]any)
				if !ok {
					continue
				}
				if name, _ := fn["name"].(string); name != "" {
					if orig, ok := shortToOrig[name]; ok {
						fn["name"] = orig
					}
				}
			}
		}
	}
}

// SanitizeMessages repairs malformed tool_calls in outbound history. Upstreams
// occasionally emit tool calls with an empty function.name (dropped stream
// fragments or tool calls leaked as text); clients replay the broken records on
// the next turn, which makes every later request fail with
// "tool_calls[N].function.name must be a non-empty string".
//
// It drops tool_calls with empty names, removes the field entirely when none
// remain, and drops orphaned tool results that no valid assistant tool_call
// references.
func SanitizeMessages(messages []any) []any {
	validToolIDs := make(map[string]bool)
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok || msg["role"] != "assistant" {
			continue
		}
		tcs, ok := msg["tool_calls"].([]any)
		if !ok {
			continue
		}
		for _, tc := range tcs {
			tcMap, ok := tc.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := tcMap["function"].(map[string]any)
			name, _ := fn["name"].(string)
			id, _ := tcMap["id"].(string)
			if name != "" && id != "" {
				validToolIDs[id] = true
			}
		}
	}

	cleaned := make([]any, 0, len(messages))
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			cleaned = append(cleaned, m)
			continue
		}
		role, _ := msg["role"].(string)

		if role == "tool" {
			id, _ := msg["tool_call_id"].(string)
			if !validToolIDs[id] {
				continue
			}
			cleaned = append(cleaned, msg)
			continue
		}

		if role == "assistant" {
			if tcs, ok := msg["tool_calls"].([]any); ok {
				kept := make([]any, 0, len(tcs))
				for _, tc := range tcs {
					tcMap, ok := tc.(map[string]any)
					if !ok {
						continue
					}
					fn, _ := tcMap["function"].(map[string]any)
					name, _ := fn["name"].(string)
					if name == "" {
						continue
					}
					kept = append(kept, tcMap)
				}
				if len(kept) == 0 {
					delete(msg, "tool_calls")
				} else {
					msg["tool_calls"] = kept
				}
			}
		}
		cleaned = append(cleaned, msg)
	}
	return cleaned
}

// SanitizeTools cleans optional tool fields in a request body before it is sent
// upstream. Converters used to copy tool fields unconditionally, so absent
// optional fields became "description": null; some gateways (Vercel/zen, the
// Cline free chain) reject null with 400 "Invalid input: expected string,
// received null".
//
// It handles both the nested {"function":{...}} shape and the flat zen
// Responses shape, keeps description only when it is a non-empty string, and
// removes any tool that has no name.
func SanitizeTools(body map[string]any) {
	items, ok := body["tools"].([]any)
	if !ok {
		return
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		tm, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		target := tm
		if fn, ok := tm["function"].(map[string]any); ok {
			target = fn
		}
		if name, _ := target["name"].(string); name == "" {
			continue
		}
		sanitizeToolFields(target)
		out = append(out, tm)
	}
	body["tools"] = out
}

func sanitizeToolFields(m map[string]any) {
	if s, ok := m["description"].(string); !ok || s == "" {
		delete(m, "description")
	}
	if m["parameters"] == nil {
		delete(m, "parameters")
	}
}

// GenToolUseID returns a fallback tool_use block ID for upstreams that omit one.
func GenToolUseID() string {
	return fmt.Sprintf("toolu_%x", time.Now().UnixNano())
}

// HasToolUseBlocks reports whether an Anthropic content block array contains a
// tool_use block.
func HasToolUseBlocks(content any) bool {
	blocks, ok := content.([]any)
	if !ok {
		return false
	}
	for _, b := range blocks {
		if bm, ok := b.(map[string]any); ok && bm["type"] == "tool_use" {
			return true
		}
	}
	return false
}

// NumericInt converts int and float64 JSON numbers to int.
func NumericInt(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case float64:
		return int(number), true
	default:
		return 0, false
	}
}
