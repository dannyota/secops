package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/internal/mirror/reconcile"
	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

// Alert-grouping rules as config-as-code on the MODERN v1alpha SOAR surface
// (siemplify-soar host, AppKey). Unlike the other SOAR reconcile surfaces (which
// ride the legacy jsonSurface adapter), grouping rules use the modern soar.Client,
// so this is a bespoke reconcile.Surface rather than a jsonSurfaceSpec. Identity is
// the server rule id; the diff basis is the writable config only (category /
// groupingType / entityType / categoryDetails) — the fields create/patch accept —
// so server-managed fields never produce a phantom diff.
//
// Prune: a rule has a clean delete-by-id (PruneEligible), but the catch-all
// fallback rule (category "ALL") cannot be deleted by the platform — the Delete
// closure refuses it so --prune never targets it (other orphans prune normally).

// groupingConfigFields are the writable config of an alert-grouping rule — the
// only fields create/patch accept and the only diff basis. Server identity
// (name/id) and any other server-managed field are excluded.
var groupingConfigFields = []string{"category", "groupingType", "entityType", "categoryDetails"}

// groupingFallbackCategory is the platform-managed catch-all rule. It cannot be
// deleted (only edited), so prune must never target it.
const groupingFallbackCategory = "ALL"

// groupingProjection extracts the writable config fields from a rule payload.
func groupingProjection(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(groupingConfigFields))
	for _, k := range groupingConfigFields {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

// canonicalGroupingRule is the diff basis: the writable config, canonicalized.
func canonicalGroupingRule(raw json.RawMessage) ([]byte, error) {
	proj, err := groupingProjection(raw)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(proj)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(b)
}

// groupingSlug builds a stable, human filename stem: the category plus the rule id
// (so multiple rules of one category never collide and the name never churns).
func groupingSlug(category, id string) string {
	base := Slugify(category)
	if base == "" {
		base = "rule"
	}
	if id == "" {
		return base
	}
	return base + "-" + fileSafeID(id)
}

// fileSafeID makes a server id safe in a filename.
func fileSafeID(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, s)
}

// GroupingRulesSurface exposes alert-grouping rules as config-as-code through the
// reconcile engine, backed by the modern soar.Client.
func GroupingRulesSurface(c *soar.Client) reconcile.Surface {
	build := func(r soar.AlertGroupingRule) (reconcile.Object, error) {
		canon, err := canonicalGroupingRule(r.Raw)
		if err != nil {
			return reconcile.Object{}, err
		}
		var cat struct {
			Category string `json:"category"`
		}
		_ = json.Unmarshal(r.Raw, &cat)
		return reconcile.Object{
			Slug:      groupingSlug(cat.Category, r.ID),
			ServerID:  r.ID,
			Canonical: canon,
			Raw:       r.Raw,
		}, nil
	}

	bodyFromCanonical := func(canon []byte) (map[string]any, []string, error) {
		var m map[string]any
		if err := json.Unmarshal(canon, &m); err != nil {
			return nil, nil, err
		}
		mask := make([]string, 0, len(m))
		for _, k := range groupingConfigFields {
			if _, ok := m[k]; ok {
				mask = append(mask, k)
			}
		}
		return m, mask, nil
	}

	return reconcile.Surface{
		Name:    "grouping",
		Dir:     "rules",
		Product: reconcile.ProductSOAR,
		Caps:    reconcile.Capabilities{WholeBodyWrite: true, NoEtag: true, PruneEligible: true},

		List: func(ctx context.Context) (reconcile.ListResult, error) {
			rules, err := c.ListAlertGroupingRules(ctx)
			if err != nil {
				return reconcile.ListResult{}, err
			}
			res := reconcile.ListResult{}
			for _, r := range rules {
				o, berr := build(r)
				if berr != nil {
					warnf("grouping rule %q: %v", r.ID, berr)
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
				b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
				if rerr != nil {
					return nil, rerr
				}
				id, _ := serverBlock(b)
				canon, cerr := canonicalGroupingRule(b)
				if cerr != nil {
					return nil, fmt.Errorf("grouping %s: %w", e.Name(), cerr)
				}
				objs = append(objs, reconcile.Object{
					Slug:      strings.TrimSuffix(e.Name(), ".json"),
					ServerID:  id,
					Canonical: canon,
				})
			}
			return objs, nil
		},

		Write: func(dir string, o reconcile.Object) error {
			if _, err := EnsureDir(dir); err != nil {
				return err
			}
			proj := map[string]any{}
			if len(o.Canonical) > 0 {
				if err := json.Unmarshal(o.Canonical, &proj); err != nil {
					return err
				}
			}
			proj["_server"] = map[string]any{"id": o.ServerID}
			b, err := json.MarshalIndent(proj, "", "  ")
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, o.Slug+".json"), append(b, '\n'), 0o644)
		},

		Create: func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
			body, _, err := bodyFromCanonical(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			created, cerr := c.CreateAlertGroupingRule(ctx, body)
			if cerr != nil {
				return reconcile.Object{}, cerr
			}
			return build(*created)
		},

		Update: func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
			body, mask, err := bodyFromCanonical(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			updated, uerr := c.UpdateAlertGroupingRule(ctx, live.ServerID, body, mask...)
			if uerr != nil {
				return reconcile.Object{}, uerr
			}
			return build(*updated)
		},

		Delete: func(ctx context.Context, live reconcile.Object) error {
			if isGroupingFallback(live.Canonical) {
				return fmt.Errorf("refusing to delete the catch-all fallback alert-grouping rule "+
					"(category %q): the platform does not allow deleting it, only editing — "+
					"restore its local file to keep it tracked", groupingFallbackCategory)
			}
			return c.DeleteAlertGroupingRule(ctx, live.ServerID)
		},
	}
}

