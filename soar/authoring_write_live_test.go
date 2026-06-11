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
		if _, err := c.CreateActionDef(ctx, integration, body); err != nil {
			t.Fatalf("create: %v", err)
		}
		// The create may return before the catalog indexes it; resolve the id
		// from the per-integration list and delete by EXACT id.
		var id string
		defs, err := c.ListActions(ctx, integration)
		if err != nil {
			t.Fatalf("list after create: %v", err)
		}
		for i := range defs {
			if defs[i].DisplayName == name {
				id = defs[i].PathID()
			}
		}
		if id == "" {
			t.Fatalf("created action %q not found in the catalog — delete it manually", name)
		}
		if err := c.DeleteActionDef(ctx, integration, id); err != nil {
			t.Fatalf("delete %s: %v — remove action %q manually", id, err, name)
		}
		defs, err = c.ListActions(ctx, integration)
		if err != nil {
			t.Fatalf("list after delete: %v", err)
		}
		for i := range defs {
			if defs[i].DisplayName == name {
				t.Errorf("action %q (id %s) still listed after delete", name, id)
			}
		}
		t.Logf("OK action authoring loop: created %q as id %s, deleted, verified gone", name, id)
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
