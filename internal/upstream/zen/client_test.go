package zen

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func newTestClient(headers map[string]string) *Client {
	return &Client{
		BaseURL:  "https://opencode.example/",
		APIKey:   "public",
		Headers:  headers,
		IDs:      NewIdentifier(),
		Sessions: NewSessionCache(),
	}
}

func TestBuildRequestChat(t *testing.T) {
	c := newTestClient(nil)
	params := map[string]any{"model": "m", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	req, err := c.BuildRequest(context.Background(), ProtocolChat, []byte(`{"stream":true}`), params, "upstream-id")
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s", req.Method)
	}
	if req.URL.String() != "https://opencode.example/chat/completions" {
		t.Fatalf("url = %s", req.URL.String())
	}
	if got := req.Header.Get("Authorization"); got != "Bearer public" {
		t.Fatalf("auth = %q", got)
	}
	if got := req.Header.Get("x-opencode-model"); got != "upstream-id" {
		t.Fatalf("model header = %q", got)
	}
	if sess := req.Header.Get("x-opencode-session"); !strings.HasPrefix(sess, "ses_") {
		t.Fatalf("session = %q", sess)
	}
	if req.Header.Get("x-opencode-session-id") != req.Header.Get("x-opencode-session") {
		t.Fatal("session headers disagree")
	}
	if id := req.Header.Get("x-opencode-request"); !strings.HasPrefix(id, "msg") {
		t.Fatalf("request id = %q", id)
	}
	body, _ := io.ReadAll(req.Body)
	if string(body) != `{"stream":true}` {
		t.Fatalf("body = %s", body)
	}
}

func TestBuildRequestResponses(t *testing.T) {
	c := newTestClient(nil)
	req, err := c.BuildRequest(context.Background(), ProtocolResponses, nil, map[string]any{"model": "muse"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/responses" {
		t.Fatalf("path = %s", req.URL.Path)
	}
	if !strings.Contains(req.Header.Get("User-Agent"), "opencode/") {
		t.Fatalf("ua = %q", req.Header.Get("User-Agent"))
	}
	if req.Header.Get("x-opencode-model") != "" {
		t.Fatal("model header should be omitted when empty")
	}
}

func TestBuildRequestCustomHeaders(t *testing.T) {
	c := newTestClient(map[string]string{
		"x-session-copy": "$session",
		"x-request-copy": "$request",
		"x-project":      "$project",
		"x-client":       "$client",
		"x-static":       "value",
	})
	req, err := c.BuildRequest(context.Background(), ProtocolChat, nil, map[string]any{"model": "m"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("x-session-copy") != req.Header.Get("x-opencode-session") {
		t.Fatal("$session placeholder not resolved")
	}
	if req.Header.Get("x-request-copy") != req.Header.Get("x-opencode-request") {
		t.Fatal("$request placeholder not resolved")
	}
	if req.Header.Get("x-project") != "global" || req.Header.Get("x-client") != "cli" {
		t.Fatal("static placeholders not resolved")
	}
	if req.Header.Get("x-static") != "value" {
		t.Fatal("static header lost")
	}
}

func TestBuildRequestContextAndEndpoint(t *testing.T) {
	c := newTestClient(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := c.BuildRequest(ctx, ProtocolChat, nil, map[string]any{"model": "m"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if req.Context().Err() == nil {
		t.Fatal("expected canceled context")
	}
	if Endpoint("unknown") != "/chat/completions" {
		t.Fatalf("unknown protocol endpoint = %s", Endpoint("unknown"))
	}
}
