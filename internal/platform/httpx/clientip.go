package httpx

import (
	"net"
	"net/http"
	"strings"
)

const maxIPLen = 64

// ClientIP returns the first X-Forwarded-For address when it parses, else the
// host of RemoteAddr. It is a secondary signal: the account limit is the one
// that matters. The header is client-controlled unless a trusted proxy
// overwrites it, so the value can be spoofed; what Railway does with
// X-Forwarded-For is still to be confirmed.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		first = strings.TrimSpace(first)
		if ip := net.ParseIP(first); ip != nil && len(first) <= maxIPLen {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if len(host) > maxIPLen {
		host = host[:maxIPLen]
	}
	return host
}
