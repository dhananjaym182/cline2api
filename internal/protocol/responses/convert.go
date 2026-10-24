// Package responses converts between the OpenAI Responses API and the OpenAI
// chat/completions representation used by upstreams. Pure data transforms only.
package responses

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToChat converts a Responses request body to a chat/completions body.
func ToChat(body map[string]any) map[string]any {
	out := map[string]any{}
	if m, ok := body["model"].(string); ok {
		out["model"] = m
	}
	if s, ok := body["stream"].(bool); ok {
		out["stream"] = s
	}
	if mt, ok := body["max_output_tokens"].(float64); ok {
		out["max_tokens"] = int(mt)
	}
	for _, k := range []string{"temperature", "top_p", "stop", "seed", "user", "metadata", "logit_bias"} {
		if v, ok := body[k]; ok {
			out[k] = v
		}
	}
	msgs := InputToMessages(body["input"])
	if instr, ok := body["instructions"].(string); ok && instr != "" {
		msgs = append([]any{map[string]any{"role": "system", "content": instr}}, msgs...)
	}
	out["messages"] = msgs
	if tools, ok := body["tools"].([]any); ok {
		out["tools"] = ToolsToChat(tools)
	}
	if tc, ok := body["tool_choice"]; ok {
		out["tool_choice"] = tc
	}
	return out
}

// InputToMessages handles both the string and item-array input forms.
func InputToMessages(input any) []any {
	var msgs []any
	switch v := input.(type) {
	case string:
		msgs = append(msgs, map[string]any{"role": "user", "content": v})
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch m["type"] {
			case "message":
				role, _ := m["role"].(string)
				if role == "" {
					role = "user"
				}
				msgs = append(msgs, map[string]any{"role": role, "content": StringifyContent(m["content"])})
			case "function_call":
				callID, _ := m["call_id"].(string)
				if callID == "" {
					callID, _ = m["id"].(string)
				}
				name, _ := m["name"].(string)
				args := ""
				switch a := m["arguments"].(type) {
				case string:
					args = a
				case map[string]any:
					if b, err := json.Marshal(a); err == nil {
						args = string(b)
					}
				}
				msgs = append(msgs, map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{map[string]any{
						"id":       callID,
						"type":     "function",
						"function": map[string]any{"name": name, "arguments": args},
					}},
				})
			case "function_call_output":
				callID, _ := m["call_id"].(string)
				if callID == "" {
					callID, _ = m["id"].(string)
				}
				output := ""
				switch o := m["output"].(type) {
				case string:
					output = o
				case map[string]any:
					if b, err := json.Marshal(o); err == nil {
						output = string(b)
					}
				}
				msgs = append(msgs, map[string]any{"role": "tool", "content": output, "tool_call_id": callID})
			case "reasoning":
				// reasoning items cannot be represented in chat input
			}
		}
	}
	return msgs
}

// StringifyContent flattens string or content-block input to text.
func StringifyContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := []string{}
		for _, block := range v {
			if b, ok := block.(map[string]any); ok {
				if t, ok := b["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// ToolsToChat converts flat Responses function tools to the nested chat shape.
func ToolsToChat(tools []any) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		tm, ok := t.(map[string]any)
		if !ok || tm["type"] != "function" {
			continue
		}
		fn := map[string]any{}
		if n, ok := tm["name"].(string); ok {
			fn["name"] = n
		}
		if d, ok := tm["description"].(string); ok {
			fn["description"] = d
		}
		if p, ok := tm["parameters"].(map[string]any); ok {
			fn["parameters"] = p
		}
		out = append(out, map[string]any{"type": "function", "function": fn})
	}
	return out
}

// NewID builds a Responses-style object id.
func NewID(prefix string) string {
	return prefix + fmt.Sprintf("%x", time.Now().UnixNano())
}

// ChatToResponses converts a chat completion to a Responses response object.
func ChatToResponses(chat map[string]any) map[string]any {
	resp := map[string]any{
		"id":          NewID("resp_"),
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      "completed",
		"model":       chat["model"],
		"output":      []any{},
		"output_text": "",
	}
	var outputs []any
	var outputText strings.Builder

	choices, _ := chat["choices"].([]any)
	if len(choices) > 0 {
		if ch, ok := choices[0].(map[string]any); ok {
			msg, _ := ch["message"].(map[string]any)
			if msg == nil {
				msg, _ = ch["delta"].(map[string]any)
			}
			content := []any{}
			var text string
			if msg != nil {
				text, _ = msg["content"].(string)
			}
			if text != "" {
				outputText.WriteString(text)
				content = append(content, map[string]any{"type": "output_text", "text": text, "annotations": []any{}})
			}
			outputs = append(outputs, map[string]any{
				"type":        "message",
				"id":          NewID("msg_"),
				"status":      "completed",
				"role":        "assistant",
				"content":     content,
				"output_text": outputText.String(),
			})
			if msg != nil {
				if tc, ok := msg["tool_calls"].([]any); ok {
					for _, c := range tc {
						cm, ok := c.(map[string]any)
						if !ok {
							continue
						}
						fn, _ := cm["function"].(map[string]any)
						// Skip empty-name tool calls clients cannot execute.
						var name, args string
						if fn != nil {
							name, _ = fn["name"].(string)
							args, _ = fn["arguments"].(string)
						}
						if name == "" {
							continue
						}
						callID, _ := cm["id"].(string)
						if callID == "" {
							callID = NewID("fc_")
						}
						outputs = append(outputs, map[string]any{
							"type":      "function_call",
							"id":        NewID("fc_"),
							"call_id":   callID,
							"name":      name,
							"arguments": args,
							"status":    "completed",
						})
					}
				}
			}
		}
	}
	resp["output"] = outputs
	resp["output_text"] = outputText.String()

	if u, ok := chat["usage"].(map[string]any); ok {
		details := map[string]any{}
		if pd, ok := u["prompt_tokens_details"].(map[string]any); ok {
			details["cached_tokens"] = pd["cached_tokens"]
		}
		od := map[string]any{}
		if rd, ok := u["completion_tokens_details"].(map[string]any); ok {
			od["reasoning_tokens"] = rd["reasoning_tokens"]
		} else if rd, ok := u["reasoning_tokens"]; ok {
			od["reasoning_tokens"] = rd
		}
		resp["usage"] = map[string]any{
			"input_tokens":          u["prompt_tokens"],
			"input_tokens_details":  details,
			"output_tokens":         u["completion_tokens"],
			"output_tokens_details": od,
			"total_tokens":          u["total_tokens"],
		}
	}
	return resp
}

// Usage is the protocol-neutral token accounting used by Responses output.
type Usage struct {
	Prompt     int64
	Completion int64
	Cached     int64
	Total      int64
}

// UsageToResponses builds the Responses usage object.
func UsageToResponses(u Usage) map[string]any {
	cached := any(0)
	if u.Cached > 0 {
		cached = u.Cached
	}
	total := u.Total
	if total == 0 {
		total = u.Prompt + u.Completion
	}
	return map[string]any{
		"input_tokens":         u.Prompt,
		"input_tokens_details": map[string]any{"cached_tokens": cached},
		"output_tokens":        u.Completion,
		"total_tokens":         total,
	}
}
