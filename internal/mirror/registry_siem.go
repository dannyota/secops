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

// SIEM reconcile-lane config surfaces: typed Chronicle resources exposed as
// config-as-code through the SAME reconcile engine the SOAR side uses — proving
// the engine is product-neutral (it imports no SDK; the closures here do). The
// fan-out adds data_tables / feeds / parsers here one at a time; rules stay
// bespoke (YARA-L source + a deployment state machine, not a single body).
type siemSurfaceDef struct {
	name  string
	build func(*chronicle.Client) reconcile.Surface
}

var siemSurfaceDefs = []siemSurfaceDef{
	{"reference_lists", referenceListsSurface},
	{"data_tables", dataTablesSurface},
	{"parsers", parsersSurface},
	{"feeds", feedsSurface},
	{"forwarders", forwardersSurface},
	{"dashboards", dashboardsSurface},
	{"rule_exclusions", ruleExclusionsSurface},
	{"metric_definitions", metricDefinitionsSurface},
	{"scheduled_reports", scheduledReportsSurface},
	{"datataps", dataTapsSurface},
	{"error_notifications", errorNotificationsSurface},
	{"federation_groups", federationGroupsSurface},
}

// SIEMSurfaceNames returns the engine-backed SIEM surface names, sorted.
func SIEMSurfaceNames() []string {
	names := make([]string, 0, len(siemSurfaceDefs))
	for _, d := range siemSurfaceDefs {
		names = append(names, d.name)
	}
	return names
}

// BuildSIEMSurface constructs the named engine surface bound to c, reporting
// whether the name is a known engine surface.
func BuildSIEMSurface(name string, c *chronicle.Client) (reconcile.Surface, bool) {
	for _, d := range siemSurfaceDefs {
		if d.name == name {
			return d.build(c), true
		}
	}
	return reconcile.Surface{}, false
}

// refListSpec is the diff basis for a reference list — the meaningful config,
// stripped of server-managed metadata. It is what Canonicalize compares.
type refListSpec struct {
	DisplayName string   `json:"display_name"`
	Description string   `json:"description,omitempty"`
	SyntaxType  string   `json:"syntax_type,omitempty"`
	Entries     []string `json:"entries"`
}

// referenceListsSurface: reference lists as code. On disk it reuses the existing
// `<slug>.txt` (entries, one per line) + `<slug>.yaml` (typed metadata) layout
// that `pull reference_lists` already writes, so a pulled snapshot pushes back
// without conversion. The API has no delete, so deletions are reported as drift
// (NoDelete); UpdateReferenceList replaces entries wholesale.
func referenceListsSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "reference_lists",
		Dir:     DirRefLists,
		Product: reconcile.ProductSIEM,
		Caps:    reconcile.Capabilities{NoDelete: true},

		List: func(ctx context.Context) (reconcile.ListResult, error) {
			lists, err := c.ListReferenceLists(ctx)
			if err != nil {
				return reconcile.ListResult{}, err
			}
			objs := make([]reconcile.Object, 0, len(lists))
			for _, rl := range lists {
				o, berr := reflistObject(rl)
				if berr != nil {
					return reconcile.ListResult{}, berr
				}
				objs = append(objs, o)
			}
			return reconcile.ListResult{Objects: objs}, nil
		},

		LoadDir: loadReferenceLists,
		Write:   writeReferenceList,

		Create: func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
			spec, err := decodeRefListSpec(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			rl, err := c.CreateReferenceList(ctx, spec.DisplayName, spec.Description, nonNil(spec.Entries), spec.SyntaxType)
			if err != nil {
				return reconcile.Object{}, err
			}
			return reflistObject(*rl)
		},

		Update: func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
			spec, err := decodeRefListSpec(local.Canonical)
			if err != nil {
				return reconcile.Object{}, err
			}
			rl, err := c.UpdateReferenceList(ctx, live.ServerID, spec.Description, nonNil(spec.Entries))
			if err != nil {
				return reconcile.Object{}, err
			}
			return reflistObject(*rl)
		},
	}
}

// reflistObject builds the engine Object (canonical diff basis + identity) for a
// live reference list.
func reflistObject(rl chronicle.ReferenceList) (reconcile.Object, error) {
	display := rl.DisplayName
	if display == "" {
		display = lastSegment(rl.Name)
	}
	values := make([]string, len(rl.Entries))
	for i, e := range rl.Entries {
		values[i] = e.Value
	}
	canon, err := canonicalRefList(refListSpec{
		DisplayName: display,
		Description: rl.Description,
		SyntaxType:  rl.SyntaxType,
		Entries:     values,
	})
	if err != nil {
		return reconcile.Object{}, err
	}
	return reconcile.Object{Slug: Slugify(display), ServerID: rl.Name, Canonical: canon}, nil
}

// loadReferenceLists reads every `<slug>.yaml` + sibling `<slug>.txt` in dir.
func loadReferenceLists(dir string) ([]reconcile.Object, error) {
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
		stem := strings.TrimSuffix(e.Name(), ".yaml")
		var meta refListMeta
		if rerr := readYAMLFile(filepath.Join(dir, e.Name()), &meta); rerr != nil {
			return nil, rerr
		}
		values, rerr := readEntryLines(filepath.Join(dir, stem+".txt"))
		if rerr != nil {
			return nil, rerr
		}
		canon, cerr := canonicalRefList(refListSpec{
			DisplayName: meta.DisplayName,
			Description: meta.Description,
			SyntaxType:  meta.SyntaxType,
			Entries:     values,
		})
		if cerr != nil {
			return nil, cerr
		}
		objs = append(objs, reconcile.Object{Slug: stem, ServerID: meta.Name, Canonical: canon})
	}
	return objs, nil
}

// writeReferenceList renders one object back to the `<slug>.txt` + `<slug>.yaml`
// layout (the same shape `pull reference_lists` writes). Server-only metadata
// (scope_info, revision time) is dropped — a re-pull restores it.
func writeReferenceList(dir string, o reconcile.Object) error {
	spec, err := decodeRefListSpec(o.Canonical)
	if err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	body := strings.Join(spec.Entries, "\n")
	if len(spec.Entries) > 0 {
		body += "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, o.Slug+".txt"), []byte(body), 0o644); err != nil {
		return err
	}
	return writeYAML(filepath.Join(dir, o.Slug+".yaml"), refListMeta{
		DisplayName: spec.DisplayName,
		Name:        o.ServerID,
		Description: spec.Description,
		SyntaxType:  spec.SyntaxType,
		EntryCount:  len(spec.Entries),
	})
}

// readEntryLines reads a `<slug>.txt` into entry values (one per line). A missing
// file means no entries.
func readEntryLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	s := strings.TrimSuffix(string(data), "\n")
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
}

func canonicalRefList(spec refListSpec) ([]byte, error) {
	// Normalize entries to a non-nil slice so an EMPTY reference list canonicalizes
	// identically on both sides: the live side builds entries with make([]string, 0)
	// (JSON []), while the on-disk side reads an empty .txt as nil (JSON null).
	// Canonicalize re-marshals as-is, so [] vs null would otherwise phantom-report
	// an empty list as drifted (~1) immediately after a clean pull.
	spec.Entries = nonNil(spec.Entries)
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(raw)
}

func decodeRefListSpec(canonical []byte) (refListSpec, error) {
	var spec refListSpec
	err := json.Unmarshal(canonical, &spec)
	return spec, err
}

// nonNil returns a non-nil slice so UpdateReferenceList/CreateReferenceList treat
// "no entries" as an explicit empty set rather than "leave unchanged".
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
