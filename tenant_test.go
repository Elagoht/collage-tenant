package tenant_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	tenant "github.com/Elagoht/collage-tenant"
	"github.com/Elagoht/collage/pkg/collage"
	"github.com/Elagoht/collage/pkg/collagetest"
)

// siteOptions is the test site: BaseURL app.example, a memory cache, the
// template p.html, no home page, and plugins (tenant among them).
func siteOptions(plugins ...collage.Plugin) []collagetest.SiteOption {
	return []collagetest.SiteOption{
		collagetest.WithoutHome(),
		collagetest.WithPlugins(plugins...),
		collagetest.WithTemplates(map[string]string{"p.html": `<p>{{.}}</p>`}),
		collagetest.WithConfig(func(c *collage.Config) {
			c.BaseURL = "https://app.example"
			c.Cache = collage.CacheConfig{Enabled: true, Type: "memory", DefaultTTL: time.Hour}
		}),
	}
}

// newApp builds the app and starts it: plugin Init runs at Start, not in New.
func newApp(t *testing.T, p *tenant.Plugin, more ...collagetest.SiteOption) (*collage.App, error) {
	t.Helper()
	site, err := collagetest.TrySite(t, append(siteOptions(p), more...)...)
	if err != nil {
		return nil, err
	}
	return site.App, nil
}

func TestInit_Misconfigurations(t *testing.T) {
	cases := map[string]struct {
		opts tenant.Options
		want error
	}{
		"nothing":         {tenant.Options{}, tenant.ErrNoTenants},
		"no id":           {tenant.Options{Tenants: []tenant.Static{{Origin: "https://a.example", Hosts: []string{"a.test"}}}}, tenant.ErrInvalidTenant},
		"bad origin":      {tenant.Options{Tenants: []tenant.Static{{ID: "a", Origin: "a.example", Hosts: []string{"a.test"}}}}, tenant.ErrInvalidTenant},
		"no hosts":        {tenant.Options{Tenants: []tenant.Static{{ID: "a", Origin: "https://a.example"}}}, tenant.ErrInvalidTenant},
		"host twice":      {tenant.Options{Tenants: []tenant.Static{{ID: "a", Origin: "https://a.example", Hosts: []string{"x.test"}}, {ID: "b", Origin: "https://b.example", Hosts: []string{"X.test:443"}}}}, tenant.ErrDuplicateHost},
		"id two origins":  {tenant.Options{Tenants: []tenant.Static{{ID: "a", Origin: "https://a.example", Hosts: []string{"a.test"}}, {ID: "a", Origin: "https://other.example", Hosts: []string{"a2.test"}}}}, tenant.ErrDuplicateID},
		"bypass conflict": {tenant.Options{Tenants: []tenant.Static{{ID: "a", Origin: "https://a.example", Hosts: []string{"a.test"}}}, Bypass: []string{"A.test"}}, tenant.ErrBypassConflict},
		"origin no host":  {tenant.Options{Tenants: []tenant.Static{{ID: "a", Origin: "https://:8080", Hosts: []string{"a.test"}}}}, tenant.ErrInvalidTenant},
		"empty bypass":    {tenant.Options{Tenants: []tenant.Static{{ID: "a", Origin: "https://a.example", Hosts: []string{"a.test"}}}, Bypass: []string{""}}, tenant.ErrInvalidTenant},
		"negative ttl":    {tenant.Options{Tenants: two, TTL: -1}, tenant.ErrInvalidConfig},
		"negative hosts":  {tenant.Options{Tenants: two, MaxHosts: -1}, tenant.ErrInvalidConfig},
		"dot bypass":      {tenant.Options{Tenants: []tenant.Static{{ID: "a", Origin: "https://a.example", Hosts: []string{"a.test"}}}, Bypass: []string{"."}}, tenant.ErrInvalidTenant},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := newApp(t, tenant.New(c.opts)); !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestInit_OneIDManyHosts(t *testing.T) {
	_, err := newApp(t, tenant.New(tenant.Options{Tenants: []tenant.Static{
		{ID: "a", Origin: "https://a.example", Hosts: []string{"a.test"}},
		{ID: "a", Origin: "https://a.example/", Hosts: []string{"a-custom.test"}},
	}}))
	if err != nil {
		t.Errorf("one ID with one origin over two entries = %v, want nil", err)
	}
}

// What plugins-config.json may not say: a duration as a number (60 was 60ns, so
// every answer expired at once), a negative maxHosts.
func TestConfigurationRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		raw  string
		want func(error) bool
	}{
		"number ttl":   {`{"ttl": 60}`, func(err error) bool { return strings.Contains(err.Error(), `"ttl"`) }},
		"negative ttl": {`{"ttl": "-1m"}`, func(err error) bool { return strings.Contains(err.Error(), `"ttl"`) }},
		"negative maxHosts": {`{"maxHosts": -1}`, func(err error) bool {
			return errors.Is(err, tenant.ErrInvalidConfig) && strings.Contains(err.Error(), "maxHosts")
		}},
	} {
		_, err := newApp(t, tenant.New(tenant.Options{Tenants: two}), collagetest.WithPluginConfig(tenant.Name, c.raw))
		if err == nil || !c.want(err) {
			t.Errorf("%s: start = %v", name, err)
		}
	}
	if _, err := newApp(t, tenant.New(tenant.Options{Tenants: two}), collagetest.WithPluginConfig(tenant.Name, `{"ttl": "90s", "maxHosts": 5}`)); err != nil {
		t.Errorf("a valid configuration: %v", err)
	}
}

// The configuration is decoded over New's Options; the slices it decodes into
// must not be the caller's.
func TestNewCopies(t *testing.T) {
	mine := []tenant.Static{{ID: "a", Origin: "https://a.example", Hosts: []string{"a.test"}}}
	bypass := []string{"x.test"}
	_, err := newApp(t, tenant.New(tenant.Options{Tenants: mine, Bypass: bypass}),
		collagetest.WithPluginConfig(tenant.Name, `{"tenants":[{"id":"b","origin":"https://b.example","hosts":["b.test"]}],"bypass":["y.test"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if mine[0].ID != "a" || mine[0].Origin != "https://a.example" || len(mine[0].Hosts) != 1 || mine[0].Hosts[0] != "a.test" {
		t.Errorf("the caller's Tenants were written: %+v", mine)
	}
	if bypass[0] != "x.test" {
		t.Errorf("the caller's Bypass was written: %v", bypass)
	}
}
