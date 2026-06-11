package soar_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// fillTemplate overlays create-marker fields onto a fetched definition
// template via a RawMessage map (numeric fields survive byte-exact).
func fillTemplate(t *testing.T, tpl json.RawMessage, integration, displayName, script string) json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(tpl, &m); err != nil {
		t.Fatalf("decode template: %v", err)
	}
	for k, v := range map[string]any{
		"name": "", "integration": integration, "displayName": displayName,
		"script": script, "custom": true,
	} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		m[k] = b
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// withField sets one top-level field on a definition body via a RawMessage map
// (every other field, including int64s, survives byte-exact).
func withField(t *testing.T, body json.RawMessage, key string, value any) json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	m[key] = b
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestLiveAuthoringWriteSmoke validates the full Python-definition authoring
// loop for actions AND jobs: fetchTemplate → create (a throwaway with a
// unique self-identifying name) → find it in the catalog → delete by exact id
// → verify gone. Cleanup runs even on assertion failure. Gated on
// SECOPS_SOAR_SMOKE=1 + SECOPS_SOAR_SMOKE_WRITE=1 — it creates and deletes
// definitions on the live tenant.
func TestLiveAuthoringWriteSmoke(t *testing.T) {
	if os.Getenv("SECOPS_SOAR_SMOKE_WRITE") != "1" {
		t.Skip("write smoke — set SECOPS_SOAR_SMOKE_WRITE=1 (and SECOPS_SOAR_SMOKE=1) to run")
	}
	c, ctx := liveClient(t)
	const integration = "HTTP"

	t.Run("action", func(t *testing.T) {
		tpl, err := c.FetchActionTemplate(ctx, integration, false)
		if err != nil {
			t.Fatalf("fetch template: %v", err)
		}
		name := fmt.Sprintf("secopsctl-smoke-action-%d", time.Now().UnixNano())
		body := fillTemplate(t, tpl, integration, name, "print('secopsctl write smoke')\n")
		// Set a known description so the update leg has something observable to
		// change in the (summary-only) catalog.
		body = withField(t, body, "description", "v1")
		created, err := c.CreateActionDef(ctx, integration, body)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		// UPDATE leg: re-save the create RESPONSE (its `name` is populated, so
		// this is an update, not a second create) with a changed description.
		updateBody := withField(t, created, "description", "v2")
		if _, err := c.UpdateActionDef(ctx, integration, updateBody); err != nil {
			t.Fatalf("update: %v (pins the update shape — if this 4xxs, the update is PATCH, not POST-with-name)", err)
		}

		// Resolve the id from the per-integration list; assert the update was an
		// UPDATE (still exactly one action with this name) and that it took
		// (description is v2), then delete by EXACT id.
		find := func(stage string) (id, desc string, count int) {
			defs, err := c.ListActions(ctx, integration)
			if err != nil {
				t.Fatalf("list %s: %v", stage, err)
			}
			for i := range defs {
				if defs[i].DisplayName == name {
					id, desc, count = defs[i].PathID(), defs[i].Description, count+1
				}
			}
			return id, desc, count
		}
		id, desc, count := find("after update")
		if id == "" {
			t.Fatalf("action %q not found in the catalog — delete it manually", name)
		}
		if count != 1 {
			t.Errorf("update created a duplicate: %d actions named %q (update should be in-place)", count, name)
		}
		if desc != "v2" {
			t.Errorf("description = %q after update, want v2 (the update did not take)", desc)
		}
		if err := c.DeleteActionDef(ctx, integration, id); err != nil {
			t.Fatalf("delete %s: %v — remove action %q manually", id, err, name)
		}
		if _, _, count := find("after delete"); count != 0 {
			t.Errorf("action %q (id %s) still listed after delete", name, id)
		}
		t.Logf("OK action authoring loop: create -> update(desc v1->v2, in-place) -> delete, id %s, verified gone", id)
	})

	t.Run("job", func(t *testing.T) {
		tpl, err := c.FetchJobTemplate(ctx, integration)
		if err != nil {
			t.Fatalf("fetch template: %v", err)
		}
		name := fmt.Sprintf("secopsctl-smoke-job-%d", time.Now().UnixNano())
		body := fillTemplate(t, tpl, integration, name, "print('secopsctl write smoke')\n")
		if _, err := c.CreateJobDef(ctx, integration, body); err != nil {
			t.Fatalf("create (the jobs collection may not accept POST like actions do): %v", err)
		}
		var id string
		jobs, err := c.ListJobs(ctx, integration)
		if err != nil {
			t.Fatalf("list after create: %v", err)
		}
		for i := range jobs {
			if jobs[i].DisplayName == name {
				id = jobs[i].PathID()
			}
		}
		if id == "" {
			t.Fatalf("created job %q not found — delete it manually", name)
		}
		if err := c.DeleteJobDef(ctx, integration, id); err != nil {
			t.Fatalf("delete %s: %v — remove job %q manually", id, err, name)
		}
		jobs, err = c.ListJobs(ctx, integration)
		if err != nil {
			t.Fatalf("list after delete: %v", err)
		}
		for i := range jobs {
			if jobs[i].DisplayName == name {
				t.Errorf("job %q (id %s) still listed after delete", name, id)
			}
		}
		t.Logf("OK job authoring loop: created %q as id %s, deleted, verified gone", name, id)
	})
}
