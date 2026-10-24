package openaicompat

import (
	"io"
	"net/http"
	"testing"
)

func TestClientBuildRequestDefaults(t *testing.T) {
	c := &Client{BaseURL: "https://api.example/v1/", APIKey: "sk-test"}
	req, err := c.BuildRequest([]byte(`{"model":"m"}`), RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s", req.Method)
	}
	if req.URL.String() != "https://api.example/v1/chat/completions" {
		t.Fatalf("url = %s", req.URL.String())
	}
	if req.Header.Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("auth = %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content-type = %q", req.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(req.Body)
	if string(body) != `{"model":"m"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestClientBuildRequestTokenAndHeaders(t *testing.T) {
	c := &Client{
		BaseURL: "https://api.example",
		APIKey:  "sk-base",
		Headers: map[string]string{"X-Static": "on", "X-Drop": "gone", "Authorization": "ignored"},
	}
	req, err := c.BuildRequest(nil, RequestOptions{
		Token:   "conn-token",
		Headers: map[string]string{"X-Drop": "", "X-Org": "org-1", "X-Static": "override"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer conn-token" {
		t.Fatalf("auth = %q", got)
	}
	if req.Header.Get("X-Drop") != "" {
		t.Fatal("empty header value should delete")
	}
	if req.Header.Get("X-Org") != "org-1" {
		t.Fatalf("org = %q", req.Header.Get("X-Org"))
	}
	if req.Header.Get("X-Static") != "override" {
		t.Fatalf("static = %q", req.Header.Get("X-Static"))
	}
	if req.Header.Get("Authorization") != "Bearer conn-token" {
		t.Fatal("client Authorization header must not override credentials")
	}
}

func TestClientBuildRequestTokenKeepsBaseWhenEmpty(t *testing.T) {
	c := &Client{BaseURL: "https://api.example", APIKey: "sk-base"}
	req, _ := c.BuildRequest(nil, RequestOptions{Token: "", Headers: map[string]string{"Authorization": ""}})
	// An explicit empty extra header deletes Authorization; this documents the
	// merge order (extras win over client headers).
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("auth = %q", req.Header.Get("Authorization"))
	}
}
