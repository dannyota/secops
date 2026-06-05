package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// pullStateFile records whether the snapshot in a surface directory came from a
// COMPLETE pull. --prune refuses to run against an incomplete snapshot, so a
// transient per-item skip during pull can never be mistaken for an intentional
// deletion and destroy live config.
const pullStateFile = ".pullstate.json"

type pullState struct {
	Surface  string `json:"surface"`
	Complete bool   `json:"complete"`
	Count    int    `json:"count"`
}

// Pull lists every live object and writes it to dir, then records pull state.
// It is non-destructive: it never removes local files (use git to review what a
// pull changed). Returns the number of objects written.
func Pull(ctx context.Context, s Surface, dir string, w io.Writer) (int, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return 0, err
	}
	res, err := s.List(ctx)
	if err != nil {
		return 0, err
	}
	written := 0
	for _, o := range res.Objects {
		if err := s.Write(dir, o); err != nil {
			return written, err
		}
		written++
	}
	st := pullState{Surface: s.Name, Complete: !res.Incomplete, Count: written}
	if err := writePullState(dir, st); err != nil {
		return written, err
	}
	note := ""
	if res.Incomplete {
		note = "  (INCOMPLETE — some items skipped; --prune will be refused)"
	}
	fmt.Fprintf(w, "%s: wrote %d object(s) -> %s/%s\n", s.Name, written, dir, note)
	return written, nil
}

// BuildPlan classifies the difference between local files and live objects.
// Matching is by ServerID; the comparison is over canonical bytes. Slugs/paths
// are taken from the local side for create/update and the live side for delete.
func BuildPlan(ctx context.Context, s Surface, dir string) (Plan, ListResult, error) {
	locals, err := s.LoadDir(dir)
	if err != nil {
		return Plan{}, ListResult{}, fmt.Errorf("load local %s: %w", s.Name, err)
	}
	live, err := s.List(ctx)
	if err != nil {
		return Plan{}, ListResult{}, fmt.Errorf("list live %s: %w", s.Name, err)
	}

	liveByID := make(map[string]*Object, len(live.Objects))
	for i := range live.Objects {
		o := &live.Objects[i]
		if o.ServerID != "" {
			liveByID[o.ServerID] = o
		}
	}

	var items []PlanItem
	matched := make(map[string]bool, len(live.Objects))
	for i := range locals {
		lo := &locals[i]
		path := filepath.Join(dir, lo.Slug)
		if lo.ServerID == "" {
			items = append(items, PlanItem{Action: ActionCreate, Slug: lo.Slug, Path: path, Local: lo})
			continue
		}
		lv, ok := liveByID[lo.ServerID]
		if !ok {
			// Local desired state references a server id that is gone — recreate.
			items = append(items, PlanItem{Action: ActionCreate, Slug: lo.Slug, ServerID: lo.ServerID, Path: path, Local: lo})
			continue
		}
		matched[lo.ServerID] = true
		act := ActionUnchanged
		if !bytes.Equal(lo.Canonical, lv.Canonical) {
			act = ActionUpdate
		}
		items = append(items, PlanItem{Action: act, Slug: lo.Slug, ServerID: lo.ServerID, Path: path, Local: lo, Live: lv})
	}
	for i := range live.Objects {
		lv := &live.Objects[i]
		if lv.ServerID == "" || matched[lv.ServerID] {
			continue
		}
		items = append(items, PlanItem{Action: ActionDelete, Slug: lv.Slug, ServerID: lv.ServerID, Live: lv})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Action != items[j].Action {
			return items[i].Action < items[j].Action
		}
		return items[i].Slug < items[j].Slug
	})
	return Plan{Items: items}, live, nil
}

// PushOpts controls a guarded push.
type PushOpts struct {
	DryRun    bool
	AssumeYes bool
	Prune     bool
	// Banner prints the LIVE-deploy warning; if nil a built-in banner is used.
	Banner func(w io.Writer, action string)
}

// Summary reports what a push did (or would do).
type Summary struct {
	Created        int
	Updated        int
	Deleted        int
	Unchanged      int
	SkippedDeletes []PlanItem
	SkipReason     string
}

