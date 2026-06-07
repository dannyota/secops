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

// Custom SOC metrics (metricDefinitions) as code, on the reconcile engine. A
// metric definition is a typed object: an id (its display name), a YARA-L 2.0
// textDefinition, and a state (ENABLED/DISABLED). Two API constraints shape the
// surface:
//
//   - textDefinition is IMMUTABLE and patch updates ONLY state. So Update can flip
//     the state but must refuse a textDefinition edit — changing a metric means a
//     new id (create), since the id is the display name.
//   - There is NO delete API (the v1alpha method set is create/get/list/patch) →
//     NoDelete: a removed local file is reported as drift, never pruned, like
//     reference_lists and rule_exclusions.
//
// On disk each metric is one `<slug>.yaml` (display_name, name, state, and the
// YARA-L text as a block scalar), pulled and pushed through the engine.

// metricState defaults a blank state to ENABLED — the server's create default —
// so a pulled metric and a state-omitting local file canonicalize equal.
func metricState(s string) string {
	if s == "" {
		return string(chronicle.MetricEnabled)
	}
	return strings.ToUpper(s)
}

// metricSpec is the diff basis: the operator-editable metric config. Server-
// derived fields (description, author, timestamps, extracted match/outcome
// variables) are excluded so they never appear in a diff.
type metricSpec struct {
	DisplayName    string `json:"display_name"`
	State          string `json:"state,omitempty"`
	TextDefinition string `json:"text_definition"`
}

// metricMeta is the on-disk `<slug>.yaml` shape (spec + server identity).
type metricMeta struct {
	DisplayName    string `yaml:"display_name"`
	Name           string `yaml:"name"`
	State          string `yaml:"state"`
	TextDefinition string `yaml:"text_definition"`
}

func metricDefinitionsSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "metric_definitions",
		Dir:     DirMetrics,
		Product: reconcile.ProductSIEM,
		// No delete API → NoDelete (drift reported, never pruned). The patch carries
		// no etag.
		Caps: reconcile.Capabilities{NoDelete: true, NoEtag: true},

		List: func(ctx context.Context) (reconcile.ListResult, error) {
			defs, err := c.ListMetricDefinitions(ctx)
			if err != nil {
				return reconcile.ListResult{}, err
			}
			res := reconcile.ListResult{}
			for i := range defs {
				o, berr := metricObject(defs[i])
				if berr != nil {
					warnf("metric_definitions: build %s: %v", defs[i].ID(), berr)
					res.Incomplete = true
					continue
				}
				res.Objects = append(res.Objects, o)
			}
			return res, nil
		},

		LoadDir: loadMetricDefinitions,
		Write:   writeMetricDefinition,

		Create: func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
			spec, err := decodeMetricSpec(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			m, err := c.CreateMetricDefinition(ctx, spec.DisplayName, spec.TextDefinition, chronicle.MetricDefinitionState(spec.State))
			if err != nil {
				return reconcile.Object{}, err
			}
			return metricObject(*m)
		},

		Update: func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
			want, err := decodeMetricSpec(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			have, err := decodeMetricSpec(live.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			// Only state can be patched. The id (display_name) and textDefinition are
			// immutable — a change to either must become a new metric id, so refuse
			// rather than silently no-op it (patch would otherwise touch only state).
			if want.TextDefinition != have.TextDefinition {
				return reconcile.Object{}, fmt.Errorf(
					"metric_definitions: %q text_definition is immutable — rename to a new id to change the metric (state is the only updatable field)",
					want.DisplayName)
			}
			if want.DisplayName != have.DisplayName {
				return reconcile.Object{}, fmt.Errorf(
					"metric_definitions: display_name (the id) is immutable — create a new id instead of renaming %q to %q",
					have.DisplayName, want.DisplayName)
			}
			m, err := c.SetMetricDefinitionState(ctx, lastSegment(live.ServerID), chronicle.MetricDefinitionState(want.State))
			if err != nil {
				return reconcile.Object{}, err
			}
			return metricObject(*m)
		},
	}
}

// metricObject builds the engine object (canonical diff basis + identity) for a
// live metric definition.
func metricObject(m chronicle.MetricDefinition) (reconcile.Object, error) {
	display := m.ID()
	canon, err := canonicalMetric(metricSpec{
		DisplayName:    display,
		State:          metricState(string(m.State)),
		TextDefinition: m.TextDefinition,
	})
	if err != nil {
		return reconcile.Object{}, err
	}
	return reconcile.Object{Slug: Slugify(display), ServerID: m.Name, Canonical: canon}, nil
}

func loadMetricDefinitions(dir string) ([]reconcile.Object, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var objs []reconcile.Object
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		var meta metricMeta
		if rerr := readYAMLFile(filepath.Join(dir, e.Name()), &meta); rerr != nil {
			return nil, rerr
		}
		canon, cerr := canonicalMetric(metricSpec{
			DisplayName:    meta.DisplayName,
			State:          metricState(meta.State),
			TextDefinition: meta.TextDefinition,
		})
		if cerr != nil {
			return nil, cerr
		}
		objs = append(objs, reconcile.Object{
			Slug:      strings.TrimSuffix(e.Name(), ".yaml"),
			ServerID:  meta.Name,
			Canonical: canon,
		})
	}
	return objs, nil
}

// writeMetricDefinition renders one object back to `<slug>.yaml`.
func writeMetricDefinition(dir string, o reconcile.Object) error {
	spec, err := decodeMetricSpec(o.Canonical)
	if err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	return writeYAML(filepath.Join(dir, o.Slug+".yaml"), metricMeta{
		DisplayName:    spec.DisplayName,
		Name:           o.ServerID,
		State:          spec.State,
		TextDefinition: spec.TextDefinition,
	})
}

func canonicalMetric(spec metricSpec) ([]byte, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(raw)
}

func decodeMetricSpec(canonical []byte) (metricSpec, error) {
	var spec metricSpec
	err := json.Unmarshal(canonical, &spec)
	return spec, err
}
