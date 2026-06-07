package soar_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"danny.vn/secops/soar/internal/transport"
)

// idFromRaw pulls the addressable id (last segment of name, else id) from a raw
// v1alpha record.
func idFromRaw(raw json.RawMessage) string {
	var m struct {
		Name string          `json:"name"`
		ID   json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(raw, &m)
	if i := strings.LastIndex(m.Name, "/"); i >= 0 {
		return m.Name[i+1:]
	}
	if len(m.ID) > 0 {
		return strings.Trim(string(m.ID), `"`)
	}
	return m.Name
}

// TestLiveConfigSurfaceWriteSmoke validates that the modern v1alpha config-surface
// WRITES work on the SOAR host — they do not 500. It runs a self-cleaning
// create→get→delete on inert, self-identifying throwaways for customLists,
// socRoles, and caseTagDefinitions (every required field per the v1alpha REST
// reference; repeated fields sent as []). environments create is exercised too
// but tolerates a license-quota 400 (a correctly-enforced business rule, not a
// failure). A v1alpha write can return an error yet still persist, so cleanup
// sweeps every collection for the marker and deletes by exact id, failing loudly
// if residue cannot be removed. Gated on SECOPS_SOAR_SMOKE=1 +
// SECOPS_SOAR_SMOKE_WRITE=1.
func TestLiveConfigSurfaceWriteSmoke(t *testing.T) {
	c, ctx := liveClient(t)
	if os.Getenv("SECOPS_SOAR_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SOAR_SMOKE_WRITE=1 to run (creates + deletes throwaway config items)")
	}

	marker := fmt.Sprintf("secopsctl-smoke-%d", time.Now().UnixNano())

	// Pick a real environment for the customLists association.
	envName := "Default Environment"
	if envs, err := c.ListEnvironments(ctx); err == nil {
		for _, e := range envs {
			var m struct {
				DisplayName string `json:"displayName"`
			}
			if json.Unmarshal(e, &m) == nil && m.DisplayName != "" {
				envName = m.DisplayName
				break
			}
		}
	}

	// Cleanup: sweep each collection for the marker and delete by id. Fails loudly
	// only if a delete cannot complete (true residue on the live tenant).
	sweep := map[string]func(context.Context, string) error{
		"customLists":        c.DeleteCustomList,
		"socRoles":           c.DeleteSocRole,
		"caseTagDefinitions": c.DeleteCaseTagDefinition,
		"environments":       c.DeleteEnvironment,
	}
	lists := map[string]func() ([]json.RawMessage, error){
		"customLists":        func() ([]json.RawMessage, error) { return c.ListCustomLists(ctx) },
		"socRoles":           func() ([]json.RawMessage, error) { return c.ListSocRoles(ctx) },
		"caseTagDefinitions": func() ([]json.RawMessage, error) { return c.ListCaseTagDefinitions(ctx) },
		"environments":       func() ([]json.RawMessage, error) { return c.ListEnvironments(ctx) },
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
					if err := sweep[res](ctx, id); err != nil {
						t.Errorf("CLEANUP FAILED to delete leftover %s/%s: %v", res, id, err)
					} else {
						t.Logf("cleanup: removed leftover %s/%s", res, id)
					}
				}
			}
		}
	})

	// full: create → confirm marker in GET → delete → confirm gone.
	full := func(name string,
		create func(context.Context, any) (json.RawMessage, error),
		get func(context.Context, string) (json.RawMessage, error),
		del func(context.Context, string) error, body map[string]any) {
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
		got, err := get(ctx, id)
		if err != nil {
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

	full("customLists", c.CreateCustomList, c.GetCustomList, c.DeleteCustomList, map[string]any{
		"category": marker, "entityIdentifier": marker, "environments": envName,
	})
	full("socRoles", c.CreateSocRole, c.GetSocRole, c.DeleteSocRole, map[string]any{
		"displayName": marker, "additionalRolesAccess": []string{},
	})
	full("caseTagDefinitions", c.CreateCaseTagDefinition, c.GetCaseTagDefinition, c.DeleteCaseTagDefinition, map[string]any{
		"displayName": marker, "matchCriteria": "BY_PRODUCT", "value": marker,
		"comparisonType": "EXACT", "priority": 1, "canBeCaseTitle": false,
	})

	// environments: prove the write endpoint is reachable. A license-quota 400 is
	// a correctly-enforced business rule, not a 500/failure.
	envBody := map[string]any{
		"displayName": marker, "description": "secopsctl smoke", "contact": "secopsctl",
		"contactEmails": "smoke@example.com", "contactPhone": "000",
		"retentionDuration": 1, "dynamicParameters": []any{},
	}
	if created, err := c.CreateEnvironment(ctx, envBody); err != nil {
		var te *transport.Error
		if errors.As(err, &te) && te.Status == 400 && strings.Contains(te.Body, "limit for the number of environments") {
			t.Logf("[environments] create reachable; license-quota 400 (endpoint works, tenant at cap)")
		} else {
			t.Errorf("[environments] unexpected create error (no 500 expected): %v", err)
		}
	} else {
		id := idFromRaw(created)
		if err := c.DeleteEnvironment(ctx, id); err != nil {
			t.Errorf("[environments] delete(%s): %v", id, err)
		} else {
			t.Logf("[environments] create→delete OK (id=%s) — v1alpha write works", id)
		}
	}
}
