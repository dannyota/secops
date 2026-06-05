package mirror

import (
	"context"
	"encoding/json"
	"sort"

	"danny.vn/secops/internal/mirror/reconcile"
	"danny.vn/secops/soar/legacy"
)

// allRecordsSelector asks a paged settings read (GetEnvironments/GetNetworkDetails)
// for every record in one shot. SOAR settings collections are small, so a single
// large page is sufficient; a multi-page read can be added if a tenant exceeds it.
var allRecordsSelector = map[string]any{"searchTerm": "", "requestedPage": 0, "pageSize": 10000}

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
	{"environments", environmentsSurface},
	{"networks", networksSurface},
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

// environmentsSurface: SOAR environments (segregation units) as config-as-code.
// GetEnvironments is a paged read that wraps records in {metadata, objectsList};
// decodeRawList unwraps objectsList (metadata is an object, not an array). Each
// record carries a top-level integer id + name. AddOrUpdateEnvironmentRecords is
// a single-record upsert keyed by id. Deletion is NOT exposed — removing an
// environment orphans every case/alert scoped to it (high blast radius), so the
// surface is additive (NoDelete); drop an environment via the UI if ever needed.
// base64Image (an icon blob) is stripped from the diff. NOTE: read shape is
// validated live; the write path is guarded (dry-run default) but verify a
// dry-run plan before --yes.
func environmentsSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name:       "environments",
		dir:        DirSOAREnvironments,
		product:    reconcile.ProductSOAR,
		idField:    "id",
		nameField:  "name",
		extraStrip: []string{"base64Image"},
		caps:       reconcile.Capabilities{NoDelete: true},
		list: func(ctx context.Context) (json.RawMessage, error) {
			return lc.GetEnvironments(ctx, allRecordsSelector)
		},
		create: lc.AddOrUpdateEnvironmentRecords,
		update: lc.AddOrUpdateEnvironmentRecords,
	})
}

// networksSurface: named networks/CIDRs (internal/external scoping, enrichment)
// as config-as-code. Same paged {metadata, objectsList} read shape as
// environments; records carry id + name + address. AddOrUpdateNetworkDetailsRecords
// is a single-record upsert keyed by id. Additive (NoDelete) for now — wire the
// by-id DeleteNetwork as a prune path only after a live write smoke confirms it.
func networksSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name:      "networks",
		dir:       DirSOARNetworks,
		product:   reconcile.ProductSOAR,
		idField:   "id",
		nameField: "name",
		caps:      reconcile.Capabilities{NoDelete: true},
		list: func(ctx context.Context) (json.RawMessage, error) {
			return lc.GetNetworkDetails(ctx, allRecordsSelector)
		},
		create: lc.AddOrUpdateNetworkDetailsRecords,
		update: lc.AddOrUpdateNetworkDetailsRecords,
	})
}
