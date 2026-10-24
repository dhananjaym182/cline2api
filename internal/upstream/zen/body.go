package zen

import (
	"strings"

	"cline-go-proxy/internal/protocol/openaichat"
)

// AnonymousCoreTools are the core tool names the anonymous free tier expects;
// the agent-shape check rejects requests that omit any of them.
var AnonymousCoreTools = []string{"bash", "edit", "glob", "grep", "read"}

// BodyOptions supplies root-owned configuration to the body builder: the
// OpenAI fields that may pass through and the model alias resolver.
type BodyOptions struct {
	PassThroughKeys []string
	ResolveModel    func(model string) (string, bool)
}

// EnsureAnonymousTools tops up missing core tool definitions so the request
// reads as an agent session. Tools already declared by the client are kept
// as-is.
func EnsureAnonymousTools(body map[string]any) {
	raw, exists := body["tools"]
	if !exists {
		body["tools"] = AnonymousToolset(nil)
		return
	}
	items, ok := raw.([]any)
	if !ok {
		return
	}
	present := make(map[string]bool, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := entry["function"].(map[string]any)
		if !ok {
			continue
		}
		if name, _ := fn["name"].(string); name != "" {
			present[name] = true
		}
	}
	missing := make([]string, 0, len(AnonymousCoreTools))
	for _, name := range AnonymousCoreTools {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return
	}
	body["tools"] = append(items, AnonymousToolset(missing)...)
}

// AnonymousToolset returns tool definitions for names, defaulting to the core
// tool names.
func AnonymousToolset(names []string) []any {
	if names == nil {
		names = AnonymousCoreTools
	}
	tools := make([]any, 0, len(names))
	for _, name := range names {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": "Agent tool " + name,
				"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		})
	}
	return tools
}

// BuildChatBody constructs a zen chat request body: only OpenAI-compatible
// fields, with the model rewritten to the canonical zen ID. The anonymous free
// tier accepts only agent-shaped streaming requests, so stream is forced on
// and core tools are injected.
func BuildChatBody(params map[string]any, stream bool, anonymous bool, opts BodyOptions) map[string]any {
	body := map[string]any{}
	for _, key := range opts.PassThroughKeys {
		if val, ok := params[key]; ok {
			body[key] = val
		}
	}
	for _, key := range []string{"model", "max_tokens", "max_completion_tokens"} {
		if val, ok := params[key]; ok {
			body[key] = val
		}
	}
	if msgsRaw, ok := params["messages"]; ok {
		if msgsArr, ok := msgsRaw.([]any); ok {
			body["messages"] = openaichat.SanitizeMessages(msgsArr)
		} else {
			body["messages"] = msgsRaw
		}
	}
	wireStream := stream
	if anonymous {
		wireStream = true
	}
	body["stream"] = wireStream
	EnsureAnonymousTools(body)
	if anonymous && wireStream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if model, ok := params["model"].(string); ok {
		if opts.ResolveModel != nil {
			if id, ok := opts.ResolveModel(model); ok {
				body["model"] = id
			} else if model != "" {
				body["model"] = strings.TrimPrefix(model, "opencode/")
			}
		} else if model != "" {
			body["model"] = strings.TrimPrefix(model, "opencode/")
		}
	}
	delete(body, "reasoning_effort")
	delete(body, "reasoningEffort")
	openaichat.SanitizeTools(body)
	return body
}
