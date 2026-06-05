// Package reconcile is the product-neutral core of secopsctl's config-as-code
// loop: pull = List/Read live state into files, push = Create/Update/Delete that
// reconciles files back to live. It deliberately imports NO SecOps SDK — a
// caller (internal/mirror) supplies each surface's List/file-I/O/CUD behavior
// through the Surface descriptor, so this package stays pure and unit-testable.
//
// Only surfaces that genuinely present per-object CUD (a stable identity, a read
// shape that round-trips to a write shape, and a delete-by-id) belong here.
// Batch upserts, opaque export/import bundles, and selector-only reads do NOT —
// they are handled by the caller's raw/explicit and imperative command lanes.
package reconcile

import "encoding/json"

// Object is the engine's intermediate representation of one config item, shared
// by both products. A local object (loaded from disk) and a live object (from
// the API) are compared by their Canonical bytes and matched by ServerID.
type Object struct {
	// Slug is the filename stem on disk, derived from the display name.
	Slug string
	// ServerID is the authoritative server identity (resource name or id). An
	// empty ServerID means "not yet created" — the engine plans a Create.
	ServerID string
	// Etag is an optional optimistic-concurrency token (NoEtag surfaces omit it).
	Etag string
	// Canonical is the deterministic, redacted, volatile-stripped representation
	// used as the diff basis. Both local and live objects canonicalize the same
	// way so secrets and server-managed fields never produce phantom diffs.
	Canonical []byte
	// Raw is the FULL, UNREDACTED live body, populated for LIVE objects only. It
	// is the overlay base for Update (apply local edits onto the live body so the
	// real secret is preserved). Local objects never carry Raw — the on-disk
	// snapshot is redacted and must never be trusted as a send body.
	Raw json.RawMessage
}

// Action classifies a single planned change.
type Action int

const (
	// ActionUnchanged means the local and live canonical forms are identical.
	ActionUnchanged Action = iota
	// ActionCreate means a local file has no matching live object.
	ActionCreate
	// ActionUpdate means identity matches but the canonical forms differ.
	ActionUpdate
	// ActionDelete means a live object has no matching local file (prune target).
	ActionDelete
)

func (a Action) String() string {
	switch a {
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	case ActionDelete:
		return "delete"
	default:
		return "unchanged"
	}
}

// PlanItem is one classified change between local files and live objects.
type PlanItem struct {
	Action   Action
	Slug     string
	ServerID string
	Path     string  // local file path (Create/Update); empty for Delete
	Local    *Object // nil for Delete
	Live     *Object // nil for Create
}

// Plan is the full classified diff for one surface.
type Plan struct {
	Items []PlanItem
}

// byAction returns the plan items with the given action.
func (p Plan) byAction(a Action) []PlanItem {
	var out []PlanItem
	for _, it := range p.Items {
		if it.Action == a {
			out = append(out, it)
		}
	}
	return out
}

// Creates returns the items the engine would create.
func (p Plan) Creates() []PlanItem { return p.byAction(ActionCreate) }

// Updates returns the items the engine would update.
func (p Plan) Updates() []PlanItem { return p.byAction(ActionUpdate) }

// Deletes returns the items the engine would delete (only with --prune).
func (p Plan) Deletes() []PlanItem { return p.byAction(ActionDelete) }

// Unchanged returns the count of items that match live exactly.
func (p Plan) Unchanged() int { return len(p.byAction(ActionUnchanged)) }

// Empty reports whether the plan has no create/update/delete work.
func (p Plan) Empty() bool {
	return len(p.Creates()) == 0 && len(p.Updates()) == 0 && len(p.Deletes()) == 0
}
