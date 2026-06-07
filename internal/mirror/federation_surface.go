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

// federationGroups (group a deployment's subtenant instances) as code, on the
// reconcile engine. SIEM/ADC, full CRUD — create (server-assigns the id), update
// (PATCH), delete (prune-eligible), NoEtag. Meaningful only on MSSP / multi-tenant
// deployments; on a single-tenant instance the list is empty.
//
// On disk each group is one `<slug>.yaml` (display_name, name, type,
// federated_instances). federated_instances are subtenant instance resource names.

// federationGroupType defaults a blank type to FEDERATION_GROUP_TYPE_DEFAULT (the
// only non-unspecified value) so a pulled group and a type-omitting file match.
func federationGroupType(s string) string {
	if s == "" {
		return string(chronicle.FederationGroupDefault)
	}
	return strings.ToUpper(s)
}

// federationGroupSpec is the diff basis: the operator-editable group config.
type federationGroupSpec struct {
	DisplayName        string   `json:"display_name"`
	Type               string   `json:"type,omitempty"`
	FederatedInstances []string `json:"federated_instances,omitempty"`
}

// federationGroupMeta is the on-disk `<slug>.yaml` shape (spec + server identity).
type federationGroupMeta struct {
	DisplayName        string   `yaml:"display_name"`
	Name               string   `yaml:"name"`
	Type               string   `yaml:"type"`
	FederatedInstances []string `yaml:"federated_instances"`
}

func federationGroupsSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "federation_groups",
		Dir:     DirFederation,
		Product: reconcile.ProductSIEM,
		Caps:    reconcile.Capabilities{PruneEligible: true, NoEtag: true},

		List: func(ctx context.Context) (reconcile.ListResult, error) {
			groups, err := c.ListFederationGroups(ctx)
			if err != nil {
				return reconcile.ListResult{}, err
			}
			res := reconcile.ListResult{}
			for i := range groups {
				o, berr := federationGroupObject(groups[i])
				if berr != nil {
					warnf("federation_groups: build %s: %v", groups[i].ID(), berr)
					res.Incomplete = true
					continue
				}
				res.Objects = append(res.Objects, o)
			}
			return res, nil
		},

		LoadDir: loadFederationGroups,
		Write:   writeFederationGroupObject,

		Create: func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
			spec, err := decodeFederationGroupSpec(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			created, err := c.CreateFederationGroup(ctx, federationGroupFromSpec(spec))
			if err != nil {
				return reconcile.Object{}, err
			}
			return federationGroupObject(*created)
		},

		Update: func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
			spec, err := decodeFederationGroupSpec(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			body, err := json.Marshal(federationGroupFromSpec(spec))
			if err != nil {
				return reconcile.Object{}, err
			}
			// Mask only the keys present in the canonical — a fixed mask plus the
			// SDK body's omitempty federatedInstances would clear the group's
			// instance membership when an unrelated field is edited.
			updated, err := c.UpdateFederationGroup(ctx, lastSegment(live.ServerID), body, federationUpdateMask(local.Canonical))
			if err != nil {
				return reconcile.Object{}, err
			}
			return federationGroupObject(*updated)
		},

		Delete: func(ctx context.Context, live reconcile.Object) error {
			return c.DeleteFederationGroup(ctx, lastSegment(live.ServerID))
		},
	}
}

// federationUpdateMask returns the updateMask for exactly the writable keys
// present in the canonical (snake_case, which are already the mask paths), so a
// PATCH never clears a field the operator's file omitted.
func federationUpdateMask(canonical []byte) []string {
	var m map[string]json.RawMessage
	if json.Unmarshal(canonical, &m) != nil {
		return nil
	}
	var mask []string
	for _, k := range []string{"display_name", "type", "federated_instances"} {
		if _, ok := m[k]; ok {
			mask = append(mask, k)
		}
	}
	return mask
}

func federationGroupFromSpec(spec federationGroupSpec) chronicle.FederationGroup {
	return chronicle.FederationGroup{
		DisplayName:        spec.DisplayName,
		Type:               chronicle.FederationGroupType(federationGroupType(spec.Type)),
		FederatedInstances: spec.FederatedInstances,
	}
}

func federationGroupObject(g chronicle.FederationGroup) (reconcile.Object, error) {
	display := g.DisplayName
	if display == "" {
		display = g.ID()
	}
	canon, err := canonicalFederationGroup(federationGroupSpec{
		DisplayName:        display,
		Type:               federationGroupType(string(g.Type)),
		FederatedInstances: g.FederatedInstances,
	})
	if err != nil {
		return reconcile.Object{}, err
	}
	return reconcile.Object{Slug: Slugify(display), ServerID: g.Name, Canonical: canon}, nil
}

func loadFederationGroups(dir string) ([]reconcile.Object, error) {
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
		var meta federationGroupMeta
		if rerr := readYAMLFile(filepath.Join(dir, e.Name()), &meta); rerr != nil {
			return nil, rerr
		}
		canon, cerr := canonicalFederationGroup(federationGroupSpec{
			DisplayName:        meta.DisplayName,
			Type:               federationGroupType(meta.Type),
			FederatedInstances: meta.FederatedInstances,
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

func writeFederationGroupObject(dir string, o reconcile.Object) error {
	spec, err := decodeFederationGroupSpec(o.Canonical)
	if err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	return writeYAML(filepath.Join(dir, o.Slug+".yaml"), federationGroupMeta{
		DisplayName:        spec.DisplayName,
		Name:               o.ServerID,
		Type:               spec.Type,
		FederatedInstances: spec.FederatedInstances,
	})
}

func canonicalFederationGroup(spec federationGroupSpec) ([]byte, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(raw)
}

func decodeFederationGroupSpec(canonical []byte) (federationGroupSpec, error) {
	var spec federationGroupSpec
	err := json.Unmarshal(canonical, &spec)
	return spec, err
}
