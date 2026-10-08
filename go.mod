// Module collage-tenant serves one collage site to many customers, each on a
// host of its own, with every absolute URL following the host.
module github.com/Elagoht/collage-tenant

go 1.26

require github.com/Elagoht/collage v0.53.0

require (
	github.com/Elagoht/collage-indexnow v0.2.1
	github.com/Elagoht/collage-meta v0.2.2
	github.com/Elagoht/collage-sitemap v0.2.1
)
