package mirror

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// rule exclusions (findings refinements) as code, on the reconcile engine. A
// rule exclusion is a typed object — display name, type, and a UDM query that
// suppresses matching detections. It has Create and Update (PATCH) but NO delete
// API, so the surface is NoDelete (deletions surface as drift, never removed) —
// like reference_lists. Deployment state (enabled/archived) is a separate
// sub-resource, but it is mirrored so dry-runs show active/disabled/archived drift.
//
// On disk each exclusion is one `<slug>.yaml` (display_name, name, type, query,
// deployment), pulled and pushed through the engine (there is no separate legacy
// puller).

// ruleExclusionSpec is the diff basis: the meaningful exclusion config.
type ruleExclusionSpec struct {
	DisplayName string                       `json:"display_name"`
	Type        string                       `json:"type,omitempty"`
	Query       string                       `json:"query"`
	Deployment  *ruleExclusionDeploymentSpec `json:"deployment,omitempty"`
}

type ruleExclusionDeploymentSpec struct {
	Enabled  bool `json:"enabled" yaml:"enabled"`
	Archived bool `json:"archived" yaml:"archived"`
}

// ruleExclusionMeta is the on-disk `<slug>.yaml` shape (spec + server identity).
type ruleExclusionMeta struct {
	DisplayName string                       `yaml:"display_name"`
	Name        string                       `yaml:"name"`
	Type        string                       `yaml:"type,omitempty"`
	Query       string                       `yaml:"query"`
	Deployment  *ruleExclusionDeploymentSpec `yaml:"deployment,omitempty"`
}

func ruleExclusionsSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "rule_exclusions",
		Dir:     DirRuleExcl,
		Product: reconcile.ProductSIEM,
		// No delete API → NoDelete (drift is reported, never pruned). The
		// refinement PATCH carries no etag.
		Caps: reconcile.Capabilities{NoDelete: true, NoEtag: true},

		List: func(ctx context.Context) (reconcile.ListResult, error) {
			exes, err := c.ListRuleExclusions(ctx)
			if err != nil {
				return reconcile.ListResult{}, err
			}
			res := reconcile.ListResult{}
			for i := range exes {
				dep, derr := c.GetRuleExclusionDeployment(ctx, exes[i].ID())
				if derr != nil {
					return reconcile.ListResult{}, derr
				}
				o, berr := ruleExclusionObject(exes[i], dep)
				if berr != nil {
					warnf("rule_exclusions: build %s: %v", exes[i].DisplayName, berr)
					res.Incomplete = true
					continue
				}
				res.Objects = append(res.Objects, o)
			}
			return res, nil
		},

		LoadDir: loadRuleExclusions,
		Write:   writeRuleExclusion,

		Create: func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
			spec, err := decodeRuleExclusionSpec(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			ex, err := c.CreateRuleExclusion(ctx, spec.DisplayName, chronicle.RuleExclusionType(spec.Type), spec.Query)
			if err != nil {
				return reconcile.Object{}, err
			}
			var dep *chronicle.RuleExclusionDeployment
			if spec.Deployment != nil {
				dep, err = c.UpdateRuleExclusionDeployment(ctx, ex.ID(), ruleExclusionDeploymentUpdate(*spec.Deployment))
				if err != nil {
					return reconcile.Object{}, err
				}
			} else {
				dep, _ = c.GetRuleExclusionDeployment(ctx, ex.ID())
			}
			return ruleExclusionObject(*ex, dep)
		},

		Update: func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
			spec, err := decodeRuleExclusionSpec(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			liveSpec, err := decodeRuleExclusionSpec(live.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			ex := chronicle.RuleExclusion{
				Name:        live.ServerID,
				DisplayName: liveSpec.DisplayName,
				Type:        chronicle.RuleExclusionType(liveSpec.Type),
				Query:       liveSpec.Query,
			}
			if ruleExclusionCoreChanged(spec, liveSpec) {
				patched, perr := c.PatchRuleExclusion(ctx, lastSegment(live.ServerID), chronicle.RuleExclusionUpdate{
					DisplayName: spec.DisplayName,
					Type:        chronicle.RuleExclusionType(spec.Type),
					Query:       spec.Query,
				})
				if perr != nil {
					return reconcile.Object{}, perr
				}
				ex = *patched
			}
			depSpec := liveSpec.Deployment
			if spec.Deployment != nil && ruleExclusionDeploymentChanged(spec.Deployment, liveSpec.Deployment) {
				dep, derr := c.UpdateRuleExclusionDeployment(ctx, lastSegment(live.ServerID), ruleExclusionDeploymentUpdate(*spec.Deployment))
				if derr != nil {
					return reconcile.Object{}, derr
				}
				depSpec = ruleExclusionDeploymentFromLive(dep)
			}
			return ruleExclusionObjectFromSpec(ruleExclusionSpec{
				DisplayName: ex.DisplayName,
				Type:        string(ex.Type),
				Query:       ex.Query,
				Deployment:  depSpec,
			}, ex.Name)
		},
	}
}

