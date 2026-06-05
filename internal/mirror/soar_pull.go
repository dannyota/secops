package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
	DirSOARWebhooks      = "webhooks"
	DirSOAREnvironments  = "environments"
	DirSOARNetworks      = "networks"
	DirSOARTrackingLists = "tracking_lists"
	DirSOARSocRoles      = "soc_roles"
	DirSOARIdp           = "idp"
	DirSOARVisualFams    = "visual_families"
)

// connectorSnapshot is the diff-friendly YAML written per connector instance.
// Secret parameters are already masked ("***…") by the server, so the snapshot
// is safe to commit as-is.
type connectorSnapshot struct {
	Name             string            `yaml:"name"`
	DisplayName      string            `yaml:"display_name"`
	Integration      string            `yaml:"integration"`
	Connector        string            `yaml:"connector"`
	Enabled          bool              `yaml:"enabled"`
	IntervalSeconds  int               `yaml:"interval_seconds"`
	ProductFieldName string            `yaml:"product_field_name,omitempty"`
	EventFieldName   string            `yaml:"event_field_name,omitempty"`
	AllowList        []string          `yaml:"allow_list,omitempty"`
	Parameters       map[string]string `yaml:"parameters,omitempty"`
}

// PullSOARConnectors snapshots every connector instance across every integration
// into <outDir>/<integration>/<connector>/<instance>.yaml. Per-integration and
// per-connector errors are warned and skipped so one bad pack doesn't abort the
// whole pull. Returns the number of instances written.
func PullSOARConnectors(ctx context.Context, c *soar.Client, outDir string) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}
	integrations, err := c.ListIntegrations(ctx)
	if err != nil {
		return 0, err
	}

	written := 0
	for _, integ := range integrations {
		key := integ.Name // addressable key (Name for clones, see the SDK gotcha)
		conns, cerr := c.ListConnectors(ctx, key)
		if cerr != nil {
			warnf("connectors for %s: %v", integ.DisplayName, cerr)
			continue
		}
		for _, conn := range conns {
			instances, ierr := c.ListConnectorInstances(ctx, key, conn.PathID())
			if ierr != nil {
				warnf("connector instances for %s/%s: %v", integ.DisplayName, conn.DisplayName, ierr)
				continue
			}
			for _, inst := range instances {
				dir, derr := EnsureDir(filepath.Join(outDir, Slugify(integ.DisplayName), Slugify(conn.DisplayName)))
				if derr != nil {
					return written, derr
				}
				snap := connectorSnapshot{
					Name: inst.Name, DisplayName: inst.DisplayName,
					Integration: integ.DisplayName, Connector: conn.DisplayName,
					Enabled: inst.Enabled, IntervalSeconds: inst.IntervalSeconds,
					ProductFieldName: inst.ProductFieldName, EventFieldName: inst.EventFieldName,
					AllowList: inst.AllowList, Parameters: inst.Parameters,
				}
				name := inst.DisplayName
				if name == "" {
					name = lastSegment(inst.Name)
				}
				if err := writeYAML(filepath.Join(dir, Slugify(name)+".yaml"), snap); err != nil {
					return written, err
				}
				written++
			}
		}
	}
	fmt.Printf("soar-connectors: wrote %d instance(s) -> %s/\n", written, outDir)
	return written, nil
}

// jobSnapshot is the diff-friendly YAML written per job instance.
type jobSnapshot struct {
	Name            string            `yaml:"name"`
	DisplayName     string            `yaml:"display_name"`
	Integration     string            `yaml:"integration"`
	Job             string            `yaml:"job"`
	Enabled         bool              `yaml:"enabled"`
	CronSchedule    string            `yaml:"cron_schedule,omitempty"`
	IntervalSeconds int               `yaml:"interval_seconds"`
	Parameters      map[string]string `yaml:"parameters,omitempty"`
}

// PullSOARJobs snapshots every job instance across every integration into
// <outDir>/<integration>/<job>/<instance>.yaml. Returns instances written.
func PullSOARJobs(ctx context.Context, c *soar.Client, outDir string) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}
	integrations, err := c.ListIntegrations(ctx)
	if err != nil {
		return 0, err
	}

	written := 0
	for _, integ := range integrations {
		key := integ.Name
		jobs, jerr := c.ListJobs(ctx, key)
		if jerr != nil {
			warnf("jobs for %s: %v", integ.DisplayName, jerr)
			continue
		}
		for _, job := range jobs {
			instances, ierr := c.ListJobInstances(ctx, key, job.PathID())
			if ierr != nil {
				warnf("job instances for %s/%s: %v", integ.DisplayName, job.DisplayName, ierr)
				continue
			}
			for _, inst := range instances {
				dir, derr := EnsureDir(filepath.Join(outDir, Slugify(integ.DisplayName), Slugify(job.DisplayName)))
				if derr != nil {
					return written, derr
				}
				snap := jobSnapshot{
					Name: inst.Name, DisplayName: inst.DisplayName,
					Integration: integ.DisplayName, Job: job.DisplayName,
					Enabled: inst.Enabled, CronSchedule: inst.CronSchedule,
					IntervalSeconds: inst.IntervalSeconds, Parameters: inst.Parameters,
				}
				name := inst.DisplayName
				if name == "" {
					name = lastSegment(inst.Name)
				}
				if err := writeYAML(filepath.Join(dir, Slugify(name)+".yaml"), snap); err != nil {
					return written, err
				}
				written++
			}
		}
	}
	fmt.Printf("soar-jobs:       wrote %d instance(s) -> %s/\n", written, outDir)
	return written, nil
}

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
// bridge) to <outDir>/<playbook>.json. Returns playbooks written.
func PullSOARPlaybooks(ctx context.Context, lc *legacy.Client, outDir string) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}
	cards, err := lc.ListPlaybooks(ctx, nil)
	if err != nil {
		return 0, err
	}

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
		if err := writeIndentedJSON(filepath.Join(outDir, Slugify(name)+".json"), pb); err != nil {
			return written, err
		}
		written++
	}
	fmt.Printf("soar-playbooks:  wrote %d playbook(s) -> %s/\n", written, outDir)
	return written, nil
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
