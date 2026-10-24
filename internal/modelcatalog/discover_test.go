package modelcatalog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseModelsShapes(t *testing.T) {
	body := []byte(`{
		"data": [{"id":" a/model ","name":"Alpha","pricing":{"prompt":"0","completion":0,"request":"0"},
		          "tags":["FREE"],"free":"true","mode":"free"},
		         {"id":"a/model","name":"duplicate"}],
		"models": [{"id":"b/model","display_name":"Beta (Free)"}]
	}`)
	entries, err := ParseModels(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	a := entries[0]
	if a.ID != "a/model" || a.Name != "Alpha" || !a.FreeFlag || a.Mode != "free" {
		t.Errorf("entry a = %+v", a)
	}
	if a.PricingPrompt == nil || *a.PricingPrompt != 0 || a.PricingRequest == nil || *a.PricingRequest != 0 {
		t.Errorf("pricing not parsed: %+v", a)
	}
	if len(a.Tags) != 1 || a.Tags[0] != "FREE" {
		t.Errorf("tags = %v", a.Tags)
	}
	if entries[1].ID != "b/model" || entries[1].Name != "Beta (Free)" {
		t.Errorf("entry b = %+v", entries[1])
	}
}

func TestParseModelsErrors(t *testing.T) {
	if _, err := ParseModels([]byte("not json")); err == nil || !strings.Contains(err.Error(), "unexpected response format") {
		t.Errorf("err = %v", err)
	}
	if _, err := ParseModels([]byte(`{"data":[]}`)); err == nil || !strings.Contains(err.Error(), "no models") {
		t.Errorf("err = %v", err)
	}
}

func TestDiscoverSendsAuthAndParses(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		if r.Header.Get("X-Custom") != "yes" {
			t.Errorf("custom header missing")
		}
		w.Write([]byte(`{"data":[{"id":"z"},{"id":"a"}]}`))
	}))
	defer srv.Close()

	entries, err := Discover(srv.Client(), srv.URL, "key-1", map[string]string{"X-Custom": "yes"}, "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if gotAuth != "Bearer key-1" {
		t.Errorf("auth = %q", gotAuth)
	}
	if len(entries) != 2 || entries[0].ID != "a" || entries[1].ID != "z" {
		t.Errorf("entries not sorted: %+v", entries)
	}
}

func TestDiscoverRetriesV1On404(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[{"id":"m"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	entries, err := Discover(srv.Client(), srv.URL, "", nil, "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "m" {
		t.Errorf("entries = %+v", entries)
	}
	if len(paths) != 2 || paths[0] != "/models" || paths[1] != "/v1/models" {
		t.Errorf("paths = %v", paths)
	}
}

func TestDiscoverFallsBackToFetcherBase(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/catalog/models" {
			w.Write([]byte(`{"models":[{"id":"catalog/model"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	entries, err := Discover(srv.Client(), srv.URL, "", nil, srv.URL+"/catalog")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "catalog/model" {
		t.Errorf("entries = %+v", entries)
	}
	if len(paths) < 3 {
		t.Errorf("expected primary attempts then fetcher fallback, paths = %v", paths)
	}
}

func TestDiscoverErrors(t *testing.T) {
	if _, err := Discover(http.DefaultClient, "", "", nil, ""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty base err = %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, strings.Repeat("x", 500), http.StatusForbidden)
	}))
	defer srv.Close()
	_, err := Discover(srv.Client(), srv.URL, "", nil, "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("err = %v", err)
	}
	if len(err.Error()) > 260 {
		t.Errorf("error body not truncated: %d chars", len(err.Error()))
	}
}

func TestFetcherBaseFor(t *testing.T) {
	if got := FetcherBaseFor("https://opencode.ai/"); got != "https://opencode.ai/zen/v1" {
		t.Errorf("opencode fetcher base = %q", got)
	}
	if got := FetcherBaseFor("https://unknown.example/v1"); got != "" {
		t.Errorf("unknown fetcher base = %q", got)
	}
}
