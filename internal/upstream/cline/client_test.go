package cline

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatSendsHeadersAndBody(t *testing.T) {
	var gotPath, gotAuth, gotTask, gotExtra, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotTask = r.Header.Get("X-Task-ID")
		gotExtra = r.Header.Get("X-Custom")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := &Client{
		HTTP:         srv.Client(),
		BaseURL:      srv.URL + "/",
		ExtraHeaders: map[string]string{"X-Custom": "yes"},
	}
	resp, err := c.Chat(context.Background(), "token-1", "sess_1", []byte(`{"model":"m"}`))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer token-1" || gotTask != "sess_1" || gotExtra != "yes" {
		t.Fatalf("headers auth=%q task=%q extra=%q", gotAuth, gotTask, gotExtra)
	}
	if gotBody != `{"model":"m"}` {
		t.Fatalf("body = %q", gotBody)
	}
}

func TestChatPropagatesContextAndTransportErrors(t *testing.T) {
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport down")
	})}, BaseURL: "http://example.test"}
	_, err := c.Chat(context.Background(), "t", "s", nil)
	if err == nil || !strings.Contains(err.Error(), "transport down") {
		t.Fatalf("error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctxClient := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})}, BaseURL: "http://example.test"}
	_, err = ctxClient.Chat(ctx, "t", "s", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}

	if _, err := (&Client{}).Chat(context.Background(), "t", "s", nil); err == nil {
		t.Fatal("nil HTTP client must error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