// ruleExclusionObject builds the engine object for a live exclusion.
func ruleExclusionObject(ex chronicle.RuleExclusion, dep *chronicle.RuleExclusionDeployment) (reconcile.Object, error) {
	return ruleExclusionObjectFromSpec(ruleExclusionSpec{
		DisplayName: ex.DisplayName,
		Type:        string(ex.Type),
		Query:       ex.Query,
		Deployment:  ruleExclusionDeploymentFromLive(dep),
	}, ex.Name)
}

func ruleExclusionObjectFromSpec(spec ruleExclusionSpec, serverID string) (reconcile.Object, error) {
	canon, err := canonicalRuleExclusion(spec)
	if err != nil {
		return reconcile.Object{}, err
	}
	display := spec.DisplayName
	if display == "" {
		display = lastSegment(serverID)
	}
	return reconcile.Object{Slug: Slugify(display), ServerID: serverID, Canonical: canon}, nil
}

func loadRuleExclusions(dir string) ([]reconcile.Object, error) {
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
		var meta ruleExclusionMeta
		if rerr := readYAMLFile(filepath.Join(dir, e.Name()), &meta); rerr != nil {
			return nil, rerr
		}
		canon, cerr := canonicalRuleExclusion(ruleExclusionSpec{
			DisplayName: meta.DisplayName,
			Type:        meta.Type,
			Query:       meta.Query,
			Deployment:  meta.Deployment,
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

// writeRuleExclusion renders one object to `<slug>.yaml`.
func writeRuleExclusion(dir string, o reconcile.Object) error {
	spec, err := decodeRuleExclusionSpec(o.Canonical)
	if err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	return writeYAML(filepath.Join(dir, o.Slug+".yaml"), ruleExclusionMeta{
		DisplayName: spec.DisplayName,
		Name:        o.ServerID,
		Type:        spec.Type,
		Query:       spec.Query,
		Deployment:  spec.Deployment,
	})
}

func ruleExclusionCoreChanged(want, have ruleExclusionSpec) bool {
	return want.DisplayName != have.DisplayName ||
		want.Type != have.Type ||
		want.Query != have.Query
}

func ruleExclusionDeploymentChanged(want, have *ruleExclusionDeploymentSpec) bool {
	if want == nil {
		return false
	}
	if have == nil {
		return true
	}
	return want.Enabled != have.Enabled || want.Archived != have.Archived
}

func ruleExclusionDeploymentFromLive(dep *chronicle.RuleExclusionDeployment) *ruleExclusionDeploymentSpec {
	if dep == nil {
		return nil
	}
	return &ruleExclusionDeploymentSpec{Enabled: dep.Enabled, Archived: dep.Archived}
}

func ruleExclusionDeploymentUpdate(spec ruleExclusionDeploymentSpec) chronicle.RuleExclusionDeploymentUpdate {
	return chronicle.RuleExclusionDeploymentUpdate{
		Enabled:  &spec.Enabled,
		Archived: &spec.Archived,
	}
}

func canonicalRuleExclusion(spec ruleExclusionSpec) ([]byte, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(raw)
}

func decodeRuleExclusionSpec(canonical []byte) (ruleExclusionSpec, error) {
	var spec ruleExclusionSpec
	err := json.Unmarshal(canonical, &spec)
	return spec, err
}
