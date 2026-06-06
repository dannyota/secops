package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/internal/mirror/reconcile"
	"danny.vn/secops/soar/legacy"
)

// Playbooks are the one SOAR surface that does NOT fit the per-object engine
// cleanly: a save mints a NEW uuid (so identity is the display NAME, not the id),
// the body is a large nested whole-body replace, and SOAR insists on int->str
// coercion on save (handled inside legacy.SavePlaybook). So this is a bespoke
// reconcile.Surface rather than a jsonSurface.
//
// Design that resolves the strip-vs-save tension: the FULL body is written to
// disk (faithful + a valid save body), while the engine compares a CANONICAL
// projection that strips the volatile churn (rotating uuids, version, timestamps,
// debug state) — so `git diff` and the push plan show real logic changes, not
// server bookkeeping. ServerID is the playbook NAME (stable across saves).
//
// Caps: WholeBodyWrite (update sends the whole edited body), NoDelete (a playbook
// is live automation; deletion is never inferred from a missing file — remove via
// the UI or an explicit command).

// playbookStripFields are dropped from the canonical (comparison) projection at
// any depth: the rotating uuids, version, server timestamps, debug state, and the
// "original*"/"workflowIdentifier"/loop-ref uuids that change without a logic edit.
var playbookStripFields = []string{
	"identifier", "version", "modifiedBy", "debugData", "logsExplorerUrl",
	"originalPlaybookIdentifier", "originalStepIdentifier", "workflowIdentifier",
	"startLoopStepIdentifier", "creationTimeUnixTimeInMs", "modificationTimeUnixTimeInMs",
}

func canonicalPlaybook(raw json.RawMessage) ([]byte, error) {
	return reconcile.Canonicalize(raw, playbookStripFields...)
}

// playbookNameOf reads the "name" field from a playbook body.
func playbookNameOf(raw json.RawMessage) string {
	var m struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &m)
	return m.Name
}

// playbooksSurface exposes SOAR playbooks as config-as-code through the engine.
func playbooksSurface(lc *legacy.Client) reconcile.Surface {
	build := func(raw json.RawMessage) (reconcile.Object, error) {
		canon, err := canonicalPlaybook(raw)
		if err != nil {
			return reconcile.Object{}, err
		}
		name := playbookNameOf(raw)
		return reconcile.Object{Slug: Slugify(name), ServerID: name, Canonical: canon, Raw: raw}, nil
	}

	resolveByName := func(ctx context.Context, name string) (reconcile.Object, error) {
		body, err := lc.GetPlaybookByName(ctx, name, false)
		if err != nil {
			return reconcile.Object{}, err
		}
		return build(body)
	}

	return reconcile.Surface{
		Name:    "playbooks",
		Dir:     DirSOARPlaybooks,
		Product: reconcile.ProductSOAR,
		Caps:    reconcile.Capabilities{WholeBodyWrite: true, NoDelete: true},

		List: func(ctx context.Context) (reconcile.ListResult, error) {
			cards, err := lc.ListPlaybooks(ctx, nil)
			if err != nil {
				return reconcile.ListResult{}, err
			}
			res := reconcile.ListResult{}
			for _, card := range cards {
				body, gerr := lc.GetPlaybook(ctx, card.Identifier)
				if gerr != nil {
					warnf("playbook %q: %v", card.Name, gerr)
					res.Incomplete = true
					continue
				}
				o, berr := build(body)
				if berr != nil {
					warnf("playbook %q: %v", card.Name, berr)
					res.Incomplete = true
					continue
				}
				res.Objects = append(res.Objects, o)
			}
			return res, nil
		},

		LoadDir: func(dir string) ([]reconcile.Object, error) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					return nil, nil
				}
				return nil, err
			}
			var objs []reconcile.Object
			for _, e := range entries {
				if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
				if rerr != nil {
					return nil, rerr
				}
				o, berr := build(raw)
				if berr != nil {
					return nil, fmt.Errorf("playbook %s: %w", e.Name(), berr)
				}
				o.Slug = strings.TrimSuffix(e.Name(), ".json")
				objs = append(objs, o)
			}
			return objs, nil
		},

		// Write the FULL body (faithful + a valid save body), pretty-printed.
		Write: func(dir string, o reconcile.Object) error {
			if _, err := EnsureDir(dir); err != nil {
				return err
			}
			return writeIndentedJSON(filepath.Join(dir, o.Slug+".json"), o.Raw)
		},

		// Create/Update are a whole-body save; SavePlaybook coerces int->str,
		// validates the name, and mints a new uuid — so re-resolve by name after.
		Create: func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
			if _, err := lc.SavePlaybook(ctx, local.Raw); err != nil {
				return reconcile.Object{}, err
			}
			return resolveByName(ctx, playbookNameOf(local.Raw))
		},
		Update: func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
			if _, err := lc.SavePlaybook(ctx, local.Raw); err != nil {
				return reconcile.Object{}, err
			}
			return resolveByName(ctx, live.ServerID)
		},
	}
}
