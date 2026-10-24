package main

// Passthrough response headers (CLIProxyAPI-inspired): optionally forward a
// curated set of upstream response headers (rate limits, request IDs,
// retry hints) to the client. Gated by ProxyConfig.PassthroughHeaders, off by
// default so response shape stays identical unless opted in. Hop-by-hop,
// framing, identity, and any credential-ish headers are never copied.

import (
	"net/http"
	"strings"
)

// passthroughHeaderCap bounds how many headers one upstream response may add.
const passthroughHeaderCap = 32

// blockedPassthroughHeaders are framing/transport headers Go or the client
// must own, plus anything that could carry credentials or session state.
var blockedPassthroughHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"content-length":      {},
	"content-type":        {},
	"content-encoding":    {},
	"content-language":    {},
	"date":                {},
	"server":              {},
	"set-cookie":          {},
	"cookie":              {},
	"authorization":       {},
	"www-authenticate":    {},
	"vary":                {},
	// CORS is owned by this proxy (see httpx.CORS); upstream CORS must not
	// collide with it.
	"access-control-allow-origin":      {},
	"access-control-allow-credentials": {},
	"access-control-allow-headers":     {},
	"access-control-allow-methods":     {},
	"access-control-expose-headers":    {},
	"access-control-max-age":           {},
	"access-control-request-headers":   {},
	"access-control-request-method":    {},
}

func blockedPassthroughHeader(name string) bool {
	lower := strings.ToLower(name)
	if _, ok := blockedPassthroughHeaders[lower]; ok {
		return true
	}
	// Belt and braces: never forward anything that looks like a credential.
	return strings.Contains(lower, "api-key") ||
		strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret")
}

// copyPassthroughHeaders merges safe upstream response headers into the
// client-facing header map. Must run before WriteHeader on every path. When
// the writer serializes concurrent writes (the nonstream keepalive wrapper),
// the merge runs under that lock so a background keepalive tick can never
// race the header-map mutation.
func copyPassthroughHeaders(w http.ResponseWriter, upstream http.Header) {
	if !getProxyConfig().PassthroughHeaders || upstream == nil {
		return
	}
	if s, ok := w.(interface{ WithLock(func()) }); ok {
		s.WithLock(func() { mergePassthroughHeaders(w, upstream) })
		return
	}
	mergePassthroughHeaders(w, upstream)
}

func mergePassthroughHeaders(w http.ResponseWriter, upstream http.Header) {
	copied := 0
	for name, values := range upstream {
		if blockedPassthroughHeader(name) {
			continue
		}
		for _, v := range values {
			if copied >= passthroughHeaderCap {
				return
			}
			if len(v) > 512 {
				continue
			}
			w.Header().Add(name, v)
			copied++
		}
	}
}
