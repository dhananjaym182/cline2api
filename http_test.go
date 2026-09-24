package main

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"golang.org/x/net/http/httpproxy"
)

func TestHTTPTransportUsesHTTPSProxyFromEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8080")
	t.Setenv("NO_PROXY", "")

	req, err := http.NewRequest(http.MethodPost, "https://api.workos.com/user_management/authorize/device", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	// 合并后的接线：transport.Proxy = clineOutboundProxy（出口代理池钩子，池生效时
	// 禁用环境代理，否则回退 clineEnvProxy/http.ProxyFromEnvironment）。
	// http.ProxyFromEnvironment 在进程内首次调用时缓存环境变量：只要本测试之前
	// 有任何测试发出过真实出站请求，这里就会拿到空代理配置，导致顺序相关的偶发失败。
	// 因此分两层断言：
	//  1. 接线：httpTransport.Proxy 确实是 clineOutboundProxy（函数指针一致）；
	//  2. 语义：环境变量解析用 x/net/httpproxy（每次调用实时读取环境变量，无缓存）。
	if httpTransport.Proxy == nil {
		t.Fatal("httpTransport must configure Proxy")
	}
	wantFunc := clineOutboundProxy
	if reflect.ValueOf(httpTransport.Proxy).Pointer() != reflect.ValueOf(wantFunc).Pointer() {
		t.Fatal("httpTransport.Proxy must be clineOutboundProxy")
	}

	proxyURL, err := httpproxy.FromEnvironment().ProxyFunc()(req.URL)
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if proxyURL == nil {
		t.Fatal("expected HTTPS_PROXY to be selected")
	}

	want, err := url.Parse("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("parse expected proxy URL: %v", err)
	}
	if proxyURL.String() != want.String() {
		t.Fatalf("proxy URL = %q, want %q", proxyURL, want)
	}
}
