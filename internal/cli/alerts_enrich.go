package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Alert enrichment. `enrich` fetches the full per-alert detection collection the
// console renders (rule + UDM events + entities + triage) via
// legacy:legacyBatchGetCollections — the surface the web UI actually uses. The
// AI agent's investigation is `alerts investigate <id> --latest`.
//
// The pre-case "run an integration action against an alert's entities" verbs are
// intentionally absent: the only known endpoint for them (enrichmentAgent:*)
// returns a server-side 500 for every variant and is not used by the console, so
// shipping it would surface a command that always fails. The in-case equivalent —
// `soar case run-action` — works today.

func newAlertsEnrichCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enrich <alert-id>",
		Short: "Read-only: a SIEM alert's full context (rule detection, UDM events, entities, triage)",
		Long: "Fetch the rich per-alert view the console renders when an analyst opens an\n" +
			"alert — the rule detection(s), every mapped UDM event, the involved entities\n" +
			"and indicators (hosts, users, process hashes, domains), the alert's MITRE\n" +
			"tags, its SOAR case linkage, and the AI triage verdict when an agent has run.\n" +
			"--json prints the complete collection. The AI agent's investigation detail is\n" +
			"`alerts investigate <id> --latest`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			resp, err := c.BatchGetCollections(baseContext(), []string{args[0]})
			if err != nil {
				return err
			}
			if len(resp.Collections) == 0 {
				return fmt.Errorf("no detection-alert collection for id %q", args[0])
			}
			col := resp.Collections[0]
			if jsonOut {
				return writeRawJSON(os.Stdout, col.Raw)
			}
			return emitAlertEnrichData(c, &col)
		},
	}
	return markJSON(cmd)
}

// emitAlertEnrichData renders a detection-alert collection compactly: the rule
// detection, the entities/indicators pulled from its UDM events, MITRE tags, the
// SOAR case bridge, and the AI triage verdict (with a pivot to the full
// investigation).
func emitAlertEnrichData(c *chronicle.Client, col *chronicle.LegacyCollection) error {
	fmt.Fprintf(os.Stdout, "Alert %s\n", col.ID)
	if len(col.Detection) > 0 {
		d := col.Detection[0]
		fmt.Fprintf(os.Stdout, "Rule:     %s  (%s)\n", orDash(d.RuleName), orDash(d.Severity))
		if d.RuleSetDisplayName != "" {
			fmt.Fprintf(os.Stdout, "Ruleset:  %s · %s\n", orDash(d.RulesetCategoryName), d.RuleSetDisplayName)
		}
	}
	if len(col.Tags) > 0 {
		fmt.Fprintf(os.Stdout, "Tags:     %s\n", strings.Join(col.Tags, ", "))
	}

	ents := collectAlertEntities(col.CollectionElements)
	fmt.Fprintf(os.Stdout, "\nEntities & indicators (%d) from %d event(s):\n", len(ents), len(col.CollectionElements))
	for _, e := range ents {
		fmt.Fprintf(os.Stdout, "  %-9s %s\n", e.kind, e.value)
	}

	if fs := col.FeedbackSummary; fs != nil {
		fmt.Fprintf(os.Stdout, "\nTriage:   %s · %s", orDash(fs.Status), orDash(fs.PriorityDisplay))
		if fs.Verdict != "" && fs.Verdict != "VERDICT_UNSPECIFIED" {
			fmt.Fprintf(os.Stdout, " · verdict %s", fs.Verdict)
		}
		fmt.Fprintln(os.Stdout)
		if fs.TriageAgentInvestigationID != "" {
			fmt.Fprintf(os.Stdout, "AI agent: investigation ran — `alerts investigate %s --latest` for its verdict + steps.\n", col.ID)
		}
	}
	printAlertCaseBridge(c, col.CaseName)
	return nil
}

// alertEntity is one deduped entity/indicator surfaced from an alert's events.
type alertEntity struct{ kind, value string }

// collectAlertEntities walks a collection's mapped UDM events and pulls the
// salient entities/indicators — hostnames, users, process files (path + sha256),
// and the about[] urls — deduped, first-seen order preserved.
func collectAlertEntities(elements []json.RawMessage) []alertEntity {
	seen := map[string]bool{}
	var out []alertEntity
	add := func(kind, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := kind + "\x00" + value
		if !seen[key] {
			seen[key] = true
			out = append(out, alertEntity{kind, value})
		}
	}
	addNoun := func(n udmNoun) {
		add("host", n.Hostname)
		add("user", n.User.UserID)
		if p := n.Process.File; p.FullPath != "" || p.SHA256 != "" {
			add("process", strings.TrimSpace(p.FullPath+"  "+p.SHA256))
		}
		add("url", n.URL)
	}
	var node struct {
		References []struct {
			Event struct {
				Principal udmNoun   `json:"principal"`
				Target    udmNoun   `json:"target"`
				About     []udmNoun `json:"about"`
			} `json:"event"`
		} `json:"references"`
	}
	for _, el := range elements {
		if json.Unmarshal(el, &node) != nil {
			continue
		}
		for _, r := range node.References {
			addNoun(r.Event.Principal)
			addNoun(r.Event.Target)
			for _, a := range r.Event.About {
				addNoun(a)
			}
		}
	}
	return out
}

// udmNoun is the minimal slice of a UDM noun (principal/target/about) the alert
// enrichment view reads.
type udmNoun struct {
	Hostname string `json:"hostname"`
	URL      string `json:"url"`
	User     struct {
		UserID string `json:"userid"`
	} `json:"user"`
	Process struct {
		File struct {
			FullPath string `json:"fullPath"`
			SHA256   string `json:"sha256"`
		} `json:"file"`
	} `json:"process"`
}
