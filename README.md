# elagoht/tenant

A collage plugin that serves one site to many customers, each on a host of its
own: `acme.app.com`, `globex.app.com`, or a customer's own domain. Every absolute
URL the site writes follows the host, and a cached page is kept per tenant, so
acme's copy of a page is never globex's. Data you cache yourself with
`collage.Cached` is not kept per tenant: see [In your data](#in-your-data).

```sh
go get github.com/Elagoht/collage-tenant
```

```go
app, err := collage.New(&collage.Config{
	Plugins: []collage.Plugin{tenant.NewWith(tenant.Options{
		Tenants: []tenant.Static{
			{ID: "acme", Origin: "https://acme.app.com", Hosts: []string{"acme.app.com", "acme.localhost"}},
		},
		Resolve: lookupCustomDomain, // or a database, for hosts added at run time
	})},
})
```

Requires collage v0.42.0 or later: it adds the origin hook this plugin implements
and `collage.BaseURL(rc)`.

## How a request is answered

- A host listed under `Tenants` is that tenant's. Any other host goes to `Resolve`
  when there is one.
- A host in `bypass` is served without a tenant: the product's own site on
  `app.com`, say.
- A host that is no tenant's is answered `404`, with the site's own 404 page.
- A resolver that returns an error, or panics, is answered `503` with a
  `Retry-After`, and the failure is not cached. The error is logged by the plugin.
- A resolver that returns a tenant whose origin is not a bare origin naming a
  host (`https://:8080` names none) is answered `503` the same way.
- `Resolve`'s answers are cached for `ttl`, "is no tenant" included, so a client
  sending random `Host` headers cannot make the resolver run without end.
- `X-Collage-Tenant` is the cache dimension a tenant is declared under. It is no
  header a client can use: one a client sends is deleted, on a bypass host too.

Hosts are compared ASCII-lower-cased and without their port, so `ACME.test:8080`
is `acme.test`.

## Options

| Option | JSON | Default | |
| --- | --- | --- | --- |
| `Tenants` | `tenants` | | Tenants known from configuration: `id`, `origin` (a bare origin, `https://acme.app.com`), `hosts` (at least one) |
| `Resolve` | not configurable | | `func(ctx, host) (Tenant, bool, error)`: looks up a host `Tenants` does not list. Go only |
| `Bypass` | `bypass` | | Hosts served without a tenant |
| `TTL` | `ttl` | `"1m"` | How long `Resolve`'s answer for a host is kept |
| `MaxHosts` | `maxHosts` | `10000` | How many hosts' answers are kept |

Either `Tenants` or `Resolve` is required. A tenant without an ID or hosts, an
origin that is not a bare `http` or `https` origin naming a host, a host listed
twice, one ID with two origins, a bypass host that is empty (`""`, `"."`), or a
host both bypassed and a tenant's stops the application from starting. Durations are written as Go writes them, `"1m"` or `"30s"`.

```json
{
  "elagoht/tenant": {
    "tenants": [
      { "id": "acme", "origin": "https://acme.app.com", "hosts": ["acme.app.com"] }
    ],
    "bypass": ["app.com"],
    "ttl": "1m"
  }
}
```

## In your data

```go
id, ok := tenant.ID(rc)             // a data handler
id, ok = tenant.IDFromContext(ctx)  // a page's StaticParams, a document handler, an action
t, ok := plug.Lookup(id)            // the tenant's Origin, from Tenants or a recent Resolve
```

Both read a cache dimension, so they are safe in a cached page. `ok` is false on a
bypass host. `collage.BaseURL(rc)` is the tenant's origin.

The page cache is kept per tenant; `collage.Cached` is not. Its store is one per
process, keyed only by the key you give it, so `Cached(rc, "posts", …)` fetches
acme's posts once and hands them to globex too. Put the tenant in the key, and in
the tags, so invalidating one tenant's data leaves the others' alone:

```go
func posts(ctx context.Context, rc *collage.RenderContext) ([]Post, []string, error) {
	id, _ := tenant.ID(rc)
	list, err := collage.Cached(rc, "posts:"+id, time.Hour, []string{"posts:" + id},
		func(ctx context.Context) ([]Post, error) { return db.Posts(ctx, id) })
	return list, nil, err
}
```

The same holds for anything else you keep across requests yourself: a map, an
API client's own cache.

## When the resolver fails mid-render

A page's absolute URLs are named during its render, which asks for the host's
origin again. If `ttl` ran out between the request's lookup and the render's, and
`Resolve` fails now, the render keeps the host's last known origin, however old,
and the error is logged. A host with no answer held, one dropped under
`maxHosts`, say, has none to keep, and collage's origin hook has no way to report
an error: that render
falls back to `Config.BaseURL`, and a cacheable page keeps it until it is
invalidated. So does a host whose tenant `Resolve` now says is gone. A `ttl`
longer than your slowest render makes either rare.

## What follows the host

With collage v0.42.0 and these plugins at v0.2.0, the absolute URLs they write are
each tenant's own, and invalidations are sent per host:

| Plugin | Follows the host in |
| --- | --- |
| `elagoht/sitemap` | the URLs in each tenant's sitemap |
| `elagoht/feed` | feed links |
| `elagoht/meta` | canonical and `og:` URLs |
| `elagoht/ogimage` | share-card URLs |
| `elagoht/indexnow` | the URLs it submits |
| `elagoht/cdnpurge` | the URLs it purges |
| `elagoht/robots` | the `Sitemap:` line |

## Development

`acme.localhost` resolves to the loopback address in a browser, so a host listed
for it works through `collage dev` with no hosts file:
`http://acme.localhost:8080/`.

## Static builds

A static build has no host, so every page is rendered without a tenant, absolute
against `Config.BaseURL`. The build says so with the warning `tenant/no-host`.
