package tenant

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Elagoht/collage/pkg/collage"
)

// retryAfter is what a 503 asks a client to wait, in seconds: about how long a
// resolver's backing store takes to come back from a blip.
const retryAfter = "30"

// middleware resolves each request's host and declares the tenant as a cache
// dimension, so a cached page is kept per tenant.
func (p *Plugin) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The dimension's value comes from the host, never from a client: drop a
		// header of its name, so nothing downstream can mistake one for it.
		r.Header.Del(varyHeader)
		host := normalize(r.Host)
		if p.bypass[host] {
			next.ServeHTTP(w, r)
			return
		}
		t, ok, err := p.resolve(r.Context(), host)
		if err != nil {
			p.log.Error("tenant: the host could not be resolved", "host", host, "error", err)
			w.Header().Set("Retry-After", retryAfter)
			p.host.ServeStatus(w, r, http.StatusServiceUnavailable)
			return
		}
		if !ok {
			p.host.ServeStatus(w, r, http.StatusNotFound)
			return
		}
		if err := collage.Vary(r, varyHeader, t.ID); err != nil {
			p.log.Error("tenant: the tenant could not be declared", "host", host, "error", err)
			p.host.ServeStatus(w, r, http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// resolve returns host's tenant: from Tenants, from the cache, or from Resolve,
// whose answer is cached — "no tenant" included, an error not. host is
// normalized.
func (p *Plugin) resolve(ctx context.Context, host string) (Tenant, bool, error) {
	if host == "" {
		return Tenant{}, false, nil
	}
	if t, ok := p.static[host]; ok {
		return t, true, nil
	}
	if p.opts.Resolve == nil {
		return Tenant{}, false, nil
	}
	now := time.Now()
	if t, known, found := p.cache.get(host, now); found {
		return t, known, nil
	}
	t, known, err := p.call(ctx, host)
	if err != nil {
		return Tenant{}, false, err
	}
	if known {
		origin, err := parseOrigin(t.Origin)
		if t.ID == "" || err != nil {
			return Tenant{}, false, fmt.Errorf("%w: Resolve answered %q with %+v", ErrInvalidTenant, host, t)
		}
		t.Origin = origin
	}
	p.cache.put(host, t, known, now)
	return t, known, nil
}

// call runs Resolve, turning a panic into an error, so a resolver's bug is a 503,
// not a dropped connection.
func (p *Plugin) call(ctx context.Context, host string) (t Tenant, known bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil { // a recovered value is any by the language definition
			err = fmt.Errorf("tenant: Resolve panicked: %v", recovered)
		}
	}()
	return p.opts.Resolve(ctx, host)
}

// ID returns the tenant the request rc renders for, and whether there is one. It
// is safe in a cached page: the tenant is part of the cache key.
func ID(rc *collage.RenderContext) (string, bool) {
	id, ok := collage.Varied(rc, varyHeader)
	return id, ok && id != ""
}

// IDFromContext is ID for code that holds only a context: a page's StaticParams,
// so each tenant's sitemap lists its own URLs; a document handler; an action.
func IDFromContext(ctx context.Context) (string, bool) {
	id, ok := collage.VariedContext(ctx, varyHeader)
	return id, ok && id != ""
}
