package mirror

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"danny.vn/secops/chronicle"
)

// PullRules snapshots every custom (user-authored) detection rule into outDir.
//
// For each rule it writes <slug>.yaral (the YARA-L source) and a companion
// <slug>.yaml (metadata + deployment state) using the shared ruleCompanion /
// deploymentMeta structs. Deployments are listed once and indexed by ruleID so
// the per-rule lookup is O(1). It returns the number of rules written.
func PullRules(ctx context.Context, c *chronicle.Client, outDir string) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}

	rules, err := c.ListRules(ctx)
	if err != nil {
		return 0, err
	}

	// Index deployments once, keyed by ruleID.
	deployments := map[string]chronicle.RuleDeployment{}
	deps, err := c.ListRuleDeployments(ctx)
	if err != nil {
		// Deployment state is best-effort metadata; a rule still round-trips
		// without it. Warn and continue rather than aborting the whole pull.
		fmt.Fprintf(os.Stderr, "  (warn) could not list rule deployments: %v\n", err)
	}
	for _, d := range deps {
		if id := d.RuleID(); id != "" {
			deployments[id] = d
		}
	}

	written := 0
	for _, rule := range rules {
		display := rule.DisplayName
		if display == "" {
			display = "unnamed"
		}
		slug := Slugify(display)
		ruleID := rule.RuleID()

		// Rule text should be present from the FULL list view; if not, fall
		// back to a per-rule GetRule to fill it.
		text := rule.Text
		if text == "" && ruleID != "" {
			if full, gerr := c.GetRule(ctx, ruleID); gerr != nil {
				fmt.Fprintf(os.Stderr, "  (warn) get_rule(%s): %v\n", ruleID, gerr)
			} else if full != nil {
				text = full.Text
				// Prefer fuller fields from GetRule where the list omitted them.
				if rule.Etag == "" {
					rule.Etag = full.Etag
				}
				if rule.Severity == nil {
					rule.Severity = full.Severity
				}
				if len(rule.AllowedRunFrequencies) == 0 {
					rule.AllowedRunFrequencies = full.AllowedRunFrequencies
				}
				if rule.TimeWindowDuration == "" {
					rule.TimeWindowDuration = full.TimeWindowDuration
				}
			}
		}

		if err := os.WriteFile(filepath.Join(outDir, slug+".yaral"), []byte(text), 0o644); err != nil {
			return written, err
		}

		severity := ""
		if rule.Severity != nil {
			severity = rule.Severity.DisplayName
		}

		comp := ruleCompanion{
			DisplayName:           display,
			RuleID:                ruleID,
			Name:                  rule.Name,
			Etag:                  rule.Etag,
			Type:                  rule.Type,
			Severity:              severity,
			AllowedRunFrequencies: rule.AllowedRunFrequencies,
			TimeWindowDuration:    rule.TimeWindowDuration,
		}
		if dep, ok := deployments[ruleID]; ok {
			comp.Deployment = deploymentMeta{
				Name:           dep.Name,
				Enabled:        dep.Enabled,
				Alerting:       dep.Alerting,
				Archived:       dep.Archived,
				RunFrequency:   dep.RunFrequency,
				ExecutionState: dep.ExecutionState,
			}
		}

		if err := comp.write(filepath.Join(outDir, slug+".yaml")); err != nil {
			return written, err
		}
		written++
	}

	fmt.Printf("rules:        wrote %d -> %s/\n", written, outDir)
	return written, nil
}
