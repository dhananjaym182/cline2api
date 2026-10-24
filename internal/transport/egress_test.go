package transport

import (
	"strings"
	"testing"
)

func TestValidateProxyListAccepts(t *testing.T) {
	for _, list := range [][]string{
		nil,
		{},
		{"", "   "},
		{"http://proxy.example:8080"},
		{"https://user:pass@proxy.example:443"},
		{"socks5://127.0.0.1:1080", "socks5h://127.0.0.1:1081"},
	} {
		if err := ValidateProxyList(list); err != nil {
			t.Errorf("%v: unexpected error %v", list, err)
		}
	}
}

func TestValidateProxyListRejects(t *testing.T) {
	cases := map[string]string{
		"ftp://proxy:21":     "unsupported scheme",
		"http://proxy":       "missing port",
		"://bad":             "invalid",
		"http://host:notnum": "invalid",
	}
	for proxy, want := range cases {
		err := ValidateProxyList([]string{proxy})
		if err == nil {
			t.Errorf("%q: expected error", proxy)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %q does not contain %q", proxy, err, want)
		}
	}
}

func TestNormalizeProxySpec(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		isErr bool
	}{
		{"", "", false},
		{"  ", "", false},
		{"direct", "direct", false},
		{"None", "direct", false},
		{"http://user:pass@127.0.0.1:8080", "http://user:pass@127.0.0.1:8080", false},
		{"socks5://10.0.0.1:1080", "socks5://10.0.0.1:1080", false},
		{"ftp://127.0.0.1:21", "", true},
		{"http://127.0.0.1", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeProxySpec(c.in)
		if c.isErr {
			if err == nil {
				t.Errorf("NormalizeProxySpec(%q) expected error", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("NormalizeProxySpec(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}
