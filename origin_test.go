package tenant_test

import (
	"context"
	"testing"

	tenant "github.com/Elagoht/collage-tenant"
	"github.com/Elagoht/collage/pkg/collage"
)

var (
	_ collage.OriginResolver    = (*tenant.Plugin)(nil)
	_ collage.BuildFinishedHook = (*tenant.Plugin)(nil)
)

func TestOriginAndLookup(t *testing.T) {
	p := tenant.NewWith(tenant.Options{Tenants: two, Bypass: []string{"app.test"}, Resolve: func(_ context.Context, host string) (tenant.Tenant, bool, error) {
		if host == "custom.test" {
			return tenant.Tenant{ID: "initech", Origin: "https://initech.example"}, true, nil
		}
		return tenant.Tenant{}, false, nil
	}})
	app, err := newApp(p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for host, want := range map[string]string{
		"acme.test": "https://acme.example", "custom.test": "https://initech.example",
		"app.test": "https://app.example", "nobody.test": "https://app.example",
	} {
		if got := app.OriginFor(ctx, host); got != want {
			t.Errorf("OriginFor(%s) = %q, want %q", host, got, want)
		}
	}
	if tn, ok := p.Lookup("acme"); !ok || tn.Origin != "https://acme.example" {
		t.Errorf("Lookup(acme) = %+v, %v", tn, ok)
	}
	if tn, ok := p.Lookup("initech"); !ok || tn.Origin != "https://initech.example" {
		t.Errorf("Lookup(initech) after a resolve = %+v, %v", tn, ok)
	}
	if _, ok := p.Lookup("nobody"); ok {
		t.Errorf("Lookup(nobody) found")
	}
}

func TestBuildWarns(t *testing.T) {
	app, err := newApp(tenant.NewWith(tenant.Options{Tenants: two}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := collage.NewBuilder(app, collage.BuildOptions{OutDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	report, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Rule == "tenant/no-host" {
			return
		}
	}
	t.Errorf("findings %+v, want tenant/no-host", report.Findings)
}
