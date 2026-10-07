// Package tenant is a collage plugin that serves one site to many customers, each
// on a host of its own — acme.app.com, globex.app.com, or a customer's own
// domain — with every absolute URL the site writes following the host.
//
//	app, err := collage.New(&collage.Config{
//		Plugins: []collage.Plugin{tenant.NewWith(tenant.Options{
//			Tenants: []tenant.Static{
//				{ID: "acme", Origin: "https://acme.app.com", Hosts: []string{"acme.app.com", "acme.localhost"}},
//			},
//			Resolve: lookupCustomDomain, // or a database, for hosts added at run time
//		})},
//	})
//
// A data handler reads the tenant with tenant.ID(rc); code that holds only a
// context — a page's StaticParams, so each tenant's sitemap lists its own URLs —
// with tenant.IDFromContext(ctx). Both read a cache dimension, so they are safe in
// a cached page: acme's copy is never globex's. collage.Cached is not kept per
// tenant: put tenant.ID(rc) in its key, with Key.With, and in its tags.
// collage.BaseURL(rc) is the tenant's origin, which elagoht/sitemap, feed, meta,
// ogimage, indexnow, cdnpurge and robots follow.
//
// A host that is no tenant is answered 404, with the site's own 404 page; a
// resolver that fails, 503. A static build has no host, so it renders without a
// tenant, and says so.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Elagoht/collage/pkg/collage"
)

// Name is the plugin's name, and the key its configuration is found under.
const Name = "elagoht/tenant"

// varyHeader is the cache dimension a tenant is declared under. It is not a
// header any client sends: the plugin deletes one a client does send.
const varyHeader = "X-Collage-Tenant"

// Tenant is one customer of the site.
type Tenant struct {
	// ID is the tenant's stable key: the cache dimension, and what the
	// application looks its own records up by.
	ID string `json:"id"`
	// Origin is where the tenant's site is, "https://acme.app.com": the origin
	// its absolute URLs are built on.
	Origin string `json:"origin"`
}

// Static is a tenant listed in configuration, with the hosts it is served on.
type Static struct {
	ID     string   `json:"id"`
	Origin string   `json:"origin"`
	Hosts  []string `json:"hosts"`
}

// Resolver maps a host — lower-cased, without its port — to a tenant. ok false
// means the host is no tenant's; an error means the answer is not known now, and
// the request is answered 503 rather than 404.
type Resolver func(ctx context.Context, host string) (t Tenant, ok bool, err error)

// Options configures the plugin.
type Options struct {
	// Tenants are the tenants known from configuration. They are looked up
	// before Resolve.
	Tenants []Static `json:"tenants"`
	// Resolve looks up a host Tenants does not list: a custom domain, a tenant
	// added at run time.
	Resolve Resolver `json:"-"`
	// Bypass are hosts served without a tenant: the product's own site, say,
	// on app.com.
	Bypass []string `json:"bypass"`
	// TTL is how long Resolve's answer for a host is kept, "is no tenant"
	// included. Default "1m".
	TTL Duration `json:"ttl"`
	// MaxHosts bounds how many hosts' answers are kept, so a client sending
	// random Host headers cannot grow memory or make Resolve run without end.
	// Default 10000.
	MaxHosts int `json:"maxHosts"`
}

var (
	// ErrNoTenants is returned by Init with neither Tenants nor Resolve.
	ErrNoTenants = errors.New("tenant: Tenants or Resolve is required")
	// ErrInvalidTenant is returned by Init for a tenant without an ID, without
	// hosts, or whose Origin is not a bare origin naming a host, and for a
	// Bypass entry that is empty once normalized ("", "."). A Resolve answer
	// with such an Origin fails the request with it, as a 503.
	ErrInvalidTenant = errors.New("tenant: invalid tenant")
	// ErrDuplicateHost is returned by Init for a host listed under two tenants.
	ErrDuplicateHost = errors.New("tenant: a host is listed twice")
	// ErrDuplicateID is returned by Init for one ID listed with two origins.
	ErrDuplicateID = errors.New("tenant: one ID has two origins")
	// ErrBypassConflict is returned by Init for a host both bypassed and a tenant's.
	ErrBypassConflict = errors.New("tenant: a host is both bypassed and a tenant's")
)

// Plugin resolves a request's host to a tenant.
type Plugin struct {
	opts   Options
	host   collage.Host
	log    *slog.Logger
	static map[string]Tenant // normalized host → tenant
	bypass map[string]bool
	cache  *hostCache
}

// New returns the plugin configured from the application's configuration alone.
func New() *Plugin { return NewWith(Options{}) }

// NewWith returns the plugin with opts as its starting point, which the
// application's configuration is then decoded over.
func NewWith(opts Options) *Plugin { return &Plugin{opts: opts} }

func (p *Plugin) Name() string                   { return Name }
func (p *Plugin) Version() string                { return "0.1.2" }
func (p *Plugin) Shutdown(context.Context) error { return nil }

// Init reads the configuration, checks the tenants, and adds the middleware that
// resolves each request's host.
func (p *Plugin) Init(_ context.Context, host collage.Host) error {
	opts, err := collage.PluginConfig(host, p.opts)
	if err != nil {
		return err
	}
	p.opts = opts
	if p.opts.TTL <= 0 {
		p.opts.TTL = Duration(time.Minute)
	}
	if p.opts.MaxHosts <= 0 {
		p.opts.MaxHosts = 10000
	}
	if len(p.opts.Tenants) == 0 && p.opts.Resolve == nil {
		return ErrNoTenants
	}
	p.host = host
	p.log = host.Logger()
	p.static = make(map[string]Tenant)
	origins := make(map[string]string) // ID → origin
	for _, s := range p.opts.Tenants {
		origin, err := parseOrigin(s.Origin)
		if s.ID == "" || len(s.Hosts) == 0 || err != nil {
			return fmt.Errorf("%w: %+v", ErrInvalidTenant, s)
		}
		if prev, seen := origins[s.ID]; seen && prev != origin {
			return fmt.Errorf("%w: %q has %q and %q", ErrDuplicateID, s.ID, prev, origin)
		}
		origins[s.ID] = origin
		for _, h := range s.Hosts {
			n := normalize(h)
			if n == "" {
				return fmt.Errorf("%w: an empty host under %q", ErrInvalidTenant, s.ID)
			}
			if _, dup := p.static[n]; dup {
				return fmt.Errorf("%w: %q", ErrDuplicateHost, n)
			}
			p.static[n] = Tenant{ID: s.ID, Origin: origin}
		}
	}
	p.bypass = make(map[string]bool)
	for _, h := range p.opts.Bypass {
		n := normalize(h)
		if n == "" {
			return fmt.Errorf("%w: an empty bypass host %q", ErrInvalidTenant, h)
		}
		if _, conflict := p.static[n]; conflict {
			return fmt.Errorf("%w: %q", ErrBypassConflict, n)
		}
		p.bypass[n] = true
	}
	p.cache = newHostCache(p.opts.MaxHosts, time.Duration(p.opts.TTL))
	return host.Use(p.middleware)
}
