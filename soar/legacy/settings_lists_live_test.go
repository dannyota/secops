package legacy

import (
	"context"
	"testing"
)

// GROUP C (low-med) — enrichment/list config CRUD. These affect entity
// enrichment / block logic, not security/access/live-cases. Each runs the full
// list -> create -> list -> read -> edit -> read -> delete -> list lifecycle on a
// throwaway record and is write-gated (SECOPS_SOAR_SMOKE_WRITE=1).

// TestLiveSettingsTrackingListCRUD — a throwaway tracking-list record (a tracked
// entity). Keyed by entityIdentifier; a record for a smoke-label entity matches
// nothing real. Template-based (the list is empty by default).
func TestLiveSettingsTrackingListCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind:     "tracking-list",
		list:     func(ctx context.Context) (RawJSON, error) { return lc.GetTrackingListRecords(ctx) },
		idOf:     intField("id"),
		nameOf:   strField("entityIdentifier"),
		rename:   setField("entityIdentifier"),
		template: func() map[string]any { return map[string]any{"category": "secopsctl-smoke", "environments": []any{}} },
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdateTrackingListRecords(ctx, o)
		},
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdateTrackingListRecords(ctx, o)
		},
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.RemoveTrackingListRecords(ctx, o)
		},
	})
}

// networkPageRequest is the paging body GetNetworkDetails expects.
func networkPageRequest() map[string]any {
	return map[string]any{"searchTerm": "", "requestedPage": 0, "pageSize": 1000}
}

// firstEnvironment returns one real environment name from the tenant (network
// records require a non-empty environments list). Falls back to the universal
// Siemplify default if the priority records don't expose a recognizable field.
func firstEnvironment(t *testing.T, ctx context.Context, lc *Client) string {
	raw, err := lc.GetEnvironmentPriorities(ctx)
	for _, o := range objects(t, "environment-priorities", raw, err) {
		for _, k := range []string{"environmentName", "environment", "name"} {
			if s := strField(k)(o); s != "" {
				return s
			}
		}
	}
	return "Default Environment"
}

// TestLiveSettingsNetworkCRUD — a throwaway network record. Uses the RFC 5737
// documentation/test range 192.0.2.0/24 so it can't affect real
// internal/external enrichment; named with the smoke label and attached to one
// real environment (the create requires a non-empty environments list). NOTE:
// the network DELETE endpoint takes an array, so remove wraps the object.
func TestLiveSettingsNetworkCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	env := firstEnvironment(t, ctx, lc)
	runLifecycle(t, ctx, lifecycleSpec{
		kind:   "network",
		list:   func(ctx context.Context) (RawJSON, error) { return lc.GetNetworkDetails(ctx, networkPageRequest()) },
		idOf:   intField("id"),
		nameOf: strField("name"),
		rename: setField("name"),
		// priority is a small 1-based network rank (0 is rejected).
		// Inert here — the test network matches nothing.
		template: func() map[string]any {
			return map[string]any{"address": "192.0.2.0/24", "priority": 1, "environments": []any{env}}
		},
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdateNetworkDetailsRecords(ctx, o)
		},
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdateNetworkDetailsRecords(ctx, o)
		},
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.RemoveNetworkDetailsRecords(ctx, []any{o})
		},
	})
}

// TestLiveSettingsBlockListCRUD — a throwaway block-list (model block) record.
// Keyed by entityIdentifier; a smoke-label hostname matches no real entity.
// BEST-EFFORT: the create body needs entityType (a SOAR entity-type string,
// guessed "HOSTNAME") plus elementType (0=HostName) and scope (3=ForModel) enums;
// if the server rejects the entityType, adjust it. A failed create creates
// nothing (cleanup is a no-op).
func TestLiveSettingsBlockListCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind:   "block-list",
		list:   func(ctx context.Context) (RawJSON, error) { return lc.GetAllModelBlockRecords(ctx) },
		idOf:   intField("id"),
		nameOf: strField("entityIdentifier"),
		rename: setField("entityIdentifier"),
		template: func() map[string]any {
			return map[string]any{"entityType": "USER", "elementType": 0, "scope": 3, "environments": []any{}}
		},
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdateModelBlockRecords(ctx, o)
		},
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdateModelBlockRecords(ctx, o)
		},
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.RemoveModelBlockRecords(ctx, o)
		},
	})
}
