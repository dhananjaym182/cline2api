// Package anthropic converts between the Anthropic Messages API and the
// OpenAI chat representation used by upstreams. It performs pure data
// transformation and never writes HTTP responses.
package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cline-go-proxy/internal/protocol"
)

// Message is one Anthropic message with an already-decoded content value.
type Message struct {
	Role    string
	Content any
}

// Request is the subset of the Anthropic request used for conversion.
type Request struct {
	Model       string
	MaxTokens   int
	Stream      bool
	Temperature float64
	TopP        float64
	System      json.RawMessage
	Messages    []Message
	Tools       json.RawMessage
	ToolChoice  json.RawMessage
	Thinking    json.RawMessage
}

// Options carries caller policy that is not part of the wire request.
type Options struct {
	SystemOverride  string // e.g. override.md content; wins when non-empty
	ReasoningEffort string
	NewToolUseID    func() string
	Logf            func(format string, args ...any)
}

func (o Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

func (o Options) newToolUseID() string {
	if o.NewToolUseID != nil {
		return o.NewToolUseID()
	}
	return fmt.Sprintf("toolu_%x", time.Now().UnixNano())
}

// ToolsToOpenAI converts Anthropic tool definitions to OpenAI function tools.
// Optional fields are only copied when present so upstream gateways never see
// explicit nulls.
func ToolsToOpenAI(tools []any) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		tMap, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if tMap["type"] == "function" {
			out = append(out, t)
			continue
		}
		fnMap := map[string]any{"name": tMap["name"]}
		if d, _ := tMap["description"].(string); d != "" {
			fnMap["description"] = d
		}
		if tMap["input_schema"] != nil {
			fnMap["parameters"] = tMap["input_schema"]
		}
		out = append(out, map[string]any{"type": "function", "function": fnMap})
	}
	return out
}

