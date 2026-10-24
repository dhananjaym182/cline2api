package dispatch

import (
	"encoding/json"
	"net/http"
)

// SSEWriter wraps an http.ResponseWriter for streaming responses. It makes
// header emission idempotent so a pre-commit retry can reuse the same response:
//   - Start is idempotent and will not repeat WriteHeader on retry;
//   - Write flushes after every chunk so intermediaries see bytes immediately.
//
// Started records whether any bytes have been committed; callers use it to
// decide between a JSON status error and an in-band error event.
type SSEWriter struct {
	W                 http.ResponseWriter
	Flusher           http.Flusher
	Started           bool // response headers already sent
	Envelope          bool // message_start already emitted (once per response)
	CompactionEmitted bool // compaction block already emitted (retry-safe)
}

// NewSSEWriter builds a writer around w. A missing http.Flusher is tolerated;
// writes still succeed but are not explicitly flushed.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	f, _ := w.(http.Flusher)
	return &SSEWriter{W: w, Flusher: f}
}

// Start commits SSE headers exactly once.
func (s *SSEWriter) Start() {
	if s.Started {
		return
	}
	s.Started = true
	s.W.Header().Set("Content-Type", "text/event-stream")
	s.W.Header().Set("Cache-Control", "no-cache")
	s.W.Header().Set("Connection", "keep-alive")
	s.W.Header().Set("Access-Control-Allow-Origin", "*")
	s.W.WriteHeader(http.StatusOK)
}

// Write commits headers if needed, writes p, and flushes.
func (s *SSEWriter) Write(p []byte) {
	s.Start()
	s.W.Write(p)
	if s.Flusher != nil {
		s.Flusher.Flush()
	}
}

// EmitError reports an error after headers were committed: the status line is
// no longer available, so an Anthropic-style in-band error event is sent.
func (s *SSEWriter) EmitError(msg string) {
	d, _ := json.Marshal(map[string]any{
		"type":  "error",
		"error": map[string]any{"type": "api_error", "message": msg},
	})
	s.Write([]byte("event: error\ndata: " + string(d) + "\n\n"))
}
