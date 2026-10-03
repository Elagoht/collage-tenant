# elagoht/tenant

A collage plugin that serves one site to many customers, each on a host of its
own: `acme.app.com`, `globex.app.com`, or a customer's own domain. Every absolute
URL the site writes follows the host, and a cached page is kept per tenant, so
acme's copy is never globex's.

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
origin that is not a bare `http` or `https` origin, a host listed twice, one ID
with two origins, or a host both bypassed and a tenant's stops the application
from starting. Durations are written as Go writes them, `"1m"` or `"30s"`.

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
