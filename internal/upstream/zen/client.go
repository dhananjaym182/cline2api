package zen

import (
	"bytes"
	"context"
	"net/http"
)

// Client builds Zen (opencode) upstream requests. It owns request shape and
// identity headers; retry, cooldown, and failover policy stay with dispatch.
type Client struct {
	HTTP     *http.Client
	BaseURL  string
	APIKey   string
	Headers  map[string]string
	IDs      *Identifier
	Sessions *SessionCache
}

// Protocol selects the upstream endpoint and identity flavor.
const (
	ProtocolChat      = "chat"
	ProtocolResponses = "responses"
)

// Endpoint returns the upstream path for a protocol.
func Endpoint(protocol string) string {
	if protocol == ProtocolResponses {
		return "/responses"
	}
	return "/chat/completions"
}

// BuildRequest constructs a POST request with opencode-compatible headers.
// modelID, when non-empty, is sent as x-opencode-model. Custom headers support
// the $session/$request/$project/$client placeholders; anything else is set
// verbatim.
func (c *Client) BuildRequest(ctx context.Context, protocol string, body []byte, params map[string]any, modelID string) (*http.Request, error) {
	sess := c.Sessions.SessionID(params)
	reqID := c.IDs.New("msg", false)
	endpoint := trimTrailingSlash(c.BaseURL) + Endpoint(protocol)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", UserAgent(protocol))
	req.Header.Set("x-opencode-project", "global")
	req.Header.Set("x-opencode-session", sess)
	req.Header.Set("x-opencode-request", reqID)
	req.Header.Set("x-opencode-client", "cli")
	req.Header.Set("x-opencode-session-id", sess)
	for k, v := range c.Headers {
		switch v {
		case "$session":
			req.Header.Set(k, sess)
		case "$request":
			req.Header.Set(k, reqID)
		case "$project":
			req.Header.Set(k, "global")
		case "$client":
			req.Header.Set(k, "cli")
		default:
			req.Header.Set(k, v)
		}
	}
	if modelID != "" {
		req.Header.Set("x-opencode-model", modelID)
	}
	return req, nil
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
