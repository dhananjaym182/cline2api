package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, map[string]any{"ok": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["ok"] != true {
		t.Fatalf("body = %v", got)
	}
}

func TestWriteAPIEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteAPI(rec, http.StatusBadRequest, Response{Error: "bad"})
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["success"] != false || got["error"] != "bad" {
		t.Fatalf("body = %v", got)
	}
	if _, ok := got["data"]; ok {
		t.Fatalf("empty data should be omitted: %v", got)
	}

	rec = httptest.NewRecorder()
	WriteAPI(rec, http.StatusOK, Response{Success: true, Data: map[string]any{"x": 1}})
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["success"] != true || got["data"] == nil {
		t.Fatalf("body = %v", got)
	}
}

func TestCORS(t *testing.T) {
	called := false
	h := CORS(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodOptions, "/v1/models", nil))
	if rec.Code != http.StatusNoContent || called {
		t.Fatalf("preflight: status=%d called=%v", rec.Code, called)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("headers = %v", rec.Header())
	}

	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("get: status=%d called=%v", rec.Code, called)
	}
}
