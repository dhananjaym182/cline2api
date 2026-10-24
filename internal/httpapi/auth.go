// Package httpapi holds transport-layer HTTP concerns shared by public and
// admin endpoints: request authentication and health reporting. Route wiring
// and provider/dispatch behavior stay in the composition root.
package httpapi

import (
	"net/http"
	"strings"

	"cline-go-proxy/internal/httpx"
)

// APIKeyAuth guards public endpoints with pool-managed API keys. When no keys
// are configured the endpoint stays open, matching the historical behavior.
type APIKeyAuth struct {
	// Keys returns the currently configured keys. Empty means "no auth".
	Keys func() []string
	// Unauthorized writes the rejection body. It is injected so each protocol
	// keeps its own error shape.
	Unauthorized func(w http.ResponseWriter, r *http.Request)
}

// ExtractKey reads x-api-key or a bearer Authorization header.
func ExtractKey(r *http.Request) string {
	if key := r.Header.Get("x-api-key"); key != "" {
		return key
	}
	if b := r.Header.Get("Authorization"); strings.HasPrefix(b, "Bearer ") {
		return b[len("Bearer "):]
	}
	return ""
}

// Wrap applies CORS and the key check around next.
func (a *APIKeyAuth) Wrap(next http.HandlerFunc) http.HandlerFunc {
	return httpx.CORS(func(w http.ResponseWriter, r *http.Request) {
		keys := a.Keys()
		if len(keys) == 0 {
			next(w, r)
			return
		}
		key := ExtractKey(r)
		for _, k := range keys {
			if k == key {
				next(w, r)
				return
			}
		}
		a.Unauthorized(w, r)
	})
}

// Health reports liveness and account availability.
type Health struct {
	Version        func() string
	ActiveAccounts func() int
}

// Handler writes the health payload.
func (h Health) Handler(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"version":        h.Version(),
		"activeAccounts": h.ActiveAccounts(),
	})
}
