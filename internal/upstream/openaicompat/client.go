package openaicompat

import (
	"bytes"
	"net/http"
	"strings"
)

// Client builds OpenAI-compatible Post /chat/completions requests.
// Rotation, cooldowns, and accounting stay with the caller.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	APIKey  string
	Headers map[string]string
}

// RequestOptions overrides per-attempt identity. Token, when set, replaces the
// client API key (for connection-bound credentials); Headers are applied after
// the client headers, and an empty value removes a header.
type RequestOptions struct {
	Token   string
	Headers map[string]string
}

// Endpoint returns the chat-completions URL for the client base URL.
func (c *Client) Endpoint() string {
	return strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
}

// BuildRequest constructs a POST request carrying body and merged headers.
func (c *Client) BuildRequest(body []byte, opts RequestOptions) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, c.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	token := c.APIKey
	if opts.Token != "" {
		token = opts.Token
	}
	applyHeaders(req.Header, c.Headers)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	applyHeaders(req.Header, opts.Headers)
	return req, nil
}

func applyHeaders(dst http.Header, src map[string]string) {
	for k, v := range src {
		if v == "" {
			dst.Del(k)
			continue
		}
		dst.Set(k, v)
	}
}
