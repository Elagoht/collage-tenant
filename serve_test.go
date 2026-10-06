package tenant_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	tenant "github.com/Elagoht/collage-tenant"
	"github.com/Elagoht/collage/pkg/collage"
	"github.com/Elagoht/collage/pkg/collagetest"
)

// serving builds a site with an Incremental page and an Incremental document,
// each writing the tenant it sees and the origin it is absolute against.
func serving(t *testing.T, opts tenant.Options) *collagetest.Client {
	t.Helper()
	app, err := collage.New(&collage.Config{
		BaseURL:  "https://app.example",
		Server:   collage.ServerConfig{Host: "localhost", Port: 3000},
		Template: collage.TemplateConfig{FS: fstest.MapFS{"t/p.html": {Data: []byte(`<p>{{.}}</p>`)}}, Root: "t"},
		Cache:    collage.CacheConfig{Enabled: true, Type: "memory", DefaultTTL: time.Hour},
		Plugins:  []collage.Plugin{tenant.NewWith(opts)},
	})
	if err != nil {
		t.Fatal(err)
	}
	content := collage.NewFragment("p", "p.html").WithData(collage.DataHandler(
		func(_ context.Context, rc *collage.RenderContext) (string, []string, error) {
			id, _ := tenant.ID(rc)
			return id + "@" + collage.BaseURL(rc), nil, nil
		})).Build()
	if err := app.RegisterPage(collage.NewPage("home").WithContent(content).WithPath("en", "/").Incremental(time.Hour).Build()); err != nil {
		t.Fatal(err)
	}
	doc := collage.NewDocument("d", "text/plain").WithPath("en", "/d.txt").
		WithHandler(func(ctx context.Context, rc *collage.RenderContext) ([]byte, []string, error) {
			id, _ := tenant.IDFromContext(ctx)
			return []byte(id + "@" + collage.BaseURL(rc)), nil, nil
		}).Incremental(time.Hour).Build()
	if err := app.RegisterDocument(doc); err != nil {
		t.Fatal(err)
	}
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	return collagetest.New(t, app.Handler())
}

var two = []tenant.Static{
	{ID: "acme", Origin: "https://acme.example", Hosts: []string{"acme.test"}},
	{ID: "globex", Origin: "https://globex.example", Hosts: []string{"globex.test"}},
}

func TestIsolation(t *testing.T) {
	c := serving(t, tenant.Options{Tenants: two})
	for range 2 { // the second round is cache hits
		for url, want := range map[string]string{
			"http://acme.test/":        "<p>acme@https://acme.example</p>",
			"http://globex.test/":      "<p>globex@https://globex.example</p>",
			"http://ACME.test:80/":     "<p>acme@https://acme.example</p>",
			"http://acme.test/d.txt":   "acme@https://acme.example",
			"http://globex.test/d.txt": "globex@https://globex.example",
		} {
			if got := c.Get(url).WantStatus(http.StatusOK).Body; !strings.Contains(got, want) {
				t.Errorf("GET %s = %q, want %q", url, got, want)
			}
		}
	}
}

func TestUnknownAndBypass(t *testing.T) {
	c := serving(t, tenant.Options{Tenants: two, Bypass: []string{"app.test"}})
	c.Get("http://nobody.test/").WantStatus(http.StatusNotFound)
	if got := c.Get("http://app.test/").WantStatus(http.StatusOK).Body; !strings.Contains(got, "<p>@https://app.example</p>") {
		t.Errorf("bypass host = %q, want no tenant and Config.BaseURL", got)
	}
}

func TestStaticBeforeResolve(t *testing.T) {
	var asked atomic.Int32
	c := serving(t, tenant.Options{Tenants: two, Resolve: func(_ context.Context, host string) (tenant.Tenant, bool, error) {
		asked.Add(1)
		if host == "custom.test" {
			return tenant.Tenant{ID: "initech", Origin: "https://initech.example/"}, true, nil
		}
		return tenant.Tenant{}, false, nil
	}})
	c.Get("http://acme.test/").WantStatus(http.StatusOK)
	if asked.Load() != 0 {
		t.Errorf("Resolve asked for a static host")
	}
	if got := c.Get("http://custom.test/").Body; !strings.Contains(got, "<p>initech@https://initech.example</p>") {
		t.Errorf("resolved host = %q", got)
	}
	c.Get("http://nobody.test/").WantStatus(http.StatusNotFound)
	c.Get("http://nobody.test/").WantStatus(http.StatusNotFound)
	if n := asked.Load(); n != 2 {
		t.Errorf("Resolve asked %d times, want 2 (custom once, nobody once: negatives are cached)", n)
	}
}

