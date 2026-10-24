package main

import (
	"net/http"
	"testing"
)

type roundTripperStub struct{ calls int }

func (s *roundTripperStub) RoundTrip(*http.Request) (*http.Response, error) {
	s.calls++
	return nil, nil
}

func TestProviderTransportForInheritsGlobal(t *testing.T) {
	old := httpClient.Transport
	t.Cleanup(func() { httpClient.Transport = old })

	probe := &roundTripperStub{}
	httpClient.Transport = probe

	tr, err := providerTransportFor("")
	if err != nil {
		t.Fatal(err)
	}
	if tr != http.RoundTripper(probe) {
		t.Fatal("empty spec must inherit the global transport exactly")
	}
}

func TestProviderTransportForDirectAndProxy(t *testing.T) {
	tr, err := providerTransportFor("none")
	if err != nil {
		t.Fatal(err)
	}
	direct, ok := tr.(*http.Transport)
	if !ok {
		t.Fatalf("direct transport expected, got %T", tr)
	}
	if direct.Proxy != nil {
		t.Fatal("direct must bypass environment proxies")
	}

	tr2, err := providerTransportFor("http://127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	proxied, ok := tr2.(*http.Transport)
	if !ok || proxied.Proxy == nil {
		t.Fatalf("proxy transport expected, got %T", tr2)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://example.test/v1", nil)
	u, err := proxied.Proxy(req)
	if err != nil || u == nil || u.Host != "127.0.0.1:8080" {
		t.Fatalf("proxy func = %v, err=%v", u, err)
	}

	// Same spec must reuse the same transport instance (connection pooling).
	tr3, err := providerTransportFor("direct")
	if err != nil {
		t.Fatal(err)
	}
	if tr3 != tr {
		t.Fatal("normalized specs should share a cached transport")
	}
}

func TestProviderTransportForRejectsInvalidSpec(t *testing.T) {
	if _, err := providerTransportFor("ftp://example.test"); err == nil {
		t.Fatal("invalid proxy scheme must be rejected")
	}
}
