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

// Operational-config SOAR surfaces on the reliable legacy AppKey path:
// connectors (ingestion sources) and jobs (scheduled automation) — the config the
// modern v1alpha pull+patch covered only partially — plus form-dynamic-parameters
// (close-case form fields). They ride the same reconcile engine + jsonSurface
// adapter as the settings surfaces in registry_soar.go.

// connectorsSurface: connector instances (ingestion sources) as config-as-code,
// full CUD keyed by `identifier`. SaveConnector is the upsert for BOTH create and
// update: the create path triggers when the body has NO `identifier` (the server
// assigns one) — sending a client-assigned identifier routes to the update path
// (which 404s for an id that doesn't exist yet). A new connector file naturally
// omits `identifier` (and the reserved `_server` block is stripped), so the engine
// create works; the operator must supply the connector's mandatory parameters.
// GetConnector returns the full instance (secret parameter values arrive
// server-masked as "***…", which the server reads as "keep existing" on save, so
// the whole-body overlay round-trips them unchanged); DeleteConnector is a clean
// by-id delete (PruneEligible). extraStrip drops the definition-version/runtime
// fields the full body carries (`version`/`isUpdateAvailable`/
// `loggingEnabledUntilUnixMs`/`isCustom`) so a pull→push round-trips clean
// (`version` is also stripped from the nested `integration` object at any depth).
func connectorsSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "connectors", dir: DirSOARConnectors,
		product:    reconcile.ProductSOAR,
		idField:    "identifier",
		nameField:  "displayName",
		extraStrip: []string{"version", "isUpdateAvailable", "loggingEnabledUntilUnixMs", "isCustom"},
		caps:       reconcile.Capabilities{WholeBodyWrite: true, PruneEligible: true},
		list:       flattenedConnectorCards(lc),
		getOne:     lc.GetConnector,
		create:     lc.SaveConnector,
		update:     lc.SaveConnector,
		del:        lc.DeleteConnector,
	})
}

// connectorAllowlistSurface projects only the connector alert allow-list into a
// separate reconcile target. The source of truth is still the connector instance:
// writes fetch the current full connector body, replace allowList, and save it
// back so parameters/secrets/runtime settings are preserved.
func connectorAllowlistSurface(lc *legacy.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "connector-allowlist",
		Dir:     DirSOARConnAllowlist,
		Product: reconcile.ProductSOAR,
		Caps:    reconcile.Capabilities{NoDelete: true, WholeBodyWrite: true},
		List: func(ctx context.Context) (reconcile.ListResult, error) {
			raw, err := flattenedConnectorCards(lc)(ctx)
			if err != nil {
				return reconcile.ListResult{}, err
			}
			cards, err := decodeRawList(raw)
			if err != nil {
				return reconcile.ListResult{}, err
			}
			var objs []reconcile.Object
			incomplete := false
			for _, card := range cards {
				id := jsonField(card, "identifier")
				if id == "" {
					incomplete = true
					warnf("connector-allowlist: card missing identifier")
					continue
				}
				full, gerr := lc.GetConnector(ctx, id)
				if gerr != nil {
					incomplete = true
					warnf("connector-allowlist: get %q: %v", id, gerr)
					continue
				}
				obj, berr := buildConnectorAllowlistObject(full)
				if berr != nil {
					incomplete = true
					warnf("connector-allowlist: build %q: %v", id, berr)
					continue
				}
				objs = append(objs, obj)
			}
			return reconcile.ListResult{Objects: objs, Incomplete: incomplete}, nil
		},
		LoadDir: loadConnectorAllowlists,
		Write:   writeConnectorAllowlist,
		Update: func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
			full := live.Raw
			if live.ServerID != "" {
				refreshed, err := lc.GetConnector(ctx, live.ServerID)
				if err != nil {
					return reconcile.Object{}, err
				}
				full = refreshed
			}
			body, err := applyConnectorAllowlist(full, local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			if _, err := lc.SaveConnector(ctx, body); err != nil {
				return reconcile.Object{}, err
			}
			after, err := lc.GetConnector(ctx, live.ServerID)
			if err != nil {
				return reconcile.Object{}, err
			}
			return buildConnectorAllowlistObject(after)
		},
	}
}

// flattenedConnectorCards adapts ListConnectorCards, whose response groups the
// cards by integration ([{integration, cards:[...]}, …]), into the flat
// connector-card list the engine expects. It tolerates a flat response too (an
// item that is itself a card), so it works regardless of grouping.
func flattenedConnectorCards(lc *legacy.Client) rawListFn {
	return func(ctx context.Context) (json.RawMessage, error) {
		raw, err := lc.ListConnectorCards(ctx)
		if err != nil {
			return nil, err
		}
		groups, err := decodeRawList(raw)
		if err != nil {
			return nil, err
		}
		var cards []json.RawMessage
		for _, g := range groups {
			var gm struct {
				Cards []json.RawMessage `json:"cards"`
			}
			if json.Unmarshal(g, &gm) == nil && len(gm.Cards) > 0 {
				cards = append(cards, gm.Cards...)
				continue
			}
			if jsonField(g, "identifier") != "" { // already a flat card
				cards = append(cards, g)
			}
		}
		return json.Marshal(cards)
	}
}