// ExtractStringContent flattens a string or an array of text blocks.
func ExtractStringContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err == nil {
		parts := []string{}
		for _, b := range blocks {
			if b["type"] == "text" {
				if t, ok := b["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// ToOpenAI converts an Anthropic Messages request to OpenAI chat params.
func ToOpenAI(req Request, opts Options) map[string]any {
	openAI := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     req.Stream,
		"messages":   []any{},
	}
	if req.Temperature != 0 {
		openAI["temperature"] = req.Temperature
	}
	if req.TopP != 0 {
		openAI["top_p"] = req.TopP
	}
	// Anthropic thinking maps to OpenAI reasoning_effort: disabled -> "none"
	// (removed later by the upstream body builder), enabled/adaptive -> default.
	if req.Thinking != nil {
		var thinking struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(req.Thinking, &thinking); err == nil {
			switch thinking.Type {
			case "enabled", "adaptive":
				openAI["reasoning_effort"] = opts.ReasoningEffort
			case "disabled":
				openAI["reasoning_effort"] = "none"
			}
		}
	}
	if req.Tools != nil {
		var toolsArr []any
		if err := json.Unmarshal(req.Tools, &toolsArr); err == nil {
			openAI["tools"] = ToolsToOpenAI(toolsArr)
		}
	}
	if req.ToolChoice != nil {
		openAI["tool_choice"] = req.ToolChoice
	}

	msgs := []any{}
	sysContent := opts.SystemOverride
	sysSource := "override.md"
	if sysContent == "" {
		sysSource = "client"
		if req.System != nil {
			sysContent = ExtractStringContent(req.System)
		}
	}
	if sysContent != "" {
		opts.logf("  system prompt: %d bytes (from %s)", len(sysContent), sysSource)
		msgs = append(msgs, map[string]any{"role": "system", "content": sysContent})
	}

	for _, m := range req.Messages {
		switch c := m.Content.(type) {
		case string:
			msgs = append(msgs, map[string]any{"role": m.Role, "content": c})
		case []any:
			textParts := []string{}
			var toolCalls []any
			var toolResults []any

			for _, block := range c {
				b, ok := block.(map[string]any)
				if !ok {
					continue
				}
				switch b["type"] {
				case "text":
					if t, ok := b["text"].(string); ok {
						textParts = append(textParts, t)
					}
				case "image":
					// images are not forwarded
				case "tool_use":
					// Skip malformed tool_use blocks with an empty name.
					name, _ := b["name"].(string)
					if name == "" {
						continue
					}
					argsStr := "{}"
					if input, ok := b["input"]; ok && input != nil {
						if s, ok := input.(string); ok {
							argsStr = s
						} else if bts, err := json.Marshal(input); err == nil {
							argsStr = string(bts)
						}
					}
					id, _ := b["id"].(string)
					if id == "" {
						id = opts.newToolUseID()
					}
					toolCalls = append(toolCalls, map[string]any{
						"id":   id,
						"type": "function",
						"function": map[string]any{
							"name":      name,
							"arguments": argsStr,
						},
					})
				case "compaction":
					// Preserve prior compaction blocks as a summary system message
					// so findExistingSummary / compaction recognition works.
					if c, ok := b["content"].(string); ok && c != "" {
						msgs = append(msgs, map[string]any{
							"role":    "system",
							"content": "[Conversation Summary]\n" + c,
						})
					}
				case "tool_result":
					trContent := b["content"]
					if _, isStr := trContent.(string); !isStr && trContent != nil {
						if bts, err := json.Marshal(trContent); err == nil {
							trContent = string(bts)
						}
					}
					toolResults = append(toolResults, map[string]any{
						"role":         "tool",
						"content":      trContent,
						"tool_call_id": b["tool_use_id"],
					})
				}
			}

			if m.Role == "assistant" && len(toolCalls) > 0 {
				msgs = append(msgs, map[string]any{
					"role":       "assistant",
					"content":    strings.Join(textParts, "\n"),
					"tool_calls": toolCalls,
				})
			} else if m.Role == "user" && len(toolResults) > 0 {
				msgs = append(msgs, toolResults...)
				if len(textParts) > 0 {
					msgs = append(msgs, map[string]any{"role": "user", "content": strings.Join(textParts, "\n")})
				}
			} else {
				msgs = append(msgs, map[string]any{"role": m.Role, "content": strings.Join(textParts, "\n")})
			}
		}
	}

	openAI["messages"] = msgs
	return openAI
}

// FromOpenAI converts an OpenAI chat completion (or chunk simulated as one)
// into an Anthropic message object.
func FromOpenAI(openAI map[string]any, opts Options) map[string]any {
	out := map[string]any{
		"id":    "msg_" + fmt.Sprintf("%x", time.Now().UnixMilli()),
		"type":  "message",
		"role":  "assistant",
		"model": protocol.GetNested(openAI, "model"),
	}

	choices := protocol.GetNested(openAI, "choices")
	if choices == nil {
		out["content"] = []any{map[string]any{"type": "text", "text": ""}}
		out["stop_reason"] = "end_turn"
		out["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0}
		return out
	}

	choice0, _ := protocol.GetNested(openAI, "choices", 0).(map[string]any)
	msg, _ := choice0["message"].(map[string]any)
	if msg == nil {
		msg, _ = choice0["delta"].(map[string]any)
	}

	text := ""
	if msg != nil {
		if c, ok := msg["content"].(string); ok {
			text = protocol.SanitizeContent(c)
		}
	}

	// Upstream reasoning_content becomes an Anthropic thinking block, ordered
	// before text.
	var thinkingBlock map[string]any
	if msg != nil {
		if rc, ok := msg["reasoning_content"].(string); ok && rc != "" {
			thinkingBlock = map[string]any{"type": "thinking", "thinking": rc, "signature": ""}
		}
	}

	var contentBlocks []any
	if thinkingBlock != nil {
		contentBlocks = append(contentBlocks, thinkingBlock)
	}
	if text != "" || thinkingBlock == nil {
		contentBlocks = append(contentBlocks, map[string]any{"type": "text", "text": text})
	}

	if msg != nil {
		if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
			contentBlocks = []any{}
			if thinkingBlock != nil {
				contentBlocks = append(contentBlocks, thinkingBlock)
			}
			if text != "" {
				contentBlocks = append(contentBlocks, map[string]any{"type": "text", "text": text})
			}
			for _, tcItem := range tc {
				tcMap, ok := tcItem.(map[string]any)
				if !ok {
					continue
				}
				funcData, _ := tcMap["function"].(map[string]any)
				// Skip empty-name tool calls so clients never receive blocks
				// they cannot execute.
				name, _ := funcData["name"].(string)
				if name == "" {
					continue
				}
				input := funcData["arguments"]
				if argsStr, ok := input.(string); ok {
					var argsObj any
					if json.Unmarshal([]byte(argsStr), &argsObj) == nil {
						input = argsObj
					}
				}
				id, _ := tcMap["id"].(string)
				if id == "" {
					id = opts.newToolUseID()
				}
				contentBlocks = append(contentBlocks, map[string]any{
					"type":  "tool_use",
					"id":    id,
					"name":  name,
					"input": input,
				})
			}
		}
	}

	out["content"] = contentBlocks

	switch protocol.GetNested(openAI, "choices", 0, "finish_reason") {
	case "stop":
		out["stop_reason"] = "end_turn"
	case "length":
		out["stop_reason"] = "max_tokens"
	case "tool_calls":
		out["stop_reason"] = "tool_use"
	default:
		out["stop_reason"] = "end_turn"
	}

	usage := map[string]any{}
	if u := protocol.GetNested(openAI, "usage"); u != nil {
		if um, ok := u.(map[string]any); ok {
			usage["input_tokens"] = um["prompt_tokens"]
			usage["output_tokens"] = um["completion_tokens"]
		}
	}
	out["usage"] = usage

	return out
}
