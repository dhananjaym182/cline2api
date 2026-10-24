// Package httpx contains small HTTP response helpers shared by the public and
// admin APIs. Handlers keep their routing and auth logic; this package only
// owns wire-level response formatting.
package httpx

import (
	"encoding/json"
	"net/http"
)

// Response is the standard admin/public API envelope.
type Response struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// bodySerializer is implemented by writers that serialize a background
// keepalive with the handler's own writes (see keepaliveWriter). WriteJSON
// holds the writer's lock across the whole header+status+body sequence so a
// concurrent keepalive tick can never interleave with the real response.
type bodySerializer interface {
	WithLock(func())
}

// WriteJSON writes data as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	if s, ok := w.(bodySerializer); ok {
		s.WithLock(func() { writeJSONBody(w, status, data) })
		return
	}
	writeJSONBody(w, status, data)
}

func writeJSONBody(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// WriteAPI writes the standard API envelope.
func WriteAPI(w http.ResponseWriter, status int, resp Response) {
	WriteJSON(w, status, resp)
}

// CORS wraps a handler with the permissive headers used by the proxy endpoints.
func CORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, anthropic-version, anthropic-beta")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}