// Push reconciles dir to live: Create new files, Update changed ones, and (only
// with a satisfied --prune) Delete server-only objects. Additive is the default
// — server-only objects are otherwise warned, skipped, and reprinted in a final
// summary block so the reminder survives a long log. Dry-run by default; a real
// apply needs AssumeYes.
func Push(ctx context.Context, s Surface, dir string, opts PushOpts, w io.Writer) (Summary, error) {
	plan, live, err := BuildPlan(ctx, s, dir)
	if err != nil {
		return Summary{}, err
	}

	creates, updates, deletes := plan.Creates(), plan.Updates(), plan.Deletes()
	sum := Summary{Unchanged: plan.Unchanged()}

	canPrune, reason := prunable(s, dir, opts.Prune, live.Incomplete)

	banner := opts.Banner
	if banner == nil {
		banner = defaultBanner
	}
	action := fmt.Sprintf("%s: +%d create, ~%d update, -%d delete",
		s.Name, len(creates), len(updates), len(deletes))
	banner(w, action)
	printPlan(w, plan)

	if plan.Empty() {
		fmt.Fprintf(w, "Nothing to do — %s is in sync.\n", s.Name)
		return sum, nil
	}
	if opts.DryRun {
		fmt.Fprintln(w, "\nDRY RUN — no API calls made. Re-run without --dry-run to apply.")
		if len(deletes) > 0 {
			finalSummary(w, deletes, canPrune, reason)
		}
		return sum, nil
	}
	if !opts.AssumeYes {
		fmt.Fprintf(w, "\nRefusing to apply %s without confirmation (pass --yes). Aborted.\n", s.Name)
		return sum, nil
	}

	// Apply creates then updates.
	for _, it := range creates {
		if s.Create == nil {
			fmt.Fprintf(w, "  SKIP create %s: surface has no create op\n", it.Slug)
			continue
		}
		echo, cerr := s.Create(ctx, *it.Local)
		if cerr != nil {
			fmt.Fprintf(w, "  FAIL create %s: %v\n", it.Slug, cerr)
			continue
		}
		refreshLocal(s, dir, it, echo, w)
		sum.Created++
		fmt.Fprintf(w, "  created  %s\n", it.Slug)
	}
	for _, it := range updates {
		if s.Update == nil {
			fmt.Fprintf(w, "  SKIP update %s: surface has no update op\n", it.Slug)
			continue
		}
		echo, uerr := s.Update(ctx, *it.Local, *it.Live)
		if uerr != nil {
			fmt.Fprintf(w, "  FAIL update %s: %v\n", it.Slug, uerr)
			continue
		}
		refreshLocal(s, dir, it, echo, w)
		sum.Updated++
		fmt.Fprintf(w, "  updated  %s\n", it.Slug)
	}

	// Deletes only when prune is satisfied; otherwise skip and remember.
	if canPrune && s.Delete != nil {
		for _, it := range deletes {
			if derr := s.Delete(ctx, *it.Live); derr != nil {
				fmt.Fprintf(w, "  FAIL delete %s: %v\n", it.Slug, derr)
				continue
			}
			sum.Deleted++
			fmt.Fprintf(w, "  deleted  %s\n", it.Slug)
		}
	} else {
		sum.SkippedDeletes = deletes
		sum.SkipReason = reason
	}

	fmt.Fprintf(w, "\nDone. %d created, %d updated, %d deleted, %d unchanged.\n",
		sum.Created, sum.Updated, sum.Deleted, sum.Unchanged)
	if len(sum.SkippedDeletes) > 0 {
		finalSummary(w, sum.SkippedDeletes, canPrune, reason)
	}
	return sum, nil
}

// prunable reports whether --prune may delete, and the reason it may not.
func prunable(s Surface, dir string, prune, liveIncomplete bool) (bool, string) {
	if !prune {
		return false, "additive mode (default) — pass --prune to delete server-only objects"
	}
	if s.Caps.NoDelete || !s.Caps.PruneEligible || s.Delete == nil {
		return false, "this surface does not support prune-delete (no safe delete-by-id)"
	}
	if liveIncomplete {
		return false, "live listing was incomplete — refusing to prune (some objects may be hidden)"
	}
	st, err := readPullState(dir)
	if err != nil || !st.Complete {
		return false, "local snapshot is from an incomplete pull — re-pull this surface before --prune"
	}
	return true, ""
}

// refreshLocal rewrites the on-disk object from the server's echo so the stored
// identity (and any server-normalized fields) match live after a mutation.
func refreshLocal(s Surface, dir string, it PlanItem, echo Object, w io.Writer) {
	if echo.ServerID == "" && len(echo.Canonical) == 0 {
		return // API echoed nothing usable; leave the local file as-is.
	}
	// Refresh the operator's EXISTING file (it.Slug), not a slugified copy of the
	// display name — otherwise a create writes the server id to a different file
	// and the original re-creates on the next push (duplicate live objects).
	echo.Slug = it.Slug
	if err := s.Write(dir, echo); err != nil {
		fmt.Fprintf(w, "  WARN %s: applied live but local refresh failed: %v\n", it.Slug, err)
	}
}

func printPlan(w io.Writer, p Plan) {
	for _, it := range p.Items {
		switch it.Action {
		case ActionCreate:
			fmt.Fprintf(w, "  + create  %s\n", it.Slug)
		case ActionUpdate:
			fmt.Fprintf(w, "  ~ update  %s\n", it.Slug)
		case ActionDelete:
			fmt.Fprintf(w, "  - delete  %s\n", it.Slug)
		}
	}
}

// finalSummary reprints skipped deletions at the very end so the reminder is not
// lost above a long apply log.
func finalSummary(w io.Writer, skipped []PlanItem, canPrune bool, reason string) {
	bar := strings.Repeat("=", 72)
	fmt.Fprintln(w, "\n"+bar)
	if canPrune {
		// Reached only on a dry run that WOULD prune.
		fmt.Fprintf(w, "PRUNE: %d server-only object(s) would be DELETED on apply:\n", len(skipped))
	} else {
		fmt.Fprintf(w, "PRUNE SKIPPED: %d server-only object(s) were NOT deleted.\n", len(skipped))
	}
	for _, it := range skipped {
		fmt.Fprintf(w, "  - %s (%s)\n", it.Slug, it.ServerID)
	}
	if !canPrune {
		fmt.Fprintf(w, "Reason: %s\n", reason)
	}
	fmt.Fprintln(w, bar)
}

func defaultBanner(w io.Writer, action string) {
	bar := strings.Repeat("!", 72)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "!! LIVE DEPLOY -- this targets a PRODUCTION SecOps tenant !!")
	fmt.Fprintf(w, "!! Action: %s\n", action)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w)
}

func writePullState(dir string, st pullState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, pullStateFile), append(b, '\n'), 0o644)
}

func readPullState(dir string) (pullState, error) {
	var st pullState
	b, err := os.ReadFile(filepath.Join(dir, pullStateFile))
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}
