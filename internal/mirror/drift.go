package mirror

import (
	"context"
	"fmt"
	"io"
	"strings"

	"danny.vn/secops/internal/mirror/reconcile"
)

// Drift detection: a read-only diff of local (committed) files against live state,
// for use as a CI gate. The loop is `pull` → commit → later `drift`: it reports,
// per surface, how the live tenant has diverged from the committed baseline. It
// NEVER mutates.
//
// Three outcomes per surface, kept distinct so the gate is trustworthy:
//   - DRIFT: a reconcilable divergence — local-only (create), changed (update), or
//     a live-only orphan on a prune-eligible surface (push --prune). Fails the gate.
//   - UNTRACKED: live-only objects on a NoDelete surface — push can never remove
//     them, so they are reported (pull to adopt) but do NOT fail the gate.
//   - INDETERMINATE: the live listing was incomplete (a transient per-item read
//     failure) or errored, so drift can't be judged. Fails the gate as "could not
//     verify", NOT as drift — an incomplete list would otherwise misclassify
//     in-sync objects as phantom creates.

// DriftTarget is one surface to check, with its on-disk directory.
type DriftTarget struct {
	Surface reconcile.Surface
	Dir     string
}

// DriftItem is the drift outcome for one surface. The *Names slices carry the
// slug of each diverged object so a report can name what drifted (a bare "~1" is
// undiagnosable without a manual diff).
type DriftItem struct {
	Name           string
	Created        int  // local-only (would be created)
	Updated        int  // changed (would be updated)
	Deleted        int  // live-only orphan on a prune-eligible surface (would be pruned)
	Untracked      int  // live-only on a NoDelete surface (not push-reconcilable; pull to adopt)
	Incomplete     bool // the live listing was incomplete (a per-item read failed)
	Err            error
	CreatedNames   []string
	UpdatedNames   []string
	DeletedNames   []string
	UntrackedNames []string
}

// Indeterminate reports that drift could not be judged (error or incomplete list).
func (d DriftItem) Indeterminate() bool { return d.Err != nil || d.Incomplete }

// Drifted reports a reconcilable divergence. It is false when indeterminate (an
// incomplete/errored listing is "could not verify", not confirmed drift) and
// ignores Untracked (not push-reconcilable).
func (d DriftItem) Drifted() bool {
	return !d.Indeterminate() && d.Created+d.Updated+d.Deleted > 0
}

// DriftReport aggregates per-surface outcomes.
type DriftReport struct{ Items []DriftItem }

// Drifted reports whether any surface has a reconcilable divergence.
func (r DriftReport) Drifted() bool {
	for _, it := range r.Items {
		if it.Drifted() {
			return true
		}
	}
	return false
}

// Indeterminate reports whether any surface could not be verified.
func (r DriftReport) Indeterminate() bool {
	for _, it := range r.Items {
		if it.Indeterminate() {
			return true
		}
	}
	return false
}

// Drift checks each target and writes a one-line-per-surface report to w. It is
// read-only (BuildPlan lists live + loads local; it never writes). A per-surface
// error is recorded on the item and the sweep continues.
func Drift(ctx context.Context, targets []DriftTarget, w io.Writer) DriftReport {
	var rep DriftReport
	for _, t := range targets {
		item := DriftItem{Name: t.Surface.Name}
		plan, live, err := reconcile.BuildPlan(ctx, t.Surface, t.Dir)
		if err != nil {
			item.Err = err
			fmt.Fprintf(w, "  %-26s ERROR         %v\n", t.Surface.Name, err)
			rep.Items = append(rep.Items, item)
			continue
		}
		item.Incomplete = live.Incomplete
		item.Created = len(plan.Creates())
		item.Updated = len(plan.Updates())
		item.CreatedNames = planSlugs(plan.Creates())
		item.UpdatedNames = planSlugs(plan.Updates())
		// Live-only orphans are reconcilable only where push can prune; elsewhere
		// (NoDelete) they are "untracked" — reported, not gate-failing.
		liveOnly := plan.Deletes()
		if t.Surface.Caps.PruneEligible && !t.Surface.Caps.NoDelete {
			item.Deleted = len(liveOnly)
			item.DeletedNames = planSlugs(liveOnly)
		} else {
			item.Untracked = len(liveOnly)
			item.UntrackedNames = planSlugs(liveOnly)
		}

		switch {
		case item.Incomplete:
			fmt.Fprintf(w, "  %-26s INDETERMINATE live list incomplete — could not verify\n", t.Surface.Name)
		case item.Drifted():
			fmt.Fprintf(w, "  %-26s DRIFT         +%d ~%d -%d%s%s\n",
				t.Surface.Name, item.Created, item.Updated, item.Deleted,
				untrackedNote(item.Untracked), item.driftNames())
		default:
			fmt.Fprintf(w, "  %-26s in sync%s\n", t.Surface.Name, untrackedNote(item.Untracked))
		}
		rep.Items = append(rep.Items, item)
	}
	return rep
}

func untrackedNote(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("  (%d untracked live-only — pull to adopt)", n)
}

// planSlugs returns the slugs of the given plan items, in plan order.
func planSlugs(items []reconcile.PlanItem) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Slug
	}
	return out
}

// driftNames appends the diverged object slugs to a DRIFT line so the report
// names what changed (e.g. "  [+a ~b -c]") rather than a bare count.
func (d DriftItem) driftNames() string {
	parts := make([]string, 0, len(d.CreatedNames)+len(d.UpdatedNames)+len(d.DeletedNames))
	for _, n := range d.CreatedNames {
		parts = append(parts, "+"+n)
	}
	for _, n := range d.UpdatedNames {
		parts = append(parts, "~"+n)
	}
	for _, n := range d.DeletedNames {
		parts = append(parts, "-"+n)
	}
	if len(parts) == 0 {
		return ""
	}
	return "  [" + strings.Join(parts, " ") + "]"
}
