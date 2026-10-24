package openaichat

import "cline-go-proxy/internal/protocol"

// Usage is a normalized token accounting record parsed from an upstream body.
type Usage struct {
	Prompt     int64
	Completion int64
	Total      int64
	Cached     int64
	Valid      bool
}

// ParseUsage reads usage from either OpenAI or Anthropic-shaped key sets. Only
// non-negative numbers are accepted; a usage object with none of the known
// counters is reported as invalid so callers can skip accounting.
func ParseUsage(value any) Usage {
	usage, ok := value.(map[string]any)
	if !ok {
		return Usage{}
	}
	read := func(keys ...string) int64 {
		for _, key := range keys {
			if v, ok := usage[key].(float64); ok && v >= 0 {
				return int64(v)
			}
		}
		return 0
	}
	readNested := func(parent string, keys ...string) int64 {
		details, ok := usage[parent].(map[string]any)
		if !ok {
			return 0
		}
		for _, key := range keys {
			if v, ok := details[key].(float64); ok && v >= 0 {
				return int64(v)
			}
		}
		return 0
	}
	prompt := read("prompt_tokens", "input_tokens")
	completion := read("completion_tokens", "output_tokens")
	cached := int64(0)
	if nested := readNested("prompt_tokens_details", "cached_tokens"); nested > 0 {
		cached = nested
	} else if nested := readNested("input_tokens_details", "cached_tokens"); nested > 0 {
		cached = nested
	} else if v := read("cache_read_input_tokens") + read("cache_creation_input_tokens"); v > 0 {
		cached = v
	} else {
		cached = read("prompt_cache_hit_tokens", "prompt_cache_creation_tokens", "cached_tokens")
	}
	total := read("total_tokens")
	if total == 0 {
		total = prompt + completion
	}
	return Usage{
		Prompt:     prompt,
		Completion: completion,
		Total:      total,
		Cached:     cached,
		Valid:      hasUsageKeys(usage),
	}
}

func hasUsageKeys(usage map[string]any) bool {
	for _, key := range []string{
		"prompt_tokens", "input_tokens", "completion_tokens", "output_tokens",
		"total_tokens", "cache_read_input_tokens", "cache_creation_input_tokens",
		"prompt_tokens_details", "input_tokens_details",
	} {
		if _, ok := usage[key]; ok {
			return true
		}
	}
	return false
}

// MergeUsage folds a later usage frame into the running total. Later non-zero
// values win, so a final cumulative frame replaces partial ones.
func MergeUsage(current, next Usage) Usage {
	if !next.Valid {
		return current
	}
	if next.Prompt != 0 {
		current.Prompt = next.Prompt
	}
	if next.Completion != 0 {
		current.Completion = next.Completion
	}
	if next.Total != 0 {
		current.Total = next.Total
	}
	if next.Cached != 0 {
		current.Cached = next.Cached
	}
	current.Valid = current.Valid || next.Valid
	if current.Total == 0 && (current.Prompt != 0 || current.Completion != 0) {
		current.Total = current.Prompt + current.Completion
	}
	return current
}

// HasFirstOutput reports whether a streamed chunk carries real content, as
// opposed to a role-only delta. Used to decide whether an attempt is still
// safe to retry on another route.
func HasFirstOutput(obj map[string]any) bool {
	choices, ok := protocol.GetNested(obj, "choices").([]any)
	if !ok || len(choices) == 0 {
		return false
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return false
	}
	if delta, ok := choice["delta"].(map[string]any); ok {
		if c, _ := delta["content"].(string); c != "" {
			return true
		}
		if tc, ok := delta["tool_calls"].([]any); ok && len(tc) > 0 {
			return true
		}
	}
	if msg, ok := choice["message"].(map[string]any); ok {
		if c, _ := msg["content"].(string); c != "" {
			return true
		}
		if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
			return true
		}
	}
	return false
}

// UnwrapDataEnvelope strips the {data:{...}} wrapper some upstreams return.
func UnwrapDataEnvelope(obj map[string]any) map[string]any {
	if data, ok := obj["data"]; ok {
		if d, ok := data.(map[string]any); ok {
			if _, hasChoices := d["choices"]; hasChoices {
				return d
			}
			if _, hasID := d["id"]; hasID {
				return d
			}
		}
	}
	return obj
}
