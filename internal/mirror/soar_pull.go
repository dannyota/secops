package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

// On-disk subdirectory names for SOAR snapshots (under <root>/soar).
const (
	DirSOAR              = "soar"
	DirSOARConnectors    = "connectors"
	DirSOARJobs          = "jobs"
	DirSOARGrouping      = "grouping"
	DirSOARCases         = "cases"
	DirSOARPlaybooks     = "playbooks"
	DirSOARConnAllowlist = "connector_allowlist"
	DirSOARWebhooks      = "webhooks"
	DirSOAREnvironments  = "environments"
	DirSOARNetworks      = "networks"
	DirSOARTrackingLists = "tracking_lists"
	DirSOARSocRoles      = "soc_roles"
	DirSOARIdp           = "idp"
	DirSOARVisualFams    = "visual_families"
	DirSOARSla           = "sla_definitions"
	DirSOARCaseStages    = "case_stages"
	DirSOARCaseTags      = "case_tags"
	DirSOARRootCauses    = "close_root_causes"
	DirSOARBlacklists    = "blacklists"
	DirSOARPlaybookCats  = "playbook_categories"
)

// Connectors and jobs config-as-code moved off the modern v1alpha pull+patch onto
// the reliable legacy AppKey reconcile engine (soar_operational_surfaces.go);
// `soar pull/push connectors|jobs` now route through the engine.

// PullSOARGrouping snapshots the alert-grouping rules and module settings into
// <outDir>/rules/<rule>.yaml and <outDir>/settings.yaml. Returns rules written.
func PullSOARGrouping(ctx context.Context, c *soar.Client, outDir string) (int, error) {
	rulesDir, err := EnsureDir(filepath.Join(outDir, "rules"))
	if err != nil {
		return 0, err
	}

	rules, err := c.ListAlertGroupingRules(ctx)
	if err != nil {
		return 0, err
	}
	for _, r := range rules {
		name := r.Name
		if name == "" {
			name = r.ID
		}
		meta := map[string]any{
			"id": r.ID, "name": r.Name, "category": r.Category,
			"grouping_type": r.GroupingType, "entity_type": r.EntityType,
		}
		if err := writeYAML(filepath.Join(rulesDir, Slugify(name)+".yaml"), meta); err != nil {
			return 0, err
		}
	}

	// Module settings (AlertGroupingSettings) as a flat name->value map.
	if props, perr := c.ListModuleSettingProperties(ctx, "AlertGroupingSettings"); perr != nil {
		warnf("module settings AlertGroupingSettings: %v", perr)
	} else {
		settings := map[string]string{}
		for _, p := range props {
			settings[p.Name] = p.Value
		}
		if err := writeYAML(filepath.Join(outDir, "settings.yaml"), settings); err != nil {
			return 0, err
		}
	}

	fmt.Printf("soar-grouping:   wrote %d rule(s) + settings -> %s/\n", len(rules), outDir)
	return len(rules), nil
}

// PullSOARCases snapshots the current OPEN case queue (legacy external API) to
// <outDir>/open.json. Cases are live data, not config — this is a point-in-time
// view. Returns the number of case cards captured.
func PullSOARCases(ctx context.Context, lc *legacy.Client, outDir string) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}
	raw, err := lc.ListCaseCards(ctx, legacy.CaseQueueRequest{
		RequestedPage: 0, PageSize: 100, Statuses: []int{1}, // 1 = OPEN
	})
	if err != nil {
		return 0, err
	}
	if err := writeIndentedJSON(filepath.Join(outDir, "open.json"), raw); err != nil {
		return 0, err
	}

	n := 0
	var probe struct {
		CaseCards []json.RawMessage `json:"caseCards"`
	}
	if json.Unmarshal(raw, &probe) == nil {
		n = len(probe.CaseCards)
	}
	fmt.Printf("soar-cases:      wrote %d open case(s) -> %s/open.json\n", n, outDir)
	return n, nil
}

// PullSOARPlaybooks snapshots every playbook's full definition (via the v1alpha
// bridge) to <outDir>/<playbook>.json. When prune is true, local .json files
// with no live counterpart are removed so the mirror is an exact 1:1
// reflection of the instance. Returns playbooks written.
func PullSOARPlaybooks(ctx context.Context, lc *legacy.Client, outDir string, prune bool) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}
	cards, err := lc.ListPlaybooks(ctx, nil)
	if err != nil {
		return 0, err
	}

	liveFiles := make(map[string]bool)
	written := 0
	for _, card := range cards {
		pb, gerr := lc.GetPlaybook(ctx, card.Identifier)
		if gerr != nil {
			warnf("get playbook %q: %v", card.Name, gerr)
			continue
		}
		name := card.Name
		if name == "" {
			name = card.Identifier
		}
		filename := Slugify(name) + ".json"
		if err := writeIndentedJSON(filepath.Join(outDir, filename), pb); err != nil {
			return written, err
		}
		liveFiles[filename] = true
		written++
	}

	pruned := 0
	if prune {
		pruned, err = pruneStaleFiles(outDir, liveFiles)
		if err != nil {
			warnf("prune: %v", err)
		}
	}

	msg := fmt.Sprintf("soar-playbooks:  wrote %d playbook(s) -> %s/", written, outDir)
	if pruned > 0 {
		msg += fmt.Sprintf(" (pruned %d stale file(s))", pruned)
	}
	fmt.Println(msg)
	return written, nil
}

// pruneStaleFiles removes .json files in dir that are not in liveFiles.
func pruneStaleFiles(dir string, liveFiles map[string]bool) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	pruned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if liveFiles[e.Name()] {
			continue
		}
		if rerr := os.Remove(filepath.Join(dir, e.Name())); rerr != nil {
			warnf("prune %s: %v", e.Name(), rerr)
			continue
		}
		pruned++
	}
	return pruned, nil
}

// writeIndentedJSON writes raw JSON to path, pretty-printed for clean diffs.
func writeIndentedJSON(path string, raw json.RawMessage) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		// Not valid JSON (shouldn't happen): write the raw bytes verbatim.
		return os.WriteFile(path, raw, 0o644)
	}
	buf.WriteByte('\n')
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// warnf prints a non-fatal warning to stderr, matching the chronicle pullers.
func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "  (warn) "+format+"\n", args...)
}
