package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func init() {
	rootCmd.AddCommand(newMitreCmd())
}

// newMitreCmd reports MITRE ATT&CK coverage across BOTH custom (`rules`) and
// Google-managed (`curated`) detections — which techniques are covered and by how
// many rules. It is top-level because coverage spans both sources (pick with
// --type). Custom-rule technique/tactic come from the YARA-L meta block
// (metadata.mitre_*); curated rules carry typed tactics/techniques. Read-only.
func newMitreCmd() *cobra.Command {
	var (
		ruleType          string
		enabled, alerting bool
		format, out       string
	)
	cmd := &cobra.Command{
		Use:   "mitre",
		Short: "Read-only: MITRE ATT&CK coverage (techniques × rule count) across custom + curated rules",
		Long: "Aggregate the MITRE ATT&CK techniques your rules detect into a coverage view —\n" +
			"per technique: how many rules cover it, the tactics involved, and the rule ids.\n" +
			"Custom-rule MITRE comes from the YARA-L meta block (mitre_tactic / mitre_technique);\n" +
			"curated rules carry typed tactics/techniques. Rules with no MITRE meta are reported\n" +
			"under UNMAPPED so coverage gaps are visible. --enabled / --alerting filter custom\n" +
			"rules by deployment state (curated rules are managed at the set level and always\n" +
			"included when selected).",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateOutputFormat(format); err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			agg := newMitreAgg()
			wantCustom := ruleType == "custom" || ruleType == "all"
			wantCurated := ruleType == "curated" || ruleType == "all"

			if wantCustom {
				rules, err := c.ListRules(baseContext())
				if err != nil {
					return err
				}
				// When filtering, treat the deployment as authoritative for
				// enabled/alerting (the rule view may not carry it) — same as
				// `rules health`.
				var depByID map[string]*chronicle.RuleDeployment
				if enabled || alerting {
					deps, err := c.ListRuleDeployments(baseContext())
					if err != nil {
						return err
					}
					depByID = make(map[string]*chronicle.RuleDeployment, len(deps))
					for i := range deps {
						depByID[deps[i].RuleID()] = &deps[i]
					}
				}
				for i := range rules {
					r := &rules[i]
					en, al := r.LiveModeEnabled, r.AlertingEnabled
					if d := depByID[r.RuleID()]; d != nil {
						en, al = en || d.Enabled, al || d.Alerting
					}
					if enabled && !en {
						continue
					}
					if alerting && !al {
						continue
					}
					agg.add(r.RuleID(), r.MitreTactics(), namesToRefs(r.MitreTechniques()))
				}
			}
			if wantCurated {
				crs, err := c.ListCuratedRules(baseContext())
				if err != nil {
					return err
				}
				for i := range crs {
					cr := &crs[i]
					agg.add(cr.ID, refNames(cr.Tactics), cr.Techniques)
				}
			}

			w, closeFn, err := openOut(out)
			if err != nil {
				return err
			}
			defer func() { _ = closeFn() }()
			return agg.render(w, format)
		},
	}
	f := cmd.Flags()
	f.StringVar(&ruleType, "type", "all", "which rules to include: custom | curated | all")
	f.BoolVar(&enabled, "enabled", false, "custom rules: only those with the deployment enabled")
	f.BoolVar(&alerting, "alerting", false, "custom rules: only those with alerting on")
	f.StringVar(&format, "format", "", "output: table | json | csv (default table, or json under --json)")
	f.StringVar(&out, "out", "", "write to a file instead of stdout")
	return markJSON(cmd)
}

// validateOutputFormat guards the shared table|json|csv choice (mitre + health).
func validateOutputFormat(format string) error {
	switch format {
	case "", "table", "json", "csv":
		return nil
	default:
		return fmt.Errorf("--format must be table, json, or csv (got %q)", format)
	}
}

// mitreCell accumulates the rules covering one technique.
type mitreCell struct {
	Technique     string          // technique id/token (the grouping key, upper-cased)
	TechniqueName string          // best display name seen
	tactics       map[string]bool // tactic ids/names involved
	ruleIDs       map[string]bool // contributing rule ids (custom ru_ / curated ur_)
}

type mitreAgg struct {
	cells    map[string]*mitreCell
	allRules map[string]bool // every rule counted (for the total)
	custom   int
	curated  int
}

const mitreUnmapped = "UNMAPPED"

func newMitreAgg() *mitreAgg {
	return &mitreAgg{cells: map[string]*mitreCell{}, allRules: map[string]bool{}}
}

// add folds one rule (its tactics + techniques) into the aggregate. A rule with
// no techniques lands in the UNMAPPED cell so gaps stay visible.
func (a *mitreAgg) add(ruleID string, tactics []string, techniques []chronicle.MitreRef) {
	if ruleID == "" {
		return
	}
	if strings.HasPrefix(ruleID, "ur_") {
		a.curated++
	} else {
		a.custom++
	}
	a.allRules[ruleID] = true

	if len(techniques) == 0 {
		a.cellFor(mitreUnmapped, "").addRule(ruleID, tactics)
		return
	}
	for _, t := range techniques {
		key := strings.ToUpper(strings.TrimSpace(orFirst(t.ID, t.DisplayName)))
		if key == "" {
			continue
		}
		a.cellFor(key, t.DisplayName).addRule(ruleID, tactics)
	}
}