func TestResolverFailures(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	c := serving(t, tenant.Options{Resolve: func(_ context.Context, host string) (tenant.Tenant, bool, error) {
		calls.Add(1)
		switch {
		case host == "panic.test":
			panic("boom")
		case host == "bad.test":
			return tenant.Tenant{ID: "bad", Origin: "not-an-origin"}, true, nil
		case host == "nohost.test":
			return tenant.Tenant{ID: "nohost", Origin: "https://:8080"}, true, nil
		case fail.Load():
			return tenant.Tenant{}, false, errors.New("database down")
		}
		return tenant.Tenant{ID: "late", Origin: "https://late.example"}, true, nil
	}})
	res := c.Get("http://late.test/").WantStatus(http.StatusServiceUnavailable)
	if res.Header.Get("Retry-After") == "" {
		t.Errorf("503 without Retry-After")
	}
	fail.Store(false)
	if got := c.Get("http://late.test/").WantStatus(http.StatusOK).Body; !strings.Contains(got, "late@") {
		t.Errorf("after recovery = %q: the error was cached", got)
	}
	c.Get("http://panic.test/").WantStatus(http.StatusServiceUnavailable)
	c.Get("http://bad.test/").WantStatus(http.StatusServiceUnavailable)
	c.Get("http://nohost.test/").WantStatus(http.StatusServiceUnavailable)
}

// The resolver fails between the middleware's lookup and the render's: the TTL
// ran out while the page rendered. The render keeps the tenant's last known
// origin rather than falling back to Config.BaseURL, which would be cached for
// the tenant.
func TestResolverBlipDuringRender(t *testing.T) {
	var calls atomic.Int32
	app, err := collage.New(&collage.Config{
		BaseURL:  "https://app.example",
		Server:   collage.ServerConfig{Host: "localhost", Port: 3000},
		Template: collage.TemplateConfig{FS: fstest.MapFS{"t/p.html": {Data: []byte(`<p>{{.}}</p>`)}}, Root: "t"},
		Cache:    collage.CacheConfig{Enabled: true, Type: "memory", DefaultTTL: time.Hour},
		Plugins: []collage.Plugin{tenant.NewWith(tenant.Options{
			TTL: tenant.Duration(5 * time.Millisecond),
			Resolve: func(context.Context, string) (tenant.Tenant, bool, error) {
				if calls.Add(1) == 2 {
					return tenant.Tenant{}, false, errors.New("database blip")
				}
				return tenant.Tenant{ID: "acme", Origin: "https://acme.example"}, true, nil
			},
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	content := collage.NewFragment("p", "p.html").WithData(collage.DataHandler(
		func(_ context.Context, rc *collage.RenderContext) (string, []string, error) {
			time.Sleep(20 * time.Millisecond) // past the TTL
			return collage.BaseURL(rc), nil, nil
		})).Build()
	if err := app.RegisterPage(collage.NewPage("home").WithContent(content).WithPath("en", "/").Incremental(time.Hour).Build()); err != nil {
		t.Fatal(err)
	}
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	c := collagetest.New(t, app.Handler())
	for range 2 { // the second is the cached copy
		if got := c.Get("http://acme.test/").WantStatus(http.StatusOK).Body; !strings.Contains(got, "<p>https://acme.example</p>") {
			t.Errorf("GET acme.test = %q, want acme's origin", got)
		}
	}
	if n := calls.Load(); n < 2 {
		t.Fatalf("Resolve called %d times; the render did not re-resolve, so the blip was not exercised", n)
	}
}

func TestSpoofedHeaderInert(t *testing.T) {
	c := serving(t, tenant.Options{Tenants: two, Bypass: []string{"app.test"}})
	for _, url := range []string{"http://acme.test/", "http://app.test/"} {
		req := c.Request(http.MethodGet, url, nil)
		req.Header.Set("X-Collage-Tenant", "globex")
		body := c.Do(req).Body
		if strings.Contains(body, "globex") {
			t.Errorf("%s with a spoofed header = %q", url, body)
		}
	}
}