func buildConnectorAllowlistObject(full json.RawMessage) (reconcile.Object, error) {
	proj, err := connectorAllowlistProjection(full)
	if err != nil {
		return reconcile.Object{}, err
	}
	canon, err := connectorAllowlistCanonical(proj)
	if err != nil {
		return reconcile.Object{}, err
	}
	id := jsonField(full, "identifier")
	name := jsonField(full, "displayName")
	if id == "" {
		return reconcile.Object{}, fmt.Errorf("connector has no identifier")
	}
	if name == "" {
		name = id
	}
	return reconcile.Object{Slug: Slugify(name), ServerID: id, Canonical: canon, Raw: full}, nil
}

func connectorAllowlistProjection(full json.RawMessage) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(full, &m); err != nil {
		return nil, err
	}
	allow, err := parseConnectorAllowlistValues(m["allowList"])
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"displayName":             stringField(m, "displayName"),
		"connectorDefinitionName": stringField(m, "connectorDefinitionName"),
		"environment":             connectorEnvironment(m["environment"]),
		"integration":             connectorIntegration(m["integration"]),
		"isAllowlistSupported":    boolFieldValue(m["isAllowlistSupported"]),
		"allowList":               allow,
	}
	return json.Marshal(out)
}

func connectorAllowlistCanonical(raw json.RawMessage) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	allow, err := parseConnectorAllowlistValues(m["allowList"])
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"allowList": allow})
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(body)
}

func loadConnectorAllowlists(dir string) ([]reconcile.Object, error) {
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
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		id, _ := serverBlock(b)
		canon, err := connectorAllowlistCanonical(b)
		if err != nil {
			return nil, fmt.Errorf("connector-allowlist %s: %w", e.Name(), err)
		}
		objs = append(objs, reconcile.Object{
			Slug:      strings.TrimSuffix(e.Name(), ".json"),
			ServerID:  id,
			Canonical: canon,
		})
	}
	return objs, nil
}

func writeConnectorAllowlist(dir string, o reconcile.Object) error {
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	var fields map[string]any
	if len(o.Raw) > 0 {
		proj, err := connectorAllowlistProjection(o.Raw)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(proj, &fields); err != nil {
			return err
		}
	} else {
		if err := json.Unmarshal(o.Canonical, &fields); err != nil {
			return err
		}
	}
	fields["_server"] = map[string]any{"id": o.ServerID}
	b, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, o.Slug+".json"), append(b, '\n'), 0o644)
}

func applyConnectorAllowlist(full, localCanonical json.RawMessage) (json.RawMessage, error) {
	var body map[string]any
	if err := json.Unmarshal(full, &body); err != nil {
		return nil, err
	}
	allow, err := connectorAllowlistFromCanonical(localCanonical)
	if err != nil {
		return nil, err
	}
	if !boolFieldValue(body["isAllowlistSupported"]) && len(allow) > 0 {
		return nil, fmt.Errorf("connector %q does not support allowList", stringField(body, "displayName"))
	}
	body["allowList"] = allow
	out, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func connectorAllowlistFromCanonical(raw json.RawMessage) ([]string, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return parseConnectorAllowlistValues(m["allowList"])
}

func parseConnectorAllowlistValues(v any) ([]string, error) {
	switch t := v.(type) {
	case nil:
		return []string{}, nil
	case []string:
		return append([]string(nil), t...), nil
	case []any:
		out := make([]string, 0, len(t))
		for i, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("allowList[%d] is %T, want string", i, item)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("allowList is %T, want array", v)
	}
}

func stringField(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func boolFieldValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func connectorEnvironment(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		for _, key := range []string{"name", "displayName"} {
			if s, ok := t[key].(string); ok {
				return s
			}
		}
	}
	return ""
}

func connectorIntegration(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for _, key := range []string{"identifier", "displayName"} {
			if s, ok := m[key].(string); ok && s != "" {
				out[key] = s
			}
		}
	}
	return out
}

// jobsSurface: installed jobs (scheduled background automation) as config-as-code.
// The installed-jobs list item IS the full write body (read DTO == write DTO), so
// there is no getOne; SaveOrUpdateJob is a whole-body upsert and re-sending the
// full read body updates the job in place. Delete takes a body (DeleteJobData),
// not a clean id, so the surface is additive (NoDelete) — remove a job via
// `soar legacy call jobs/DeleteJobData`. Engine Create is NOT wired: a create must
// send a TRIMMED template body (echoing the read-only/audit fields the list item
// carries is rejected), which the whole-body adapter cannot do. extraStrip drops
// the run-state/version fields the server stamps so a pull→push round-trips clean.
func jobsSurface(lc *legacy.Client) reconcile.Surface {
	return jsonSurface(jsonSurfaceSpec{
		name: "jobs", dir: DirSOARJobs,
		product:    reconcile.ProductSOAR,
		idField:    "uniqueIdentifier",
		nameField:  "name",
		extraStrip: []string{"lastRunStatus", "lastRunTime", "version", "creator"},
		caps:       reconcile.Capabilities{WholeBodyWrite: true, NoDelete: true},
		list:       lc.ListInstalledJobs,
		update:     lc.SaveOrUpdateJob,
	})
}

// NOTE: form-dynamic-parameters (close-case form fields) was investigated as a
// reconcile surface but DEFERRED — its strict PUT update silently resets the
// parameter's formType to Invalid (dropping it out of its form) even when given the
// integer-enum body the UI uses, so a reconcile update is not safe. The surface is
// reachable read-only via `soar legacy call settings/form-dynamic-parameters?formType=CloseCase`.
