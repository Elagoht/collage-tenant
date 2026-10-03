package tenant

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"ACME.test": "acme.test", "acme.test:8080": "acme.test", "acme.test.": "acme.test",
		"[::1]:3000": "::1", "": "", "çay.test": "çay.test", "K.test": "k.test",
	} {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
	// The Kelvin sign (U+212A) is not folded to k: ASCII-only.
	if got := normalize("K.test"); got != "K.test" {
		t.Errorf("normalize(Kelvin) = %q, folded", got)
	}
}

func TestHostCache_TTLAndNegatives(t *testing.T) {
	c := newHostCache(10, time.Minute)
	now := time.Unix(0, 0)
	c.put("a.test", Tenant{ID: "a"}, true, now)
	c.put("nobody.test", Tenant{}, false, now)
	if tn, known, found := c.get("a.test", now.Add(59*time.Second)); !found || !known || tn.ID != "a" {
		t.Errorf("a.test before TTL = %v, %v, %v", tn, known, found)
	}
	if _, known, found := c.get("nobody.test", now); !found || known {
		t.Errorf("negative = known %v, found %v; want found, not known", known, found)
	}
	if _, _, found := c.get("a.test", now.Add(time.Minute+time.Second)); found {
		t.Errorf("a.test after TTL still found")
	}
	if tn, ok := c.byID("a"); !ok || tn.ID != "a" {
		t.Errorf("byID(a) = %v, %v", tn, ok)
	}
}

func TestHostCache_Bounded(t *testing.T) {
	c := newHostCache(100, time.Hour)
	now := time.Unix(0, 0)
	for i := range 50000 {
		c.put("h"+strconv.Itoa(i)+".test", Tenant{}, false, now)
	}
	if n := c.len(); n > 100 {
		t.Errorf("len = %d after 50000 hosts, want <= 100", n)
	}
	if _, _, found := c.get("h49999.test", now); !found {
		t.Errorf("the newest host was evicted")
	}
	if _, _, found := c.get("h0.test", now); found {
		t.Errorf("the oldest host survived")
	}
	// Refreshing a host already cached does not grow the order.
	c.put("h49999.test", Tenant{}, false, now)
	if n := c.len(); n > 100 {
		t.Errorf("len = %d after a refresh", n)
	}
}

// Origin, past the TTL with Resolve failing, answers the host's last known
// tenant's origin, and logs the error; a stale "no tenant" is not reused.
func TestOrigin_StaleIfError(t *testing.T) {
	var logs bytes.Buffer
	p := &Plugin{
		opts: Options{Resolve: func(context.Context, string) (Tenant, bool, error) {
			return Tenant{}, false, errors.New("database down")
		}},
		log:    slog.New(slog.NewTextHandler(&logs, nil)),
		static: map[string]Tenant{},
		bypass: map[string]bool{},
		cache:  newHostCache(10, time.Minute),
	}
	past := time.Now().Add(-2 * time.Minute)
	p.cache.put("acme.test", Tenant{ID: "acme", Origin: "https://acme.example"}, true, past)
	p.cache.put("nobody.test", Tenant{}, false, past)

	if origin, ok := p.Origin(context.Background(), "acme.test"); !ok || origin != "https://acme.example" {
		t.Errorf("Origin(acme.test) = %q, %v; want the last known https://acme.example", origin, ok)
	}
	if !strings.Contains(logs.String(), "database down") {
		t.Errorf("the error was not logged: %q", logs.String())
	}
	if origin, ok := p.Origin(context.Background(), "nobody.test"); ok {
		t.Errorf("Origin(nobody.test) = %q, known; a stale negative must not answer", origin)
	}
	if origin, ok := p.Origin(context.Background(), "new.test"); ok {
		t.Errorf("Origin(new.test) = %q, known; nothing was ever resolved for it", origin)
	}
	// The middleware still sees the failure: get keeps its meaning.
	if _, _, found := p.cache.get("acme.test", time.Now()); found {
		t.Errorf("get returned an expired entry")
	}
}
