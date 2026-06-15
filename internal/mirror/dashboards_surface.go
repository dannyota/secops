package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// dashboards (native Chronicle dashboards) as code, on the reconcile engine.
// Only CUSTOM dashboards are managed — CURATED dashboards are Google-managed with
// no create/update path, so they are excluded from the surface entirely (not
// pulled here, never planned for create/update/delete).
//
// On disk each dashboard is one `<slug>.json`: its canonical config (displayName,
// description, access, type, definition{filters,charts}) plus a reserved `_server`
// identity block. Charts live inline under definition.charts and are replaced
// wholesale on update, so the dry-run preview is essential. The dashboard PATCH
// carries no etag (NoEtag); access is immutable after create.

// dashboardExtraStrip are dashboard-specific volatile keys dropped from the diff
// basis at any depth. `dashboardUserData` is per-user view state (saved filters,
// layout) that changes per viewer. The create/update actor ids (createUserId /
// updateUserId) are stripped globally by the engine (reconcile.actorKeys), so they
// are not listed here.
var dashboardExtraStrip = []string{"dashboardUserData"}

// dashboardConfig is the writable subset parsed out of the canonical body to
// drive Create/Update.
type dashboardConfig struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Access      string `json:"access"`
	Definition  struct {
		Filters []json.RawMessage `json:"filters"`
		Charts  []json.RawMessage `json:"charts"`
	} `json:"definition"`
}

func dashboardsSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "dashboards",
		Dir:     DirDashboards,
		Product: reconcile.ProductSIEM,
		// No dashboard-level etag round-trip; delete loses a whole dashboard
		// (high-blast) → not prune-eligible.
		Caps: reconcile.Capabilities{NoEtag: true},

		List:    dashboardsList(c),
		LoadDir: loadDashboards,
		Write:   writeDashboardObject,
		Create:  dashboardsCreate(c),
		Update:  dashboardsUpdate(c),
		Delete: func(ctx context.Context, live reconcile.Object) error {
			return c.DeleteDashboard(ctx, lastSegment(live.ServerID))
		},
	}
}

// dashboardsList reads every CUSTOM dashboard in FULL view (the BASIC listing
// omits the definition) into engine objects. CURATED dashboards are skipped.
func dashboardsList(c *chronicle.Client) func(context.Context) (reconcile.ListResult, error) {
	return func(ctx context.Context) (reconcile.ListResult, error) {
		list, err := c.ListNativeDashboards(ctx)
		if err != nil {
			return reconcile.ListResult{}, err
		}
		res := reconcile.ListResult{}
		for _, d := range list {
			if d.Type != "CUSTOM" {
				continue // CURATED dashboards are read-only/unmanaged
			}
			full, gerr := c.GetDashboard(ctx, lastSegment(d.Name), true)
			if gerr != nil {
				warnf("dashboards: get %s full: %v", d.DisplayName, gerr)
				res.Incomplete = true
				continue
			}
			o, berr := buildDashboardObject(full.Raw)
			if berr != nil {
				warnf("dashboards: build %s: %v", d.DisplayName, berr)
				res.Incomplete = true
				continue
			}
			res.Objects = append(res.Objects, o)
		}
		return res, nil
	}
}

// dashboardCanonical strips the root resource name (identity, carried in
// ServerID) and then canonicalizes. The name is removed root-only — a nested
// "name" inside a chart/filter is preserved — so it can't be done via extraStrip
// (which drops at any depth).
func dashboardCanonical(raw json.RawMessage) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "name")
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(b, dashboardExtraStrip...)
}

// buildDashboardObject canonicalizes a full dashboard body and extracts identity.
func buildDashboardObject(raw json.RawMessage) (reconcile.Object, error) {
	canon, err := dashboardCanonical(raw)
	if err != nil {
		return reconcile.Object{}, err
	}
	id := jsonField(raw, "name")
	display := jsonField(raw, "displayName")
	slug := display
	if slug == "" {
		slug = lastSegment(id)
	}
	if id == "" {
		return reconcile.Object{}, fmt.Errorf("dashboard has no resource name")
	}
	return reconcile.Object{Slug: Slugify(slug), ServerID: id, Canonical: canon, Raw: raw}, nil
}

func loadDashboards(dir string) ([]reconcile.Object, error) {
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
		canon, cerr := dashboardCanonical(b)
		if cerr != nil {
			return nil, fmt.Errorf("dashboards: canonicalize %s: %w", e.Name(), cerr)
		}
		objs = append(objs, reconcile.Object{
			Slug:      strings.TrimSuffix(e.Name(), ".json"),
			ServerID:  id,
			Canonical: canon,
		})
	}
	return objs, nil
}

// writeDashboardObject renders the canonical config plus the `_server` identity
// block to `<slug>.json`.
func writeDashboardObject(dir string, o reconcile.Object) error {
	fields := map[string]any{}
	if len(o.Canonical) > 0 {
		if err := json.Unmarshal(o.Canonical, &fields); err != nil {
			return err
		}
	}
	fields["_server"] = map[string]any{"id": o.ServerID}
	b, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, o.Slug+".json"), append(b, '\n'), 0o644)
}

func dashboardsCreate(c *chronicle.Client) func(context.Context, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
		cfg, err := parseDashboardConfig(local.Canonical)
		if err != nil {
			return reconcile.Object{}, err
		}
		created, err := c.CreateDashboard(ctx, cfg.DisplayName, cfg.Description, cfg.Access,
			cfg.Definition.Filters, cfg.Definition.Charts)
		if err != nil {
			return reconcile.Object{}, err
		}
		full, err := c.GetDashboard(ctx, lastSegment(created.Name), true)
		if err != nil {
			return reconcile.Object{}, err
		}
		return buildDashboardObject(full.Raw)
	}
}

func dashboardsUpdate(c *chronicle.Client) func(context.Context, reconcile.Object, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
		want, err := parseDashboardConfig(local.Canonical)
		if err != nil {
			return reconcile.Object{}, err
		}
		have, err := parseDashboardConfig(live.Canonical)
		if err != nil {
			return reconcile.Object{}, err
		}
		if want.Access != have.Access {
			return reconcile.Object{}, fmt.Errorf(
				"dashboards: access of %q changed (%s -> %s); access is immutable after create — recreate the dashboard to change it",
				lastSegment(live.ServerID), have.Access, want.Access)
		}
		id := lastSegment(live.ServerID)
		upd := chronicle.DashboardUpdate{
			DisplayName: &want.DisplayName,
			Description: &want.Description,
			Filters:     nonNilRaw(want.Definition.Filters),
			Charts:      nonNilRaw(want.Definition.Charts),
		}
		if _, err := c.UpdateDashboard(ctx, id, upd); err != nil {
			return reconcile.Object{}, err
		}
		full, err := c.GetDashboard(ctx, id, true)
		if err != nil {
			return reconcile.Object{}, err
		}
		return buildDashboardObject(full.Raw)
	}
}

// --- helpers ----------------------------------------------------------------

func parseDashboardConfig(canonical []byte) (dashboardConfig, error) {
	var cfg dashboardConfig
	err := json.Unmarshal(canonical, &cfg)
	return cfg, err
}

// nonNilRaw guarantees a non-nil slice so an absent filters/charts is sent as an
// explicit empty replacement (a true wholesale replace) rather than "unchanged".
func nonNilRaw(s []json.RawMessage) []json.RawMessage {
	if s == nil {
		return []json.RawMessage{}
	}
	return s
}
