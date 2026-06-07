package soar_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestLiveDataSurfaceWriteSmoke validates that the modern v1alpha SOAR data-surface
// WRITES work on the siemplify-soar host — they do not 500. Self-cleaning
// create→get→delete on inert, self-identifying throwaways:
//   - slaDefinitions: an SLA on a non-existent alert-rule-generator (matches no
//     alert), string enums, environments sent as the configured environment.
//   - soarNetworks: an RFC5737 TEST-NET (192.0.2.0/24) entry, environmentsJson a
//     JSON-encoded string, low-blast enrichment data.
//
// Cleanup sweeps each collection for the marker and deletes by exact id, failing
// loudly only if a delete cannot complete. Gated on SECOPS_SOAR_SMOKE=1 +
// SECOPS_SOAR_SMOKE_WRITE=1.
func TestLiveDataSurfaceWriteSmoke(t *testing.T) {
	c, ctx := liveClient(t)
	if os.Getenv("SECOPS_SOAR_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SOAR_SMOKE_WRITE=1 to run (creates + deletes throwaway SLA + network)")
	}

	marker := fmt.Sprintf("secopsctl-smoke-%d", time.Now().UnixNano())
	env := "Default Environment"
	if envs, err := c.ListEnvironments(ctx); err == nil {
		for _, e := range envs {
			var m struct {
				DisplayName string `json:"displayName"`
			}
			if json.Unmarshal(e, &m) == nil && m.DisplayName != "" {
				env = m.DisplayName
				break
			}
		}
	}

	lists := map[string]func() ([]json.RawMessage, error){
		"slaDefinitions": func() ([]json.RawMessage, error) { return c.ListSlaDefinitions(ctx) },
		"soarNetworks":   func() ([]json.RawMessage, error) { return c.ListSoarNetworks(ctx) },
	}
	dels := map[string]func(context.Context, string) error{
		"slaDefinitions": c.DeleteSlaDefinition,
		"soarNetworks":   c.DeleteSoarNetwork,
	}
	t.Cleanup(func() {
		for res, list := range lists {
			items, err := list()
			if err != nil {
				t.Errorf("cleanup: list %s failed, residue may remain: %v", res, err)
				continue
			}
			for _, it := range items {
				if bytes.Contains(it, []byte(marker)) {
					id := idFromRaw(it)
					if err := dels[res](ctx, id); err != nil {
						t.Errorf("CLEANUP FAILED to delete leftover %s/%s: %v", res, id, err)
					} else {
						t.Logf("cleanup: removed leftover %s/%s", res, id)
					}
				}
			}
		}
	})

	full := func(name string,
		create func(context.Context, any) (json.RawMessage, error),
		get func(context.Context, string) (json.RawMessage, error),
		del func(context.Context, string) error, body map[string]any,
	) {
		created, err := create(ctx, body)
		if err != nil {
			t.Errorf("[%s] create failed (no 500 expected): %v", name, err)
			return
		}
		id := idFromRaw(created)
		if id == "" {
			t.Errorf("[%s] create returned no id: %s", name, created)
			return
		}
		if got, err := get(ctx, id); err != nil {
			t.Errorf("[%s] get(%s): %v", name, id, err)
		} else if !bytes.Contains(got, []byte(marker)) {
			t.Errorf("[%s] round-trip: marker missing from %s", name, id)
		}
		if err := del(ctx, id); err != nil {
			t.Errorf("[%s] delete(%s): %v", name, id, err)
			return
		}
		t.Logf("[%s] create→get→delete OK (id=%s) — v1alpha write works", name, id)
	}

	full("slaDefinitions", c.CreateSlaDefinition, c.GetSlaDefinition, c.DeleteSlaDefinition, map[string]any{
		"slaType": "ALERT_RULE_GENERATOR", "alertType": "SPECIFIC_ALERTS",
		"slaTypeValue": marker, "slaPeriod": 2, "slaPeriodTimeUnit": "DAYS",
		"criticalSlaPeriod": 1, "criticalSlaPeriodTimeUnit": "DAYS",
		"environments": []string{env},
	})
	full("soarNetworks", c.CreateSoarNetwork, c.GetSoarNetwork, c.DeleteSoarNetwork, map[string]any{
		"displayName": marker, "address": "192.0.2.0/24",
		"environmentsJson": fmt.Sprintf("[%q]", env), "priority": 1,
	})
}
