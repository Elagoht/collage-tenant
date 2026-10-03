package tenant

import (
	"strconv"
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
