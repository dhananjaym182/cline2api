package cline

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Client executes chat requests against the Cline API. Dispatch owns retries,
// token refresh, and account state; this type only builds and sends one request.
type Client struct {
	HTTP         *http.Client
	BaseURL      string
	ExtraHeaders map[string]string
}

// Chat sends a prepared chat/completions body using the given bearer token.
func (c *Client) Chat(ctx context.Context, token, sessionID string, body []byte) (*http.Response, error) {
	if c.HTTP == nil {
		return nil, fmt.Errorf("cline: HTTP client is nil")
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header = Headers(token, sessionID, c.ExtraHeaders)
	return c.HTTP.Do(req)
}

// Headers returns the standard Cline request headers plus configured extras.
func Headers(token, sessionID string, extra map[string]string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set("Content-Type", "application/json")
	h.Set("X-Task-ID", sessionID)
	for k, v := range extra {
		h.Set(k, v)
	}
	return h
}
