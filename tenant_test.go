package tenant_test

import (
	"errors"
	"testing"
	"testing/fstest"
	"time"

	tenant "github.com/Elagoht/collage-tenant"
	"github.com/Elagoht/collage/pkg/collage"
)

// newApp builds the app and starts it: plugin Init runs at Start, not in New.
func newApp(p *tenant.Plugin) (*collage.App, error) {
	app, err := collage.New(&collage.Config{
		BaseURL:  "https://app.example",
		Server:   collage.ServerConfig{Host: "localhost", Port: 3000},
		Template: collage.TemplateConfig{FS: fstest.MapFS{"t/p.html": {Data: []byte(`<p>{{.}}</p>`)}}, Root: "t"},
		Cache:    collage.CacheConfig{Enabled: true, Type: "memory", DefaultTTL: time.Hour},
		Plugins:  []collage.Plugin{p},
	})
	if err != nil {
		return nil, err
	}
	return app, app.Start()
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
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := newApp(tenant.NewWith(c.opts)); !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestInit_OneIDManyHosts(t *testing.T) {
	_, err := newApp(tenant.NewWith(tenant.Options{Tenants: []tenant.Static{
		{ID: "a", Origin: "https://a.example", Hosts: []string{"a.test"}},
		{ID: "a", Origin: "https://a.example/", Hosts: []string{"a-custom.test"}},
	}}))
	if err != nil {
		t.Errorf("one ID with one origin over two entries = %v, want nil", err)
	}
}

func TestDuration_JSON(t *testing.T) {
	var d tenant.Duration
	if err := d.UnmarshalJSON([]byte(`"90s"`)); err != nil || time.Duration(d) != 90*time.Second {
		t.Errorf("\"90s\" = %v, %v", time.Duration(d), err)
	}
	if err := d.UnmarshalJSON([]byte(`1000`)); err != nil || time.Duration(d) != time.Microsecond {
		t.Errorf("1000 = %v, %v", time.Duration(d), err)
	}
	if err := d.UnmarshalJSON([]byte(`"soon"`)); err == nil {
		t.Errorf("\"soon\" parsed")
	}
}
