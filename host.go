package tenant

import (
	"net"
	"net/http"
	"strings"
	"time"
)

// normalize is host as the plugin keys it: ASCII lower-cased, without a port or a
// trailing dot. An internationalized name is kept as it is spelt; no punycode
// conversion. Lower-casing is ASCII-only on purpose: Unicode folding maps
// distinct names together ("K" and the Kelvin sign).
func normalize(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	b := []byte(host)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// Temporary stubs; C2 and C3 replace them.
type hostCache struct{}

func newHostCache(int, time.Duration) *hostCache { return &hostCache{} }

func (p *Plugin) middleware(next http.Handler) http.Handler { return next }
