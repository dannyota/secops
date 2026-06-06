package mirror

import (
	"context"
	"encoding/json"
	"fmt"
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
	{"tracking-lists", trackingListsSurface},
	{"soc-roles", socRolesSurface},
	{"idp", idpSurface},
	{"visual-families", visualFamiliesSurface},
	{"sla-definitions", slaDefinitionsSurface},
	{"case-stages", caseStagesSurface},
	{"case-tags", caseTagsSurface},
	{"close-root-causes", closeRootCausesSurface},
	{"blacklists", blacklistsSurface},
	{"playbook-categories", playbookCategoriesSurface},
	{"playbooks", playbooksSurface},
	// Wave-7 operational config (defined in soar_operational_surfaces.go).
	{"connectors", connectorsSurface},
	{"jobs", jobsSurface},
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

// The surfaces below were spec'd by a swagger-grounded workflow and the method
// signatures verified by hand. All are ADDITIVE (NoDelete) for now: their deletes
// take a body selector (not a clean id) or are RBAC/SSO-sensitive, and the write
// path is not yet smoke-validated — pull + diff is the safe value; the guarded
// write awaits a smoke. idField "id" is stripped from the diff and
// carried in _server; the live record's id flows back on update via DeepMerge.

// trackingListsSurface: tracked entities (flat array; id + entityIdentifier).
func trackingListsSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "tracking-lists", dir: DirSOARTrackingLists,
		product: reconcile.ProductSOAR,
		idField: "id", nameField: "entityIdentifier",
		caps:   reconcile.Capabilities{NoDelete: true},
		list:   lc.GetTrackingListRecords,
		create: lc.AddOrUpdateTrackingListRecords,
		update: lc.AddOrUpdateTrackingListRecords,
	})
}

// socRolesSurface: SOC role definitions (RBAC; flat array; id + name). SocRoleGet
// takes an int id (not the string getOne shape), so list records are used as-is.
func socRolesSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "soc-roles", dir: DirSOARSocRoles,
		product: reconcile.ProductSOAR,
		idField: "id", nameField: "name",
		caps:   reconcile.Capabilities{NoDelete: true},
		list:   lc.SocRoleList,
		create: lc.SocRoleAddOrUpdate,
		update: lc.SocRoleAddOrUpdate,
	})
}

// idpSurface: IdP group→role mappings (SSO; wrapped {metadata, objectsList}; UUID
// id + idpGroup name). UpdateIdpGroupMapping takes (id, body), so the update is a
// thin closure that pulls the id back out of the merged body.
func idpSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "idp", dir: DirSOARIdp,
		product: reconcile.ProductSOAR,
		idField: "id", nameField: "idpGroup",
		caps:   reconcile.Capabilities{NoDelete: true},
		list:   lc.ListIdpGroupMappings,
		getOne: lc.GetIdpGroupMapping,
		create: lc.CreateIdpGroupMapping,
		update: func(ctx context.Context, body any) (json.RawMessage, error) {
			return lc.UpdateIdpGroupMapping(ctx, bodyStr(body, "id"), body)
		},
	})
}

// visualFamiliesSurface: ontology visual families (flat array; id + family). The
// write API expects the record wrapped as {visualFamilyDataModel: record}; the
// icon blob (imageBase64) is stripped from the diff. DeleteFamilyData is a clean
// by-id delete on a custom family (which affects no detection or real entity), so
// the surface is PruneEligible.
func visualFamiliesSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "visual-families", dir: DirSOARVisualFams,
		product: reconcile.ProductSOAR,
		idField: "id", nameField: "family",
		extraStrip: []string{"imageBase64"},
		wrapKey:    "visualFamilyDataModel",
		caps:       reconcile.Capabilities{PruneEligible: true},
		list:       lc.ListVisualFamilies,
		create:     lc.AddOrUpdateVisualFamily,
		update:     lc.AddOrUpdateVisualFamily,
		del:        lc.DeleteFamilyData,
	})
}

// The following case/playbook config surfaces were spec'd by a swagger-grounded
// workflow (each confirmed a SINGLE-record upsert via the request body schema, not
// a batch array) and the signatures verified by hand. All are NoDelete (their
// removes take a body, not a clean id) — additive. case-stages / case-tags need a
// paged selector on the list read; their responses wrap records in objectsList,
// which decodeRawList unwraps.

