package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestStreamAnthropicComboContinuesWhenEntryHitsContextLimit locks down the
// combo streaming fallback contract: one entry rejecting the payload for
// context length must not abort the whole combo while later entries could
// still serve the (truncated) request.
func TestStreamAnthropicComboContinuesWhenEntryHitsContextLimit(t *testing.T) {
	t.Setenv(dataDirEnv, t.TempDir())
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
		resetRoutingState()
	})

	acc := &Account{
		AccountID:   "ctx-account",
		Email:       "ctx@example.com",
		AccessToken: "ctx-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{acc}}
	cfg := defaultProxyConfig()
	cfg.OnlyFree = false
	setProxyConfig(cfg)

	const longModel = "combo-entry-long-context"
	const shortModel = "combo-entry-short-context"
	var requested []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		model, _ := payload["model"].(string)
		requested = append(requested, model)
		if model == longModel {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"error":{"message":"maximum context length is 128000 tokens"}}`)),
				Request: req,
			}, nil
		}
		sse := "data: {\"choices\":[{\"delta\":{\"content\":\"fallback-ok\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(sse)),
			Request:    req,
		}, nil
	})

	combo := ModelCombo{
		ID:      "context-combo",
		Enabled: true,
		Entries: []ComboEntry{{Model: longModel}, {Model: shortModel}},
	}
	params := map[string]any{
		"model":  combo.ID,
		"stream": true,
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	rec := httptest.NewRecorder()
	reqLog := RequestLog{StartedAt: time.Now(), Protocol: "anthropic", Model: combo.ID, Stream: true}
	streamAnthropicCombo(context.Background(), rec, params, combo, &reqLog, "ctx-session", "", nil)

	out := rec.Body.String()
	if !strings.Contains(out, "fallback-ok") {
		t.Fatalf("combo did not fall through to the next entry after a context-limit rejection; requested=%v body=%s",
			requested, out)
	}
	if strings.Contains(out, `"type":"error"`) {
		t.Fatalf("combo emitted an in-band error instead of failing over; body=%s", out)
	}
}
