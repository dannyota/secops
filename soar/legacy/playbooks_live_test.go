package legacy

import (
	"context"
	"encoding/json"
	"testing"
)

// TestLivePlaybooksReads exercises the playbook/ontology read endpoints (safe).
// Runs under SECOPS_SOAR_SMOKE=1.
func TestLivePlaybooksReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "playbooks/GetEnabledWFCards", func() (RawJSON, error) { return lc.ListEnabledWorkflowCards(ctx, struct{}{}) })
	readProbe(t, "playbooks/ListWorkflowCategories", func() (RawJSON, error) { return lc.ListWorkflowCategories(ctx) })
	readProbe(t, "ontology/ListVisualFamilies", func() (RawJSON, error) { return lc.ListVisualFamilies(ctx) })
}

// TestLivePlaybookCategoryCRUD runs the full lifecycle on a throwaway playbook
// category — the safest mutable config object (a cosmetic folder). It is the
// exemplar for the runLifecycle harness and only runs under
// SECOPS_SOAR_SMOKE_WRITE=1; it auto-skips under a read-only smoke run.
//
// Chain: list -> create -> list -> read -> edit -> read -> delete -> list, with
// t.Cleanup deleting the created category if any step fails midway.
func TestLivePlaybookCategoryCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind:   "playbook-category",
		list:   func(ctx context.Context) (RawJSON, error) { return lc.ListWorkflowCategories(ctx) },
		idOf:   intField("id"),
		nameOf: strField("name"),
		rename: setField("name"),
		prep: func(o map[string]any) {
			delete(o, "id")
			delete(o, "creationTimeUnixTimeInMs")
			delete(o, "modificationTimeUnixTimeInMs")
			o["isDefaultCategory"] = false // never clone a default-category flag
		},
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdatePlaybookCategory(ctx, o)
		},
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdatePlaybookCategory(ctx, o)
		},
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			id, _ := intField("id")(o)
			return lc.RemovePlaybookCategories(ctx, map[string]any{"ids": []int{id}})
		},
	})
}

// GROUP E (operational config) — playbooks. A full playbook is automation: it can
// run when an alert matches its trigger. To exercise the workflow surface inertly
// we DUPLICATE an existing DISABLED playbook (a disabled source yields a disabled
// copy that cannot run) and delete the copy. We never enable anything and never
// touch the source.

// playbookIDSet returns the set of playbook identifiers in a card list.
func playbookIDSet(cards []PlaybookCard) map[string]bool {
	m := make(map[string]bool, len(cards))
	for _, c := range cards {
		if c.Identifier != "" {
			m[c.Identifier] = true
		}
	}
	return m
}

// TestLivePlaybookDuplicateDelete duplicates a disabled playbook into a throwaway
// copy, verifies the copy is present and inert (disabled), then deletes it. It
// finds the copy by diffing the identifier set before/after (robust to naming and
// to a nested duplicate producing more than one new playbook), and cleans up every
// new playbook even on failure. Write-gated.
func TestLivePlaybookDuplicateDelete(t *testing.T) {
	lc, ctx := liveClient(t)
	requireWrite(t)

	cards, err := lc.ListPlaybooks(ctx, nil)
	if err != nil {
		t.Fatalf("list playbooks: %v", err)
	}
	var srcID string
	for _, c := range cards {
		if !c.IsEnabled && c.Identifier != "" {
			srcID = c.Identifier
			break
		}
	}
	if srcID == "" {
		t.Skip("no disabled playbook to use as a safe duplication source")
	}
	before := playbookIDSet(cards)

	// The source's full definition is the duplicate request body; force the copy
	// disabled and give it a unique name.
	raw, err := lc.GetWorkflowFullInfo(ctx, srcID)
	if err != nil {
		t.Fatalf("get source full info: %v", err)
	}
	var src map[string]any
	if err := json.Unmarshal(raw, &src); err != nil {
		t.Fatalf("decode source: %v", err)
	}
	label := smokeLabel("playbook")
	dup := cloneObj(src)
	dup["name"] = label
	dup["isEnabled"] = false
	if _, err := lc.DuplicateWorkflow(ctx, dup); err != nil {
		t.Fatalf("duplicate workflow: %v", err)
	}

	// Identify every new playbook (set diff), fetch each full object (for delete),
	// and register cleanup immediately so nothing leaks on a later failure.
	cards2, err := lc.ListPlaybooks(ctx, nil)
	if err != nil {
		t.Fatalf("list playbooks #2: %v", err)
	}
	var newIDs []string
	for _, c := range cards2 {
		if c.Identifier != "" && !before[c.Identifier] {
			newIDs = append(newIDs, c.Identifier)
		}
	}
	if len(newIDs) == 0 {
		t.Fatalf("duplicate not found: no new playbook identifier appeared")
	}
	copies := make([]map[string]any, 0, len(newIDs))
	for _, id := range newIDs {
		raw, err := lc.GetWorkflowFullInfo(ctx, id)
		if err != nil {
			t.Fatalf("get copy %q full info: %v", id, err)
		}
		var o map[string]any
		if err := json.Unmarshal(raw, &o); err != nil {
			t.Fatalf("decode copy %q: %v", id, err)
		}
		copies = append(copies, o)
	}
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		for _, o := range copies {
			if _, err := lc.DeleteWorkflow(ctx, o); err != nil {
				t.Logf("cleanup: could not delete throwaway playbook %v: %v", o["identifier"], err)
			}
		}
	})

	// Every copy must be inert (disabled).
	for _, o := range copies {
		if en, _ := o["isEnabled"].(bool); en {
			t.Fatalf("duplicated playbook %v is ENABLED; expected disabled (inert)", o["identifier"])
		}
	}

	// Delete every copy.
	for _, o := range copies {
		if _, err := lc.DeleteWorkflow(ctx, o); err != nil {
			t.Fatalf("delete workflow %v: %v", o["identifier"], err)
		}
	}
	deleted = true

	// Verify all gone.
	cards3, err := lc.ListPlaybooks(ctx, nil)
	if err != nil {
		t.Fatalf("list playbooks #3: %v", err)
	}
	still := playbookIDSet(cards3)
	for _, id := range newIDs {
		if still[id] {
			t.Fatalf("duplicated playbook %q still present after delete", id)
		}
	}
}