// isGroupingFallback reports whether a rule's config is the catch-all fallback.
func isGroupingFallback(canon []byte) bool {
	var m struct {
		Category string `json:"category"`
	}
	if json.Unmarshal(canon, &m) != nil {
		return false
	}
	return strings.EqualFold(m.Category, groupingFallbackCategory)
}

// PullSOARGrouping snapshots alert-grouping rules (the modern v1alpha reconcile
// surface) into <outDir>/rules/<slug>.json and the General/Overflow grouping
// settings singleton into <outDir>/settings.json. Returns the rules written.
// Prune removes local rule files with no live counterpart (engine-gated on a
// complete pull and never targets the non-deletable fallback rule).
func PullSOARGrouping(ctx context.Context, c *soar.Client, lc *legacy.Client, outDir string, prune bool) (int, error) {
	n, err := reconcile.Pull(ctx, GroupingRulesSurface(c), filepath.Join(outDir, "rules"), os.Stdout, reconcile.PullOpts{Prune: prune})
	if err != nil {
		return 0, err
	}
	writeGroupingSettings(ctx, c, lc, outDir)
	return n, nil
}

// writeGroupingSettings snapshots the General/Overflow grouping settings singleton
// to <outDir>/settings.json. The reliable source is the legacy AppKey config
// endpoint (Max-alerts-per-case / overflow); the modern moduleSettings bag is a
// fallback. A read failure is a warning, not fatal — the rules are the primary
// snapshot.
func writeGroupingSettings(ctx context.Context, c *soar.Client, lc *legacy.Client, outDir string) {
	if lc != nil {
		if raw, err := lc.SettingXGetMaximumAlertsGroupingConfiguration(ctx); err == nil && hasJSONContent(raw) {
			// The endpoint returns a bare scalar (the max-alerts-per-case value);
			// wrap it under a descriptive key so the file is self-documenting. A
			// JSON object response is written as-is. (Timeframe/overflow/co-grouping
			// are not exposed by this endpoint — edit them in SOAR Settings.)
			body := raw
			if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
				wrapped, werr := json.Marshal(map[string]json.RawMessage{"maximumAlertsGroupingConfiguration": raw})
				if werr == nil {
					body = wrapped
				}
			}
			if werr := writeIndentedJSON(filepath.Join(outDir, "settings.json"), body); werr != nil {
				warnf("grouping settings write: %v", werr)
			}
			return
		} else if err != nil {
			warnf("grouping settings (legacy config): %v", err)
		}
	}
	// Fallback: the modern moduleSettings property bag.
	if c == nil {
		return
	}
	props, err := c.ListModuleSettingProperties(ctx, "AlertGroupingSettings")
	if err != nil {
		warnf("grouping settings (moduleSettings): %v", err)
		return
	}
	if len(props) == 0 {
		return
	}
	settings := make(map[string]string, len(props))
	for _, p := range props {
		settings[p.Name] = p.Value
	}
	b, err := json.Marshal(settings)
	if err != nil {
		warnf("grouping settings marshal: %v", err)
		return
	}
	if werr := writeIndentedJSON(filepath.Join(outDir, "settings.json"), b); werr != nil {
		warnf("grouping settings write: %v", werr)
	}
}

// hasJSONContent reports whether raw is non-empty and not an empty object/array.
func hasJSONContent(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "{}" && s != "[]" && s != "null"
}
