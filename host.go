package tenant

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Elagoht/collage/pkg/collage"
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

// parseOrigin is collage.ParseOrigin, held to one more rule: the origin names a
// host. "https://:8080" passes ParseOrigin, and collage ignores it as a
// resolver's answer.
func parseOrigin(raw string) (string, error) {
	origin, err := collage.ParseOrigin(raw)
	if err != nil {
		return "", err
	}
	if u, err := url.Parse(origin); err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("tenant: origin %q names no host", raw)
	}
	return origin, nil
}

// hostCache keeps Resolve's answers per host, positive and negative, for a TTL,
// and at most max hosts of them, the oldest dropped first. An expired answer is
// kept until it is replaced or dropped, for getStale.
type hostCache struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration
	items map[string]cached
	order []string // hosts in items, oldest first
}

type cached struct {
	tenant  Tenant
	known   bool
	expires time.Time
}

func newHostCache(max int, ttl time.Duration) *hostCache {
	return &hostCache{max: max, ttl: ttl, items: make(map[string]cached)}
}

// get returns host's answer and whether one is held and still fresh.
func (c *hostCache) get(host string, now time.Time) (Tenant, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[host]
	if !ok || !now.Before(e.expires) {
		return Tenant{}, false, false
	}
	return e.tenant, e.known, true
}

// getStale returns host's last positive answer, expired or not: what Origin
// falls back on when Resolve fails. A held "no tenant" is not returned.
func (c *hostCache) getStale(host string) (Tenant, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[host]
	if !ok || !e.known {
		return Tenant{}, false
	}
	return e.tenant, true
}

// put records host's answer, dropping the oldest hosts beyond max.
func (c *hostCache) put(host string, t Tenant, known bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, held := c.items[host]; !held {
		for len(c.order) >= c.max {
			delete(c.items, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, host)
	}
	c.items[host] = cached{tenant: t, known: known, expires: now.Add(c.ttl)}
}

// byID finds a tenant some cached host resolved to.
func (c *hostCache) byID(id string) (Tenant, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.items {
		if e.known && e.tenant.ID == id {
			return e.tenant, true
		}
	}
	return Tenant{}, false
}

func (c *hostCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}
