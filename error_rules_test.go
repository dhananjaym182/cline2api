package main

// 请求级错误规则引擎测试：ruleDecision 的 action 映射，以及 provider 候选循环
// 中 stop（中止并按 requestErrorStop 包装）与 continue_and_cooldown（继续降级
// 但落一次规则冷却）的端到端行为。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cline-go-proxy/internal/config"
)

func installRuleConfig(t *testing.T, rules ...config.RequestErrorRule) {
	t.Helper()
	cfg := defaultProxyConfig()
	cfg.RequestErrorRules = rules
	cfg.Normalize()
	setProxyConfig(cfg)
	t.Cleanup(func() { setProxyConfig(defaultProxyConfig()) })
}

func TestRuleDecision(t *testing.T) {
	installRuleConfig(t,
		config.RequestErrorRule{StatusCodes: []int{429}, Match: "quota window", Action: config.ErrorActionStopAndCooldown},
		config.RequestErrorRule{Match: "server_reported_error_type", Action: config.ErrorActionContinue},
	)

	if stop, cool, ok := ruleDecision(&clineAPIError{statusCode: 429, message: "quota window reset in 6h"}); !stop || !cool || !ok {
		t.Fatalf("429 quota = stop:%v cool:%v ok:%v", stop, cool, ok)
	}
	if stop, cool, ok := ruleDecision(&clineAPIError{statusCode: 503, message: "x server_reported_error_type y"}); stop || cool || !ok {
		t.Fatalf("continue rule = stop:%v cool:%v ok:%v", stop, cool, ok)
	}
	if _, _, ok := ruleDecision(&clineAPIError{statusCode: 400, message: "other"}); ok {
		t.Fatal("unmatched API error must not match")
	}
	if _, _, ok := ruleDecision(errors.New("plain")); ok {
		t.Fatal("non-API error must not match")
	}
	if _, _, ok := ruleDecision(&requestErrorStop{err: &clineAPIError{statusCode: 429, message: "quota window"}}); ok {
		t.Fatal("already-ruled error must not re-match")
	}
}

func TestErrorRuleStopsAndCooldownsProviderLoop(t *testing.T) {
	var mu sync.Mutex
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte(`{"error":{"message":"budget exceeded for plan"}}`))
	}))
	defer srv.Close()

	installProviders(t,
		&CustomProvider{ID: "p1", Name: "One", BaseURL: srv.URL, APIKey: "k1", Enabled: true, Priority: 1, ModelIDs: []string{"x"}},
		&CustomProvider{ID: "p2", Name: "Two", BaseURL: srv.URL, APIKey: "k2", Enabled: true, Priority: 2, ModelIDs: []string{"x"}},
	)
	params := map[string]any{"model": "x", "messages": []any{}}

	// 无规则：两家都试，错误原样返回（对照）。
	installRuleConfig(t)
	_, err, attempted, _ := callCustomProviderAPI(map[string]any{"model": "x", "messages": []any{}}, false)
	if !attempted || err == nil {
		t.Fatalf("control: attempted=%v err=%v", attempted, err)
	}
	var st *requestErrorStop
	if errors.As(err, &st) {
		t.Fatal("control: error must not be rule-wrapped without rules")
	}
	mu.Lock()
	controlHits := hits
	mu.Unlock()
	if controlHits != 2 {
		t.Fatalf("control: both providers must be tried, hits=%d", controlHits)
	}

	// stop 规则：第一家失败即中止（只打一次上游），错误带 requestErrorStop 包装。
	mu.Lock()
	hits = 0
	mu.Unlock()
	installRuleConfig(t, config.RequestErrorRule{
		StatusCodes: []int{http.StatusTeapot}, Match: "budget exceeded", Action: config.ErrorActionStop,
	})
	_, err, attempted, _ = callCustomProviderAPI(params, false)
	if !attempted || err == nil {
		t.Fatalf("stop rule: attempted=%v err=%v", attempted, err)
	}
	if !errors.As(err, &st) {
		t.Fatalf("stop rule: error not wrapped in requestErrorStop: %v", err)
	}
	mu.Lock()
	stopHits := hits
	mu.Unlock()
	if stopHits != 1 {
		t.Fatalf("stop rule must skip remaining providers, hits=%d", stopHits)
	}

	// continue_and_cooldown：两家照试，但 provider 规则冷却落账。
	mu.Lock()
	hits = 0
	mu.Unlock()
	installRuleConfig(t, config.RequestErrorRule{
		StatusCodes: []int{http.StatusTeapot}, Match: "budget exceeded", Action: config.ErrorActionContinueAndCooldown,
	})
	_, err, attempted, _ = callCustomProviderAPI(params, false)
	if !attempted || err == nil || errors.As(err, &st) {
		t.Fatalf("continue rule: attempted=%v err=%v", attempted, err)
	}
	mu.Lock()
	continueHits := hits
	mu.Unlock()
	if continueHits != 2 {
		t.Fatalf("continue rule must try all providers, hits=%d", continueHits)
	}
	providersMu.Lock()
	until, cooled := providerCooldowns["p1:x"]
	providersMu.Unlock()
	if !cooled || !until.After(time.Now()) {
		t.Fatalf("rule cooldown not recorded: until=%v cooled=%v", until, cooled)
	}
	// 规则冷却不应改变后续候选语义（provider 循环本身已由上面 hits==2 验证）。
	if stop, cool, ok := ruleDecision(&clineAPIError{statusCode: http.StatusTeapot, message: "budget exceeded"}); stop || !cool || !ok {
		t.Fatalf("decision sanity after cooldown: stop:%v cool:%v ok:%v", stop, cool, ok)
	}
}
