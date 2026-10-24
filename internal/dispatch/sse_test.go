package dispatch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (f *flushRecorder) Flush() { f.flushes++ }

func TestSSEWriterStartIsIdempotent(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := NewSSEWriter(rec)
	sw.Start()
	sw.Start()
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type = %q", got)
	}
	if !sw.Started {
		t.Fatal("Started not set")
	}
}

func TestSSEWriterWriteFlushes(t *testing.T) {
	fr := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	sw := NewSSEWriter(fr)
	sw.Write([]byte("data: x\n\n"))
	if fr.flushes != 1 {
		t.Fatalf("flushes = %d, want 1", fr.flushes)
	}
	if got := fr.Body.String(); got != "data: x\n\n" {
		t.Fatalf("body = %q", got)
	}
	if !sw.Started {
		t.Fatal("write did not start")
	}
}

func TestSSEWriterWithoutFlusher(t *testing.T) {
	w := &noFlushWriter{header: http.Header{}}
	sw := NewSSEWriter(w)
	sw.Write([]byte("ok"))
	if sw.Flusher != nil {
		t.Fatal("expected nil flusher")
	}
	if string(w.body) != "ok" {
		t.Fatalf("body = %q", w.body)
	}
}

func TestSSEWriterEmitError(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := NewSSEWriter(rec)
	sw.EmitError("boom")
	body := rec.Body.String()
	if !strings.HasPrefix(body, "event: error\ndata: ") || !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("body = %q", body)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(body, "event: error\ndata: "), "\n\n")
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["type"] != "error" {
		t.Fatalf("type = %v", obj["type"])
	}
	inner, _ := obj["error"].(map[string]any)
	if inner["message"] != "boom" {
		t.Fatalf("message = %v", inner["message"])
	}
}

type noFlushWriter struct {
	header http.Header
	body   []byte
	code   int
}

func (w *noFlushWriter) Header() http.Header { return w.header }
func (w *noFlushWriter) Write(p []byte) (int, error) {
	w.body = append(w.body, p...)
	return len(p), nil
}
func (w *noFlushWriter) WriteHeader(code int) { w.code = code }
