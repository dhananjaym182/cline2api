package main

import (
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const proxyEnvironmentHelper = "CLINE_PROXY_ENV_TEST_HELPER"

func TestHTTPTransportUsesHTTPSProxyFromEnvironment(t *testing.T) {
	if os.Getenv(proxyEnvironmentHelper) == "1" {
		clineProxyCfgMu.Lock()
		clineProxyCfg = defaultClineProxyConfig()
		clineProxyCfgMu.Unlock()

		req, err := http.NewRequest(http.MethodPost, "https://api.workos.com/user_management/authorize/device", nil)
		if err != nil {
			t.Fatalf("create request: %v", err)
		}

		proxyURL, err := httpTransport.Proxy(req)
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
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestHTTPTransportUsesHTTPSProxyFromEnvironment$", "-test.count=1")
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(entry, "=", 2)[0])
		switch key {
		case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", proxyEnvironmentHelper:
			continue
		}
		env = append(env, entry)
	}
	cmd.Env = append(env,
		proxyEnvironmentHelper+"=1",
		"HTTPS_PROXY=http://127.0.0.1:8080",
		"NO_PROXY=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated proxy environment test failed: %v\n%s", err, output)
	}
}
