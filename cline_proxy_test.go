package main

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

// resetClineProxyTestState 保存并清空 Cline 出口代理全局状态，避免测试间污染。
func resetClineProxyTestState(t *testing.T) {
	t.Helper()
	clineProxyCfgMu.Lock()
	oldCfg := clineProxyCfg
	clineProxyCfg = nil
	clineProxyCfgMu.Unlock()

	oldCount := clineProxyCount.Load()

	t.Cleanup(func() {
		clineProxyCfgMu.Lock()
		clineProxyCfg = oldCfg
		clineProxyCfgMu.Unlock()
		clineProxyCount.Store(oldCount)
	})
}

func setClineProxyTestConfig(t *testing.T, proxies []string, strategy string) {
	t.Helper()
	resetClineProxyTestState(t)
	clineProxyCfgMu.Lock()
	clineProxyCfg = &clineProxyConfigData{Proxies: proxies, ProxyStrategy: strategy}
	clineProxyCfgMu.Unlock()
}

func TestClineProxyConfigNormalize(t *testing.T) {
	cfg := &clineProxyConfigData{
		Proxies:       []string{" socks5://a:1080 ", "", "http://b:8080"},
		ProxyStrategy: "bogus",
	}
	normalizeClineProxyConfig(cfg)
	if len(cfg.Proxies) != 2 || cfg.Proxies[0] != "socks5://a:1080" || cfg.Proxies[1] != "http://b:8080" {
		t.Fatalf("unexpected proxies: %v", cfg.Proxies)
	}
	if cfg.ProxyStrategy != "round_robin" {
		t.Fatalf("bogus strategy should fall back to round_robin, got %q", cfg.ProxyStrategy)
	}
}

func TestClineOutboundProxyPrecedence(t *testing.T) {
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "api.cline.bot"}}

	// 应用内代理生效时禁用环境变量代理，避免双重代理
	setClineProxyTestConfig(t, []string{"socks5://127.0.0.1:1080"}, "round_robin")
	if u, err := clineOutboundProxy(req); u != nil || err != nil {
		t.Fatalf("app proxy active: want nil env proxy, got %v (err %v)", u, err)
	}

	// 未配置时回退环境变量代理钩子（替身，避免触发真实 ProxyFromEnvironment 缓存）
	sentinel := &url.URL{Scheme: "http", Host: "127.0.0.1:9999"}
	oldEnvProxy := clineEnvProxy
	clineEnvProxy = func(*http.Request) (*url.URL, error) { return sentinel, nil }
	t.Cleanup(func() { clineEnvProxy = oldEnvProxy })

	setClineProxyTestConfig(t, nil, "round_robin")
	got, err := clineOutboundProxy(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != sentinel {
		t.Fatalf("inactive: want env proxy fallback %v, got %v", sentinel, got)
	}
}

func TestClineDirectDialHost(t *testing.T) {
	direct := []string{"127.0.0.1:11434", "192.168.1.5:8080", "10.0.0.2:443", "[::1]:3457", "localhost:8080", "LOCALHOST:80"}
	for _, addr := range direct {
		if !clineDirectDialHost(addr) {
			t.Errorf("%q should dial directly", addr)
		}
	}
	viaProxy := []string{"api.cline.bot:443", "8.8.8.8:443"}
	for _, addr := range viaProxy {
		if clineDirectDialHost(addr) {
			t.Errorf("%q should dial via proxy", addr)
		}
	}
}

func TestPickClineProxyStrategies(t *testing.T) {
	proxies := []string{"socks5://a:1080", "socks5://b:1080", "socks5://c:1080"}

	setClineProxyTestConfig(t, nil, "round_robin")
	if p := pickClineProxy(); p != "" {
		t.Fatalf("empty config should return empty, got %q", p)
	}

	setClineProxyTestConfig(t, proxies, "fill")
	for i := 0; i < 5; i++ {
		if p := pickClineProxy(); p != proxies[0] {
			t.Fatalf("fill should always pick first, got %q", p)
		}
	}

	setClineProxyTestConfig(t, proxies, "round_robin")
	seen := map[string]bool{}
	for i := 0; i < len(proxies); i++ {
		seen[pickClineProxy()] = true
	}
	if len(seen) != len(proxies) {
		t.Fatalf("round_robin should cycle through all proxies, seen %v", seen)
	}

	setClineProxyTestConfig(t, proxies, "random")
	allowed := map[string]bool{"socks5://a:1080": true, "socks5://b:1080": true, "socks5://c:1080": true}
	for i := 0; i < 20; i++ {
		if p := pickClineProxy(); !allowed[p] {
			t.Fatalf("random returned unexpected proxy %q", p)
		}
	}
}

// TestClineDialContextDirectLoopback 验证应用内代理生效时回环目标仍直连
// （不经过代理隧道），且连接本机未监听端口会立刻失败而非经代理绕一圈。
func TestClineDialContextDirectLoopback(t *testing.T) {
	setClineProxyTestConfig(t, []string{"socks5://203.0.113.1:1080"}, "round_robin")
	_, err := clineDialContext(context.Background(), "tcp", "127.0.0.1:1")
	if err == nil {
		t.Fatal("dial to closed loopback port should fail")
	}
	if !clineDirectDialHost("127.0.0.1:1") {
		t.Fatal("loopback host should be classified as direct")
	}
}

// TestClineDialContextInactiveDirect 未配置代理时保持直连语义。
func TestClineDialContextInactiveDirect(t *testing.T) {
	setClineProxyTestConfig(t, nil, "round_robin")
	// 直连保留端口（127.0.0.1:1 不可拨）应失败，证明确实发起了拨号而非跳过
	_, err := clineDialContext(context.Background(), "tcp", "127.0.0.1:1")
	if err == nil {
		t.Fatal("dial to closed port should fail")
	}
}
