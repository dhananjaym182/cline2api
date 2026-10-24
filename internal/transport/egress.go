// Package transport owns outbound HTTP transport configuration and validation.
package transport

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateProxyList checks proxy URLs: http, https, socks5, and socks5h are
// accepted and each entry must include an explicit host:port. Empty lines are
// ignored.
func ValidateProxyList(proxies []string) error {
	for _, p := range proxies {
		line := strings.TrimSpace(p)
		if line == "" {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			return fmt.Errorf("proxy %q invalid: %v", line, err)
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return fmt.Errorf("proxy %q: unsupported scheme (http/https/socks5/socks5h)", line)
		}
		if u.Host == "" {
			return fmt.Errorf("proxy %q: missing host:port", line)
		}
		if _, _, err := net.SplitHostPort(u.Host); err != nil {
			return fmt.Errorf("proxy %q: missing port", line)
		}
	}
	return nil
}
