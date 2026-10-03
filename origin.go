package tenant

import (
	"context"

	"github.com/Elagoht/collage/pkg/collage"
)

// Origin is the tenant's origin for host, which collage.BaseURL and the plugins
// writing absolute URLs follow. A bypassed host, and one that is no tenant's,
// is not known here, so it has Config.BaseURL's.
func (p *Plugin) Origin(ctx context.Context, host string) (string, bool) {
	host = normalize(host)
	if p.bypass[host] {
		return "", false
	}
	t, ok, err := p.resolve(ctx, host)
	if err != nil || !ok {
		return "", false
	}
	return t.Origin, true
}

// Lookup returns the tenant with id, from Tenants or from a host Resolve has
// answered for recently.
func (p *Plugin) Lookup(id string) (Tenant, bool) {
	for _, t := range p.static {
		if t.ID == id {
			return t, true
		}
	}
	if p.cache == nil {
		return Tenant{}, false
	}
	return p.cache.byID(id)
}

// OnBuildFinished says what a static build cannot do: it has no host, so every
// page was rendered without a tenant, absolute against Config.BaseURL.
func (p *Plugin) OnBuildFinished(_ context.Context, ev *collage.BuildFinishedEvent) error {
	ev.Warn("", "tenant/no-host", "a static build has no host: every page was rendered without a tenant, absolute against Config.BaseURL")
	return nil
}
