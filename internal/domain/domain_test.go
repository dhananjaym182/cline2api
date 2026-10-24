package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func marshalKeySet(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestAccountJSONContract(t *testing.T) {
	acct := Account{
		AccountID:    "acct-1",
		Email:        "user@example.com",
		RefreshToken: "refresh",
		AccessToken:  "secret-access",
		ExpiresAt:    12345,
		Status:       "active",
	}
	got := marshalKeySet(t, acct)

	if got["accountId"] != "acct-1" {
		t.Errorf("accountId = %v, want acct-1", got["accountId"])
	}
	if got["refreshToken"] != "refresh" {
		t.Errorf("refreshToken = %v, want refresh", got["refreshToken"])
	}
	for _, hidden := range []string{"accessToken", "AccessToken", "expiresAt", "ExpiresAt"} {
		if _, ok := got[hidden]; ok {
			t.Errorf("credential field %q must not be serialized", hidden)
		}
	}
	for _, omitted := range []string{"modelStats", "modelCooldowns"} {
		if _, ok := got[omitted]; ok {
			t.Errorf("empty field %q should be omitted", omitted)
		}
	}
	// time.Time is a struct, so omitempty keeps serializing its zero value;
	// this matches the pre-extraction wire format.
	if _, ok := got["cooldownUntil"]; !ok {
		t.Error("cooldownUntil must keep serializing its zero value")
	}
}

func TestRequestLogToolNamesNotSerialized(t *testing.T) {
	log := RequestLog{
		ID:            "req-1",
		AccountID:     "acct-1",
		Protocol:      "openai-chat",
		Model:         "gpt-test",
		OutputTPS:     12.5,
		ToolNames:     map[string]string{"t1": "long_tool_name"},
		FallbackCount: 2,
	}
	got := marshalKeySet(t, log)

	if got["outputTokensPerSecond"] != 12.5 {
		t.Errorf("outputTokensPerSecond = %v, want 12.5", got["outputTokensPerSecond"])
	}
	if got["fallbackCount"] != float64(2) {
		t.Errorf("fallbackCount = %v, want 2", got["fallbackCount"])
	}
	if _, ok := got["ToolNames"]; ok {
		t.Error("ToolNames must not appear in JSON")
	}
	if _, ok := got["toolNames"]; ok {
		t.Error("toolNames must not appear in JSON")
	}
}

func TestRequestLogComboAttributionRoundTrip(t *testing.T) {
	started := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	in := RequestLog{
		ID:             "req-2",
		StartedAt:      started,
		Model:          "served-model",
		RequestedModel: "my-combo",
		ComboID:        "my-combo",
		ComboName:      "My Combo",
		FallbackUsed:   true,
		Attempts: []AttemptLog{
			{Model: "first", Order: 1, Outcome: "failed", Reason: "429"},
			{Model: "served-model", Order: 2, Outcome: "success", ProviderName: "openrouter"},
		},
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out RequestLog
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.RequestedModel != "my-combo" || out.Model != "served-model" {
		t.Errorf("model fields = %q/%q", out.RequestedModel, out.Model)
	}
	if len(out.Attempts) != 2 || out.Attempts[1].ProviderName != "openrouter" {
		t.Errorf("attempts not preserved: %+v", out.Attempts)
	}
	if !out.StartedAt.Equal(started) {
		t.Errorf("startedAt = %v, want %v", out.StartedAt, started)
	}
}

func TestCustomProviderJSONContract(t *testing.T) {
	p := CustomProvider{
		ID:         "p1",
		Name:       "OpenRouter",
		BaseURL:    "https://openrouter.ai/api/v1",
		APIKey:     "sk-test",
		ModelIDs:   []string{"a/b"},
		Enabled:    true,
		Free:       true,
		DailyLimit: 100,
		CreatedAt:  time.Unix(0, 0).UTC(),
	}
	got := marshalKeySet(t, p)

	want := map[string]any{
		"id":                "p1",
		"baseURL":           "https://openrouter.ai/api/v1",
		"dailyRequestLimit": float64(100),
		"free":              true,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if ids, ok := got["modelIds"].([]any); !ok || len(ids) != 1 || ids[0] != "a/b" {
		t.Errorf("modelIds = %v", got["modelIds"])
	}
	for _, omitted := range []string{"headers", "timeoutSec", "connectionId", "connectionIds", "rpmLimit"} {
		if _, ok := got[omitted]; ok {
			t.Errorf("empty field %q should be omitted", omitted)
		}
	}
}

func TestConnectionJSONContract(t *testing.T) {
	c := Connection{
		ID:          "conn-1",
		Provider:    "kilocode",
		AuthType:    "device_code",
		AccessToken: "access",
		Status:      "active",
	}
	got := marshalKeySet(t, c)
	if got["accessToken"] != "access" {
		t.Errorf("accessToken = %v", got["accessToken"])
	}
	for _, omitted := range []string{"refreshToken", "expiresAt", "providerSpecificData", "email", "usageCount"} {
		if _, ok := got[omitted]; ok {
			t.Errorf("empty field %q should be omitted", omitted)
		}
	}
}

func TestModelComboJSONContract(t *testing.T) {
	c := ModelCombo{
		ID:      "fast",
		Name:    "Fast",
		Enabled: true,
		Entries: []ComboEntry{
			{Model: "m1"},
			{Model: "m2", Disabled: true, Source: "provider", Provider: "openrouter"},
		},
	}
	got := marshalKeySet(t, c)
	entries, ok := got["entries"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("entries = %v", got["entries"])
	}
	second := entries[1].(map[string]any)
	if second["model"] != "m2" || second["disabled"] != true || second["provider"] != "openrouter" {
		t.Errorf("second entry = %v", second)
	}
	if _, ok := entries[0].(map[string]any)["disabled"]; ok {
		t.Error("false disabled should be omitted")
	}
}

func TestLoginMethodValues(t *testing.T) {
	if MethodDeviceOAuth != 0 || MethodRefreshToken != 1 || MethodSSOCookie != 2 {
		t.Errorf("login method iota values changed: %d %d %d", MethodDeviceOAuth, MethodRefreshToken, MethodSSOCookie)
	}
}

func TestModelOmittedDefaults(t *testing.T) {
	m := Model{ID: "m", Provider: "p", Cost: "free", Status: "active", Custom: true}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "delisted") || strings.Contains(string(raw), "metaLocked") {
		t.Errorf("empty optional flags should be omitted: %s", raw)
	}
	if !strings.Contains(string(raw), `"cost":"free"`) || !strings.Contains(string(raw), `"custom":true`) {
		t.Errorf("required fields missing: %s", raw)
	}
}
