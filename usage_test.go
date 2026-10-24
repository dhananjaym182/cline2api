package main

import "testing"

func TestParseTokenUsageOpenAIFields(t *testing.T) {
	usage := parseTokenUsage(map[string]any{
		"prompt_tokens":     float64(120),
		"completion_tokens": float64(45),
		"total_tokens":      float64(165),
	})
	if !usage.Valid || usage.Prompt != 120 || usage.Completion != 45 || usage.Total != 165 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestParseTokenUsageFallsBackToSum(t *testing.T) {
	usage := parseTokenUsage(map[string]any{
		"input_tokens":  float64(7),
		"output_tokens": float64(3),
	})
	if !usage.Valid || usage.Prompt != 7 || usage.Completion != 3 || usage.Total != 10 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestParseTokenUsageTracksOpenAICachedTokens(t *testing.T) {
	usage := parseTokenUsage(map[string]any{
		"prompt_tokens":     float64(120),
		"completion_tokens": float64(45),
		"total_tokens":      float64(165),
		"prompt_tokens_details": map[string]any{
			"cached_tokens": float64(90),
		},
	})
	if usage.Cached != 90 || usage.Prompt != 120 || usage.Total != 165 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestParseTokenUsageTracksAnthropicCachedTokens(t *testing.T) {
	usage := parseTokenUsage(map[string]any{
		"input_tokens":                float64(100),
		"output_tokens":               float64(20),
		"cache_read_input_tokens":     float64(70),
		"cache_creation_input_tokens": float64(10),
	})
	if usage.Cached != 80 || usage.Prompt != 100 || usage.Total != 120 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestMergeTokenUsagePreservesStreamFields(t *testing.T) {
	merged := mergeTokenUsage(
		tokenUsage{Prompt: 100, Cached: 70, Valid: true},
		tokenUsage{Completion: 20, Total: 120, Valid: true},
	)
	if merged.Prompt != 100 || merged.Completion != 20 || merged.Cached != 70 || merged.Total != 120 {
		t.Fatalf("unexpected merged usage: %+v", merged)
	}
}

func TestParseTokenUsageRejectsMissingUsage(t *testing.T) {
	if usage := parseTokenUsage(nil); usage.Valid {
		t.Fatalf("usage should be invalid: %+v", usage)
	}
}
