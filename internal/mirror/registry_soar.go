package mirror

import (
	"sort"

	"danny.vn/secops/internal/mirror/reconcile"
	"danny.vn/secops/soar/legacy"
)

// SOAR Lane-1 config surfaces: clean per-object CUD endpoints exposed as
// config-as-code through the reconcile engine. The fan-out adds entries here —
// each is one jsonSurfaceSpec, no bespoke puller. Batch upserts (AddOrUpdate*),
// export/import bundles, and selector-only reads do NOT belong here; they are
// served by the raw `soar legacy call` passthrough and (case actions) the
// imperative command layer.
type soarSurfaceDef struct {
	name  string
	build func(*legacy.Client) reconcile.Surface
}

var soarSurfaceDefs = []soarSurfaceDef{
	{"webhooks", webhooksSurface},
}

// SOARSurfaceNames returns the engine-backed SOAR surface names, sorted.
func SOARSurfaceNames() []string {
	names := make([]string, 0, len(soarSurfaceDefs))
	for _, d := range soarSurfaceDefs {
		names = append(names, d.name)
	}
	sort.Strings(names)
	return names
}

// BuildSOARSurface constructs the named engine surface bound to lc, reporting
// whether the name is a known engine surface.
func BuildSOARSurface(name string, lc *legacy.Client) (reconcile.Surface, bool) {
	for _, d := range soarSurfaceDefs {
		if d.name == name {
			return d.build(lc), true
		}
	}
	return reconcile.Surface{}, false
}

// webhooksSurface: inbound webhook endpoints that feed connectors. Clean
// per-object CUD by string identifier; UpdateWebhook is a whole-body PUT, so
// edits overlay onto the live body (preserving the server-issued apiKey, which
// the pull redacts). Create may be refused by an environment resource limit —
// the engine surfaces that as a per-item FAIL without aborting the surface.
func webhooksSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name:      "webhooks",
		dir:       DirSOARWebhooks,
		product:   reconcile.ProductSOAR,
		idField:   "identifier",
		nameField: "name",
		caps:      reconcile.Capabilities{WholeBodyWrite: true, PruneEligible: true},
		list:      lc.ListWebhookCards,
		getOne:    lc.GetWebhook,
		create:    lc.CreateWebhook,
		update:    lc.UpdateWebhook,
		del:       lc.DeleteWebhook,
	})
}

// NOTE: a "connectors-legacy" reconcile surface was tried and removed — live
// validation showed ListConnectorCards returns a list GROUPED by integration
// ({integration, cards:[{identifier, displayName, ...}]}), not a flat per-object
// list, and GetConnector's shape differs from the card. A proper version needs a
// bespoke flattening List (over cards[]), not the generic jsonSurface; connectors
// are already managed by the modern tier (pull + patch), so this is deferred.