func (a *mitreAgg) cellFor(key, name string) *mitreCell {
	cell := a.cells[key]
	if cell == nil {
		cell = &mitreCell{Technique: key, tactics: map[string]bool{}, ruleIDs: map[string]bool{}}
		a.cells[key] = cell
	}
	if cell.TechniqueName == "" && name != "" && !strings.EqualFold(name, key) {
		cell.TechniqueName = name
	}
	return cell
}

func (cell *mitreCell) addRule(ruleID string, tactics []string) {
	cell.ruleIDs[ruleID] = true
	for _, t := range tactics {
		if t = strings.TrimSpace(t); t != "" {
			cell.tactics[t] = true
		}
	}
}

// mitreRow is the rendered, sorted shape of one technique cell.
type mitreRow struct {
	Technique     string   `json:"technique"`
	TechniqueName string   `json:"technique_name,omitempty"`
	Tactics       []string `json:"tactics"`
	RuleCount     int      `json:"rule_count"`
	RuleIDs       []string `json:"rule_ids"`
}

func (a *mitreAgg) rows() []mitreRow {
	rows := make([]mitreRow, 0, len(a.cells))
	for _, cell := range a.cells {
		rows = append(rows, mitreRow{
			Technique:     cell.Technique,
			TechniqueName: cell.TechniqueName,
			Tactics:       sortedKeys(cell.tactics),
			RuleCount:     len(cell.ruleIDs),
			RuleIDs:       sortedKeys(cell.ruleIDs),
		})
	}
	// Most-covered first; UNMAPPED always last; then by technique id.
	sort.Slice(rows, func(i, j int) bool {
		if (rows[i].Technique == mitreUnmapped) != (rows[j].Technique == mitreUnmapped) {
			return rows[j].Technique == mitreUnmapped
		}
		if rows[i].RuleCount != rows[j].RuleCount {
			return rows[i].RuleCount > rows[j].RuleCount
		}
		return rows[i].Technique < rows[j].Technique
	})
	return rows
}

func (a *mitreAgg) summary(rows []mitreRow) map[string]any {
	techniques, tactics := 0, map[string]bool{}
	unmapped := 0
	for _, r := range rows {
		if r.Technique == mitreUnmapped {
			unmapped = r.RuleCount
			continue
		}
		techniques++
		for _, t := range r.Tactics {
			tactics[t] = true
		}
	}
	return map[string]any{
		"rules_total":        len(a.allRules),
		"custom":             a.custom,
		"curated":            a.curated,
		"techniques_covered": techniques,
		"tactics_covered":    len(tactics),
		"rules_unmapped":     unmapped,
	}
}

func (a *mitreAgg) render(w io.Writer, format string) error {
	rows := a.rows()
	if format == "" {
		format = "table"
		if jsonOut {
			format = "json"
		}
	}
	switch format {
	case "json":
		return writeIndentedValue(w, map[string]any{"summary": a.summary(rows), "techniques": rows})
	case "csv":
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"technique", "technique_name", "tactics", "rule_count", "rule_ids"})
		for _, r := range rows {
			_ = cw.Write([]string{
				r.Technique, r.TechniqueName, strings.Join(r.Tactics, ";"),
				fmt.Sprintf("%d", r.RuleCount), strings.Join(r.RuleIDs, ";"),
			})
		}
		cw.Flush()
		return cw.Error()
	default: // table
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "TECHNIQUE\tNAME\tTACTICS\tRULES")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", r.Technique, orDash(r.TechniqueName),
				orDash(strings.Join(r.Tactics, ", ")), r.RuleCount)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		s := a.summary(rows)
		fmt.Fprintf(w, "\n%d technique(s) covered by %d rule(s) (%d custom, %d curated); %d rule(s) unmapped.\n",
			s["techniques_covered"], s["rules_total"], s["custom"], s["curated"], s["rules_unmapped"])
		return nil
	}
}

// --- small shared helpers ---------------------------------------------------

// namesToRefs lifts bare technique tokens (custom-rule meta) into MitreRefs.
func namesToRefs(names []string) []chronicle.MitreRef {
	refs := make([]chronicle.MitreRef, 0, len(names))
	for _, n := range names {
		refs = append(refs, chronicle.MitreRef{ID: n})
	}
	return refs
}

// refNames flattens curated MitreRefs to their display strings (id preferred).
func refNames(refs []chronicle.MitreRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if v := orFirst(r.ID, r.DisplayName); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// openOut returns a writer for --out (caller closes) or stdout.
func openOut(path string) (io.Writer, func() error, error) {
	if path == "" {
		return os.Stdout, func() error { return nil }, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open --out %q: %w", path, err)
	}
	return f, f.Close, nil
}

// writeIndentedValue marshals v as indented JSON to w (the --out / file analogue
// of emitJSON, which is stdout-only).
func writeIndentedValue(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
