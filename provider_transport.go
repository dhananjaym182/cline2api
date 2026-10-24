package main

import (
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"cline-go-proxy/internal/transport"
)

// providerTransportCache keeps one transport per normalized egress spec so a
// provider override does not construct a fresh connection pool per request.
var providerTransportCache sync.Map // spec -> http.RoundTripper

// providerTransportFor resolves a provider's egress override:
//   - "" inherits the global transport (including test overrides),
//   - "direct"/"none" bypasses both the app proxy pool and environment proxies,
//   - a proxy URL dials through only that proxy.
func providerTransportFor(spec string) (http.RoundTripper, error) {
	norm, err := transport.NormalizeProxySpec(spec)
	if err != nil {
		return nil, err
	}
	if norm == "" {
		return httpClient.Transport, nil
	}
	if cached, ok := providerTransportCache.Load(norm); ok {
		return cached.(http.RoundTripper), nil
	}
	tr := isolatedTransport(norm)
	actual, _ := providerTransportCache.LoadOrStore(norm, http.RoundTripper(tr))
	return actual.(http.RoundTripper), nil
}

// isolatedTransport builds a transport that does not consult the Cline egress
// pool. A direct provider must also ignore HTTPS_PROXY, which the global
// transport's Proxy hook intentionally honors when the pool is empty.
func isolatedTransport(spec string) *http.Transport {
	var tr *http.Transport
	if base, ok := httpClient.Transport.(*http.Transport); ok {
		tr = base.Clone()
	} else {
		tr = &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 5 * time.Minute,
		}
	}
	dialer := &net.Dialer{Timeout: 12 * time.Second, KeepAlive: 30 * time.Second}
	tr.Proxy = nil
	tr.DialContext = dialer.DialContext
	tr.DialTLSContext = nil
	if spec != "direct" {
		if u, err := url.Parse(spec); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return tr
}
