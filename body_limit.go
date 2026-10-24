package main

// Client request body cap (CLIProxyAPI-inspired): ProxyConfig.
// MaxRequestBodyBytes bounds how much a chat/completions client may upload.
// 0 (default) disables the limit. MaxBytesReader makes oversized reads fail;
// handlers surface that as their normal 400 decode error.

import (
	"net/http"
)

func withBodyLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if lim := getProxyConfig().MaxRequestBodyBytes; lim > 0 && r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, lim)
		}
		next(w, r)
	}
}
