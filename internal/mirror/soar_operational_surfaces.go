package mirror

import (
	"context"
	"encoding/json"

	"danny.vn/secops/internal/mirror/reconcile"
	"danny.vn/secops/soar/legacy"
)

// Operational-config SOAR surfaces on the reliable legacy AppKey path:
// connectors (ingestion sources) and jobs (scheduled automation) — the config the
// modern v1alpha pull+patch covered only partially — plus form-dynamic-parameters
// (close-case form fields). They ride the same reconcile engine + jsonSurface
// adapter as the settings surfaces in registry_soar.go.

// connectorsSurface: connector instances (ingestion sources) as config-as-code,
// full CUD keyed by `identifier`. SaveConnector is the upsert for BOTH create and
// update: the create path triggers when the body has NO `identifier` (the server
// assigns one) — sending a client-assigned identifier routes to the update path
// (which 404s for an id that doesn't exist yet). A new connector file naturally
// omits `identifier` (and the reserved `_server` block is stripped), so the engine
// create works; the operator must supply the connector's mandatory parameters.
// GetConnector returns the full instance (secret parameter values arrive
// server-masked as "***…", which the server reads as "keep existing" on save, so
// the whole-body overlay round-trips them unchanged); DeleteConnector is a clean
// by-id delete (PruneEligible). extraStrip drops the definition-version/runtime
// fields the full body carries (`version`/`isUpdateAvailable`/
// `loggingEnabledUntilUnixMs`/`isCustom`) so a pull→push round-trips clean
// (`version` is also stripped from the nested `integration` object at any depth).
func connectorsSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "connectors", dir: DirSOARConnectors,
		product:    reconcile.ProductSOAR,
		idField:    "identifier",
		nameField:  "displayName",
		extraStrip: []string{"version", "isUpdateAvailable", "loggingEnabledUntilUnixMs", "isCustom"},
		caps:       reconcile.Capabilities{WholeBodyWrite: true, PruneEligible: true},
		list:       flattenedConnectorCards(lc),
		getOne:     lc.GetConnector,
		create:     lc.SaveConnector,
		update:     lc.SaveConnector,
		del:        lc.DeleteConnector,
	})
}

// flattenedConnectorCards adapts ListConnectorCards, whose response groups the
// cards by integration ([{integration, cards:[...]}, …]), into the flat
// connector-card list the engine expects. It tolerates a flat response too (an
// item that is itself a card), so it works regardless of grouping.
func flattenedConnectorCards(lc *legacy.Client) rawListFn {
	return func(ctx context.Context) (json.RawMessage, error) {
		raw, err := lc.ListConnectorCards(ctx)
		if err != nil {
			return nil, err
		}
		groups, err := decodeRawList(raw)
		if err != nil {
			return nil, err
		}
		var cards []json.RawMessage
		for _, g := range groups {
			var gm struct {
				Cards []json.RawMessage `json:"cards"`
			}
			if json.Unmarshal(g, &gm) == nil && len(gm.Cards) > 0 {
				cards = append(cards, gm.Cards...)
				continue
			}
			if jsonField(g, "identifier") != "" { // already a flat card
				cards = append(cards, g)
			}
		}
		return json.Marshal(cards)
	}
}

// jobsSurface: installed jobs (scheduled background automation) as config-as-code.
// The installed-jobs list item IS the full write body (read DTO == write DTO), so
// there is no getOne; SaveOrUpdateJob is a whole-body upsert and re-sending the
// full read body updates the job in place. Delete takes a body (DeleteJobData),
// not a clean id, so the surface is additive (NoDelete) — remove a job via
// `soar legacy call jobs/DeleteJobData`. Engine Create is NOT wired: a create must
// send a TRIMMED template body (echoing the read-only/audit fields the list item
// carries is rejected), which the whole-body adapter cannot do. extraStrip drops
// the run-state/version fields the server stamps so a pull→push round-trips clean.
func jobsSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "jobs", dir: DirSOARJobs,
		product:    reconcile.ProductSOAR,
		idField:    "uniqueIdentifier",
		nameField:  "name",
		extraStrip: []string{"lastRunStatus", "lastRunTime", "version", "creator"},
		caps:       reconcile.Capabilities{WholeBodyWrite: true, NoDelete: true},
		list:       lc.ListInstalledJobs,
		update:     lc.SaveOrUpdateJob,
	})
}

// NOTE: form-dynamic-parameters (close-case form fields) was investigated as a
// reconcile surface but DEFERRED — its strict PUT update silently resets the
// parameter's formType to Invalid (dropping it out of its form) even when given the
// integer-enum body the UI uses, so a reconcile update is not safe. The surface is
// reachable read-only via `soar legacy call settings/form-dynamic-parameters?formType=CloseCase`.