// slaDefinitionsSurface: SLA definitions (flat array; id + value).
func slaDefinitionsSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "sla-definitions", dir: DirSOARSla,
		product: reconcile.ProductSOAR,
		idField: "id", nameField: "value",
		caps:   reconcile.Capabilities{NoDelete: true},
		list:   lc.GetSlaDefinitionsRecords,
		create: lc.AddSlaDefinitionsRecord,
		update: lc.AddSlaDefinitionsRecord,
	})
}

// caseStagesSurface: case stage definitions (wrapped objectsList; id + name).
func caseStagesSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "case-stages", dir: DirSOARCaseStages,
		product: reconcile.ProductSOAR,
		idField: "id", nameField: "name",
		caps: reconcile.Capabilities{NoDelete: true},
		list: func(ctx context.Context) (json.RawMessage, error) {
			return lc.GetCaseStageDefinitionRecords(ctx, allRecordsSelector)
		},
		create: lc.AddCaseStageDefinitionRecord,
		update: lc.AddCaseStageDefinitionRecord,
	})
}

// caseTagsSurface: case tag definitions (wrapped objectsList; id + name).
func caseTagsSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "case-tags", dir: DirSOARCaseTags,
		product: reconcile.ProductSOAR,
		idField: "id", nameField: "name",
		caps: reconcile.Capabilities{NoDelete: true},
		list: func(ctx context.Context) (json.RawMessage, error) {
			return lc.GetTagDefinitionsRecords(ctx, allRecordsSelector)
		},
		create: lc.AddTagDefinitionsRecords,
		update: lc.AddTagDefinitionsRecords,
	})
}

// closeRootCausesSurface: case close root-causes (flat array; id + rootCause). Each
// record's `forCloseReason` is the `legacy.CloseReason` enum (Malicious=0, …) it is
// offered under; it stays in the canonical so the reason→root-cause link round-trips.
func closeRootCausesSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "close-root-causes", dir: DirSOARRootCauses,
		product: reconcile.ProductSOAR,
		idField: "id", nameField: "rootCause",
		caps:   reconcile.Capabilities{NoDelete: true},
		list:   lc.GetRootCauseCloseRecords,
		create: lc.AddOrUpdateRootCauseClose,
		update: lc.AddOrUpdateRootCauseClose,
	})
}

// blacklistsSurface: model block-list entries (flat array; id + entityIdentifier).
func blacklistsSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "blacklists", dir: DirSOARBlacklists,
		product: reconcile.ProductSOAR,
		idField: "id", nameField: "entityIdentifier",
		caps:   reconcile.Capabilities{NoDelete: true},
		list:   lc.GetAllModelBlockRecords,
		create: lc.AddOrUpdateModelBlockRecords,
		update: lc.AddOrUpdateModelBlockRecords,
	})
}

// playbookCategoriesSurface: playbook (workflow) categories (flat array; id + name).
func playbookCategoriesSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "playbook-categories", dir: DirSOARPlaybookCats,
		product: reconcile.ProductSOAR,
		idField: "id", nameField: "name",
		caps:   reconcile.Capabilities{NoDelete: true},
		list:   lc.ListWorkflowCategories,
		create: lc.AddOrUpdatePlaybookCategory,
		update: lc.AddOrUpdatePlaybookCategory,
	})
}

// bodyStr extracts a string field from a decoded JSON body (used to thread an id
// from a merged body into an SDK method that takes id as a separate parameter).
func bodyStr(body any, key string) string {
	m, ok := body.(map[string]any)
	if !ok {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case json.Number:
		return v.String()
	}
	return ""
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
// is a single-record upsert keyed by id. PruneEligible: DeleteNetwork is a clean
// by-id delete (the record id is the DELETE path identifier) and a named
// network/CIDR is low-blast enrichment data (removing one drops that scoping
// entry, orphaning no cases).
func networksSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name:      "networks",
		dir:       DirSOARNetworks,
		product:   reconcile.ProductSOAR,
		idField:   "id",
		nameField: "name",
		caps:      reconcile.Capabilities{PruneEligible: true},
		list: func(ctx context.Context) (json.RawMessage, error) {
			return lc.GetNetworkDetails(ctx, allRecordsSelector)
		},
		create: lc.AddOrUpdateNetworkDetailsRecords,
		update: lc.AddOrUpdateNetworkDetailsRecords,
		del:    lc.DeleteNetwork,
	})
}
