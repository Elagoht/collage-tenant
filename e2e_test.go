package tenant_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	indexnow "github.com/Elagoht/collage-indexnow"
	meta "github.com/Elagoht/collage-meta"
	sitemap "github.com/Elagoht/collage-sitemap"
	tenant "github.com/Elagoht/collage-tenant"
	"github.com/Elagoht/collage/pkg/collage"
	"github.com/Elagoht/collage/pkg/collagetest"
)

// fakeIndexNow records the hosts it is sent submissions for.
type fakeIndexNow struct {
	*httptest.Server
	mu      sync.Mutex
	hosts   []string
	arrived chan struct{}
}

func newFakeIndexNow(t *testing.T) *fakeIndexNow {
	f := &fakeIndexNow{arrived: make(chan struct{}, 10)}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var s struct {
			Host string `json:"host"`
		}
		_ = json.NewDecoder(r.Body).Decode(&s)
		f.mu.Lock()
		f.hosts = append(f.hosts, s.Host)
		f.mu.Unlock()
		f.arrived <- struct{}{}
	}))
	t.Cleanup(f.Close)
	return f
}

// Two tenants with different posts: each sitemap lists its own under its own
// origin, each page's canonical is on its own origin, and an invalidation tells
// IndexNow once per origin.
func TestEndToEnd(t *testing.T) {
	f := newFakeIndexNow(t)
	app, err := collage.New(&collage.Config{
		Server: collage.ServerConfig{Host: "localhost", Port: 3000},
		Template: collage.TemplateConfig{FS: fstest.MapFS{
			"t/layout.html": {Data: []byte(`<html><head>{{hoist "head"}}</head><body>{{slot "content"}}</body></html>`)},
			"t/p.html":      {Data: []byte(`<main>post</main>`)},
		}, Root: "t"},
		Cache:  collage.CacheConfig{Enabled: true, Type: "memory", DefaultTTL: time.Hour},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Plugins: []collage.Plugin{
			tenant.NewWith(tenant.Options{Tenants: two}),
			sitemap.New(sitemap.Options{}),
			meta.New(meta.Options{}),
			indexnow.New(indexnow.Options{Key: "k1234567", Endpoint: f.URL, Window: 50 * time.Millisecond, Backoff: time.Millisecond}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	slugs := map[string][]string{"acme": {"a1", "a2"}, "globex": {"g1"}}
	post := collage.NewPage("post").
		WithLayouts(collage.NewFragment("layout", "layout.html").Build()).
		WithContent(collage.NewFragment("post", "p.html").Build()).
		WithPath("en", "/posts/{slug}").
		WithStaticParams(func(ctx context.Context, _ string) ([]map[string]string, error) {
			id, _ := tenant.IDFromContext(ctx)
			var out []map[string]string
			for _, s := range slugs[id] {
				out = append(out, map[string]string{"slug": s})
			}
			return out, nil
		}).
		Static().WithDependency("posts").Build()
	if err := app.RegisterPage(post); err != nil {
		t.Fatal(err)
	}
	c := collagetest.New(t, app.Handler())

	acme := c.Get("http://acme.test/sitemap.xml").WantStatus(http.StatusOK).Body
	if !strings.Contains(acme, "<loc>https://acme.example/posts/a1</loc>") ||
		!strings.Contains(acme, "<loc>https://acme.example/posts/a2</loc>") || strings.Contains(acme, "g1") {
		t.Errorf("acme sitemap = %s", acme)
	}
	globex := c.Get("http://globex.test/sitemap.xml").WantStatus(http.StatusOK).Body
	if !strings.Contains(globex, "<loc>https://globex.example/posts/g1</loc>") || strings.Contains(globex, "a1") {
		t.Errorf("globex sitemap = %s", globex)
	}
	page := c.Get("http://acme.test/posts/a1").WantStatus(http.StatusOK).Body
	if want := `<link rel="canonical" href="https://acme.example/posts/a1">`; !strings.Contains(page, want) {
		t.Errorf("acme post = %s, want %s", page, want)
	}
	c.Get("http://globex.test/posts/g1").WantStatus(http.StatusOK)

	if err := app.InvalidateTags(context.Background(), "posts"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-f.arrived:
		case <-time.After(5 * time.Second):
			t.Fatalf("IndexNow got %v, want two submissions", f.hosts)
		}
	}
	f.mu.Lock()
	hosts := append([]string(nil), f.hosts...)
	f.mu.Unlock()
	sort.Strings(hosts)
	if strings.Join(hosts, ",") != "acme.example,globex.example" {
		t.Errorf("IndexNow hosts = %v, want acme.example and globex.example", hosts)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = app.Shutdown(ctx)
}
