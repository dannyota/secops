package reconcile

import "context"

// Product distinguishes the two CLI trees so the engine can label output and the
// caller can keep two registries. It carries no behavior of its own.
type Product int

const (
	ProductSIEM Product = iota
	ProductSOAR
)

func (p Product) String() string {
	if p == ProductSOAR {
		return "soar"
	}
	return "siem"
}

// Capabilities declares what one reconcile-lane surface supports, so the engine can
// adapt the push semantics without inspecting payloads.
type Capabilities struct {
	// NoDelete: the surface has no delete operation. Live-only objects are always
	// reported as warn-skip drift and never deleted, even with --prune.
	NoDelete bool
	// WholeBodyWrite: Create/Update send a whole body (the Update closure should
	// overlay local edits onto the live body via DeepMerge); otherwise the closure
	// sends a sparse patch of changed fields.
	WholeBodyWrite bool
	// NoEtag: the surface has no optimistic-concurrency token.
	NoEtag bool
	// PruneEligible: the surface has a clean delete-by-id, so --prune may delete
	// live-only objects (still guarded + gated on a complete pull). Surfaces with
	// irreversible/high-blast-radius deletes leave this false.
	PruneEligible bool
}

// ListResult is the outcome of a surface List: the live objects plus whether the
// listing was INCOMPLETE (one or more items skipped due to a per-item error). An
// incomplete list disables --prune so a transient skip can never be mistaken for
// an intentional deletion.
type ListResult struct {
	Objects    []Object
	Incomplete bool
}

// Surface is the declarative descriptor for one per-object-CUD config surface.
// The caller (internal/mirror) fills the closures using a SecOps SDK; this
// package never sees the SDK. The CUD closures own their body construction
// (sparse patch vs whole-body overlay, type coercion, the redaction guard),
// because only the surface knows the write shape.
type Surface struct {
	Name    string // CLI target, e.g. "webhooks", "reference_lists"
	Dir     string // on-disk subdir under the product root
	Product Product
	Caps    Capabilities

	// List reads every live object (Slug/ServerID/Canonical filled; Raw filled
	// where an Update overlay needs it).
	List func(ctx context.Context) (ListResult, error)

	// LoadDir loads every local on-disk object from dir (ServerID from the stored
	// identity; Canonical recomputed). Missing dir → empty, not an error.
	LoadDir func(dir string) ([]Object, error)

	// Write renders one live object to disk under dir (the pull writer).
	Write func(dir string, o Object) error

	// Create/Update/Delete are the live mutations. Create receives the local
	// object; Update receives both local and live (for overlay); Delete receives
	// the live object. Each returns the server-echoed object where available (to
	// refresh the on-disk identity), or the zero Object when the API echoes
	// nothing. A nil closure means the capability is unsupported.
	Create func(ctx context.Context, local Object) (Object, error)
	Update func(ctx context.Context, local, live Object) (Object, error)
	Delete func(ctx context.Context, live Object) error

	// Validate, when set, statically checks a to-be-written (Create/Update) local
	// object's shape BEFORE any API call, so a body the server would reject at
	// --yes surfaces in the dry-run preview instead of as a late 400. A nil hook
	// means no extra validation (the diff is the only check). Best-effort and
	// structural — it is not a substitute for the server's own validation.
	Validate func(local Object) error
}
