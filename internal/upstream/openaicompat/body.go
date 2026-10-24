// Package openaicompat contains shared helpers for conventional
// OpenAI-compatible upstreams. It never performs HTTP; callers own transport,
// connection rotation, and retries.
package openaicompat

import (
	"encoding/json"
	"fmt"

	"cline-go-proxy/internal/protocol/openaichat"
)

// BodyOptions supplies root-owned limits to the request builder.
type BodyOptions struct {
	PassThroughKeys     []string
	MinMaxTokens        int
	DefaultMaxTokens    int
	ModelMaxOutputLimit func(model string) int
	// Logf receives clamp messages; nil disables logging.
	Logf func(format string, args ...any)
}

// BuildChatBody constructs a chat/completions request body. It copies the
// allowed fields, repairs malformed tool_call history, clamps output budgets to
// the upstream minimum and the model's known hard limit, and strips null tool
// fields that some gateways reject.
func BuildChatBody(params map[string]any, stream bool, opts BodyOptions) map[string]any {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	body := map[string]any{}
	for _, k := range opts.PassThroughKeys {
		if v, ok := params[k]; ok {
			body[k] = v
		}
	}
	for _, k := range []string{"model", "messages", "max_tokens", "max_completion_tokens", "stream"} {
		if v, ok := params[k]; ok {
			body[k] = v
		}
	}
	if msgs, ok := params["messages"].([]any); ok {
		body["messages"] = openaichat.SanitizeMessages(msgs)
	}
	body["stream"] = stream
	if _, ok := body["max_tokens"]; !ok {
		if mt, ok := params["max_tokens"].(float64); ok {
			body["max_tokens"] = mt
		}
	}
	if mt, ok := body["max_tokens"].(float64); ok && mt < float64(opts.MinMaxTokens) {
		logf("  provider clamp max_tokens=%d -> %d (upstream requires >= %d)", int(mt), opts.DefaultMaxTokens, opts.MinMaxTokens)
		body["max_tokens"] = opts.DefaultMaxTokens
	}
	if mt, ok := body["max_completion_tokens"].(float64); ok && mt < float64(opts.MinMaxTokens) {
		logf("  provider clamp max_completion_tokens=%d -> %d (upstream requires >= %d)", int(mt), opts.DefaultMaxTokens, opts.MinMaxTokens)
		body["max_completion_tokens"] = opts.DefaultMaxTokens
	}
	if modelID, _ := body["model"].(string); modelID != "" && opts.ModelMaxOutputLimit != nil {
		if limit := opts.ModelMaxOutputLimit(modelID); limit > 0 {
			if mt, ok := body["max_tokens"].(float64); ok && mt > float64(limit) {
				logf("  provider clamp max_tokens=%d -> %d (model %q output limit)", int(mt), limit, modelID)
				body["max_tokens"] = float64(limit)
			}
			if mt, ok := body["max_completion_tokens"].(float64); ok && mt > float64(limit) {
				logf("  provider clamp max_completion_tokens=%d -> %d (model %q output limit)", int(mt), limit, modelID)
				body["max_completion_tokens"] = float64(limit)
			}
		}
	}
	openaichat.SanitizeTools(body)
	return body
}

// PrepareBody is BuildChatBody plus JSON serialization, returning a wrapped
// marshal error suitable for logs.
func PrepareBody(params map[string]any, stream bool, opts BodyOptions) ([]byte, error) {
	data, err := json.Marshal(BuildChatBody(params, stream, opts))
	if err != nil {
		return nil, fmt.Errorf("marshal provider body: %w", err)
	}
	return data, nil
}
