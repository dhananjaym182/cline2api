package main

// RetryPolicy 辅助函数与瞬时冷却覆盖的单元测试。回退轮数/凭证上限的循环接线
// 在 callClineAPI（需完整 pool+上游 harness），这里覆盖其纯配置读取部分。

import (
	"testing"
	"time"

	"cline-go-proxy/internal/config"
)

func installRetryConfig(t *testing.T, r config.RetryPolicy) {
	t.Helper()
	cfg := defaultProxyConfig()
	cfg.Retry = r
	cfg.Normalize()
	setProxyConfig(cfg)
	t.Cleanup(func() { setProxyConfig(defaultProxyConfig()) })
}

func TestFailoverModelCap(t *testing.T) {
	installRetryConfig(t, config.RetryPolicy{})
	if got := failoverModelCap(); got != 3 {
		t.Fatalf("default cap = %d, want 3", got)
	}
	installRetryConfig(t, config.RetryPolicy{MaxFailoverModels: 7})
	if got := failoverModelCap(); got != 7 {
		t.Fatalf("configured cap = %d, want 7", got)
	}
}

func TestRetryRoundSettings(t *testing.T) {
	installRetryConfig(t, config.RetryPolicy{})
	rounds, credCap, interval := retryRoundSettings()
	if rounds != 1 || credCap != 0 || interval != 0 {
		t.Fatalf("defaults = rounds:%d credCap:%d interval:%v, want 1/0/0", rounds, credCap, interval)
	}

	installRetryConfig(t, config.RetryPolicy{MaxRounds: 3, MaxCredentialsPerRound: 2, MaxIntervalSeconds: 30})
	rounds, credCap, interval = retryRoundSettings()
	if rounds != 3 || credCap != 2 || interval != 30*time.Second {
		t.Fatalf("configured = rounds:%d credCap:%d interval:%v", rounds, credCap, interval)
	}
}

func TestTransientCooldownOverride(t *testing.T) {
	// 默认：5xx 走 dispatch 分类（30s）。
	installRetryConfig(t, config.RetryPolicy{})
	until := providerCooldownUntil(500, "", "")
	if d := time.Until(until); d < 20*time.Second || d > 40*time.Second {
		t.Fatalf("default 500 cooldown ~30s, got %v", d)
	}

	// 覆盖为 90s。
	installRetryConfig(t, config.RetryPolicy{TransientCooldownSeconds: 90})
	until = providerCooldownUntil(503, "", "")
	if d := time.Until(until); d < 80*time.Second || d > 100*time.Second {
		t.Fatalf("override 503 cooldown ~90s, got %v", d)
	}

	// -1 禁用瞬时冷却。
	installRetryConfig(t, config.RetryPolicy{TransientCooldownSeconds: -1})
	if until = providerCooldownUntil(500, "", ""); !until.IsZero() {
		t.Fatalf("disabled transient cooldown should be zero, got %v", until)
	}

	// 非瞬时（429）不受 TransientCooldownSeconds 影响。
	installRetryConfig(t, config.RetryPolicy{TransientCooldownSeconds: -1})
	if until = providerCooldownUntil(429, "", ""); until.IsZero() {
		t.Fatal("429 must keep its own cooldown even when transient disabled")
	}
}
