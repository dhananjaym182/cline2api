package main

// Cline 账号会话亲和测试：绑定/命中/过期/冷却回退/失效解绑，以及
// observeAffinityOutcome 的成功刷新与 401/429 清绑、5xx 保留语义。

import (
	"errors"
	"testing"
	"time"

	"cline-go-proxy/internal/config"
)

func installAffinityConfig(t *testing.T, enabled bool, ttl time.Duration) {
	t.Helper()
	cfg := defaultProxyConfig()
	cfg.SessionAffinity = config.SessionAffinityPolicy{Enabled: enabled, TTLSeconds: int(ttl.Seconds())}
	cfg.Normalize()
	setProxyConfig(cfg)
	t.Cleanup(func() { setProxyConfig(defaultProxyConfig()) })
	accountAffinity.mu.Lock()
	accountAffinity.binds = map[string]accountAffinityBind{}
	accountAffinity.mu.Unlock()
}

func affinityAccount(id, email, status string) *Account {
	return &Account{AccountID: id, Email: email, Status: status, ModelStats: map[string]*ModelStat{}, ModelCooldowns: map[string]time.Time{}}
}

func TestAccountAffinityBindGetExpiry(t *testing.T) {
	installAffinityConfig(t, true, time.Hour)
	acc1 := affinityAccount("a1", "one@x.io", "active")
	acc2 := affinityAccount("a2", "two@x.io", "active")
	installPool(t, []Model{{ID: "m1"}}, acc1, acc2)

	accountAffinity.bind("sk-1", acc1)
	if got := accountAffinity.get("sk-1", "m1", time.Now()); got == nil || got.AccountID != "a1" {
		t.Fatalf("bound account lookup = %+v, want a1", got)
	}
	// 冷却中的绑定账号 → 未命中（融入常规轮转）
	acc1.ModelCooldowns["m1"] = time.Now().Add(time.Minute)
	if got := accountAffinity.get("sk-1", "m1", time.Now()); got != nil {
		t.Fatal("cooling bound account must not be returned")
	}
	// 冷却到期后恢复命中
	acc1.ModelCooldowns["m1"] = time.Now().Add(-time.Second)
	if got := accountAffinity.get("sk-1", "m1", time.Now()); got == nil || got.AccountID != "a1" {
		t.Fatalf("expired cooldown lookup = %+v, want a1", got)
	}
	// TTL 到期 → 未命中
	if got := accountAffinity.get("sk-1", "m1", time.Now().Add(2*time.Hour)); got != nil {
		t.Fatal("expired binding must not match")
	}
	// 账号消失/非 active → 未命中
	accountAffinity.bind("sk-1", acc1)
	acc1.Status = "expired"
	if got := accountAffinity.get("sk-1", "m1", time.Now()); got != nil {
		t.Fatal("inactive bound account must not be returned")
	}
	acc1.Status = "active"
	if got := accountAffinity.get("sk-2", "m1", time.Now()); got != nil {
		t.Fatal("unknown session key must not match")
	}
	_ = acc2
}

func TestAccountAffinityDisabledNoop(t *testing.T) {
	installAffinityConfig(t, false, time.Hour)
	acc := affinityAccount("a1", "one@x.io", "active")
	installPool(t, []Model{{ID: "m1"}}, acc)

	accountAffinity.bind("sk-1", acc)
	if got := accountAffinity.get("sk-1", "m1", time.Now()); got != nil {
		t.Fatal("disabled affinity must not bind")
	}
}

func TestObserveAffinityOutcome(t *testing.T) {
	installAffinityConfig(t, true, time.Hour)
	acc := affinityAccount("a1", "one@x.io", "active")
	installPool(t, []Model{{ID: "m1"}}, acc)

	observeAffinityOutcome("sk-1", acc, nil)
	if _, pinned := pickAffinityAccount("sk-1", "m1"); !pinned {
		t.Fatal("success must bind the session")
	}
	// 瞬时 5xx：保留绑定
	observeAffinityOutcome("sk-1", acc, &clineAPIError{statusCode: 500, message: "boom"})
	if _, pinned := pickAffinityAccount("sk-1", "m1"); !pinned {
		t.Fatal("transient error must keep binding")
	}
	// 401：解绑
	observeAffinityOutcome("sk-1", acc, &clineAPIError{statusCode: 401, message: "token dead"})
	if _, pinned := pickAffinityAccount("sk-1", "m1"); pinned {
		t.Fatal("401 must clear binding")
	}
	// 重新绑定后 429：解绑
	observeAffinityOutcome("sk-1", acc, nil)
	observeAffinityOutcome("sk-1", acc, &clineAPIError{statusCode: 429, message: "rate limited"})
	if _, pinned := pickAffinityAccount("sk-1", "m1"); pinned {
		t.Fatal("429 must clear binding")
	}
	// account-unavailable：解绑
	observeAffinityOutcome("sk-1", acc, nil)
	observeAffinityOutcome("sk-1", acc, &clineAccountUnavailableError{err: errors.New("token fail")})
	if _, pinned := pickAffinityAccount("sk-1", "m1"); pinned {
		t.Fatal("account-unavailable must clear binding")
	}
	// 空 session key / nil 账号：安全无操作
	observeAffinityOutcome("", acc, nil)
	observeAffinityOutcome("sk-1", nil, nil)
}
