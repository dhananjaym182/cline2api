package openaichat

import "cline-go-proxy/internal/protocol"

// dropMetadataKeys copies m without provider/proxy bookkeeping fields.
func dropMetadataKeys(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k == "provider_metadata" || k == "proxy_metadata" {
			continue
		}
		out[k] = v
	}
	return out
}

// NormalizeMessage strips metadata, keeps content present when tool calls exist,
// and sanitizes text content.
func NormalizeMessage(msg map[string]any) map[string]any {
	out := dropMetadataKeys(msg)
	if tc, ok := out["tool_calls"].([]any); ok && len(tc) > 0 {
		if out["content"] == nil {
			out["content"] = ""
		}
	}
	if c, ok := out["content"].(string); ok {
		out["content"] = protocol.SanitizeContent(c)
	}
	return out
}

// NormalizeResponse strips metadata from a chat completion and its choices so
// upstream bookkeeping never reaches clients.
func NormalizeResponse(obj map[string]any) map[string]any {
	out := dropMetadataKeys(obj)
	choices, ok := out["choices"].([]any)
	if !ok {
		return out
	}
	normalized := make([]any, 0, len(choices))
	for _, ch := range choices {
		c, ok := ch.(map[string]any)
		if !ok {
			normalized = append(normalized, ch)
			continue
		}
		nc := dropMetadataKeys(c)
		if msg, ok := nc["message"].(map[string]any); ok {
			nc["message"] = NormalizeMessage(msg)
		}
		if delta, ok := nc["delta"].(map[string]any); ok {
			nd := dropMetadataKeys(delta)
			if tc, ok := nd["tool_calls"].([]any); ok && len(tc) > 0 {
				if nd["content"] == nil {
					nd["content"] = ""
				}
			}
			nc["delta"] = nd
		}
		normalized = append(normalized, nc)
	}
	out["choices"] = normalized
	return out
}
