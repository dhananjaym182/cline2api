package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAcceptLanguage(t *testing.T) {
	tests := []struct {
		header string
		want   locale
	}{
		{"", localeZH},
		{"zh-CN,zh;q=0.9,en;q=0.8", localeZH},
		{"en-US,en;q=0.9", localeEN},
		{"en,zh-CN;q=0.8", localeEN},
		{"zh;q=0.8,en;q=0.9", localeEN},
		{"fr-FR,fr;q=0.9", localeZH},
		{"en", localeEN},
		{"zh", localeZH},
	}
	for _, tt := range tests {
		if got := parseAcceptLanguage(tt.header); got != tt.want {
			t.Errorf("parseAcceptLanguage(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestRequestLocaleCookieOverridesHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/admin/api/stats", nil)
	r.Header.Set("Accept-Language", "en-US")
	r.AddCookie(&http.Cookie{Name: adminLangCookie, Value: "zh"})
	if got := requestLocale(r); got != localeZH {
		t.Fatalf("cookie should override Accept-Language, got %q", got)
	}

	r = httptest.NewRequest(http.MethodGet, "/admin/api/stats", nil)
	r.Header.Set("Accept-Language", "zh-CN")
	r.AddCookie(&http.Cookie{Name: adminLangCookie, Value: "en"})
	if got := requestLocale(r); got != localeEN {
		t.Fatalf("cookie should override Accept-Language, got %q", got)
	}
}

func TestTAPI(t *testing.T) {
	zh := httptest.NewRequest(http.MethodGet, "/", nil)
	zh.AddCookie(&http.Cookie{Name: adminLangCookie, Value: "zh"})
	en := httptest.NewRequest(http.MethodGet, "/", nil)
	en.AddCookie(&http.Cookie{Name: adminLangCookie, Value: "en"})

	if got := tAPI(zh, "login_required"); got != "需要登录" {
		t.Fatalf("zh login_required = %q", got)
	}
	if got := tAPI(en, "login_required"); got != "Login required" {
		t.Fatalf("en login_required = %q", got)
	}
	if got := tAPI(en, "imported_accounts", 3, 1, 2); got != "Imported 3 accounts, 1 failed, 2 duplicates skipped" {
		t.Fatalf("en imported_accounts = %q", got)
	}
	if got := tAPI(zh, "imported_accounts", 3, 1, 2); got != "已导入 3 个账号，失败 1 个，跳过重复 2 个" {
		t.Fatalf("zh imported_accounts = %q", got)
	}
}
