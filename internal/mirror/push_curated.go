package mirror

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"danny.vn/secops/chronicle"
)

// PushCuratedDeployments reconciles curated/deployments.yaml to live curated
// rule-set deployment state. Curated content is Google-managed, so the file
// never creates or deletes rule sets; it only sets enabled/alerting on existing
// (category, rule set, precision) deployments through the batch update API.
func PushCuratedDeployments(ctx context.Context, c *chronicle.Client, curatedDir string, dryRun, assumeYes bool, w io.Writer) (int, error) {
	path := filepath.Join(curatedDir, "deployments.yaml")
	var state curatedState
	if err := readYAMLFile(path, &state); err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("push curated: %s not found; run `secopsctl pull curated` first", path)
		}
		return 0, err
	}

	live, err := c.ListCuratedRuleSetDeployments(ctx)
	if err != nil {
		return 0, err
	}
	cands, err := curatedDeploymentChanges(state, live)
	if err != nil {
		return 0, err
	}
	if len(cands) == 0 {
		fmt.Fprintln(w, "Nothing to push -- curated deployments match live.")
		return 0, nil
	}

	liveBanner(w, fmt.Sprintf("CURATED DEPLOY %d deployment(s) (enabled/alerting)", len(cands)))
	fmt.Fprintf(w, "%-3s %-42s %-8s %-17s %-17s\n", "#", "Rule set", "Precision", "live", "desired")
	fmt.Fprintln(w, strings.Repeat("-", 91))
	for i, cd := range cands {
		fmt.Fprintf(w, "%-3d %-42s %-8s %-17s %-17s\n",
			i+1,
			truncate(sortKey(cd.RuleSetDisplay, cd.RuleSetID), 42),
			cd.Precision,
			curatedDeployPair(cd.Have.Enabled, cd.Have.Alerting),
			curatedDeployPair(cd.Want.Enabled, cd.Want.Alerting))
	}
	fmt.Fprintln(w)

	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to apply.")
		return 0, nil
	}
	if !assumeYes {
		fmt.Fprintf(w, "Refusing to deploy %d curated deployment(s) without confirmation (pass --yes). Aborted.\n", len(cands))
		return 0, nil
	}

	changes := make([]chronicle.CuratedDeploymentChange, 0, len(cands))
	for _, cd := range cands {
		changes = append(changes, chronicle.CuratedDeploymentChange{
			CategoryID: cd.CategoryID,
			RuleSetID:  cd.RuleSetID,
			Precision:  cd.Precision,
			Enabled:    cd.Want.Enabled,
			Alerting:   cd.Want.Alerting,
		})
	}
	if _, err := c.BatchUpdateCuratedRuleSetDeployments(ctx, changes); err != nil {
		return 0, err
	}
	if err := writeYAML(path, state); err != nil {
		fmt.Fprintf(w, "  WARN curated deployed live but local deployments.yaml write failed: %v\n", err)
	}
	fmt.Fprintf(w, "Done. %d curated deployment(s) updated.\n", len(cands))
	return len(cands), nil
}

type curatedDeployCandidate struct {
	CategoryID      string
	CategoryDisplay string
	RuleSetID       string
	RuleSetDisplay  string
	Precision       string
	Want            curatedDeployment
	Have            chronicle.CuratedRuleSetDeployment
}

func curatedDeploymentChanges(state curatedState, live []chronicle.CuratedRuleSetDeployment) ([]curatedDeployCandidate, error) {
	liveByKey := make(map[string]chronicle.CuratedRuleSetDeployment, len(live))
	for _, dep := range live {
		cat, set, prec, err := chronicle.ParseCuratedDeploymentName(dep.Name)
		if err != nil {
			continue
		}
		liveByKey[curatedDeployKey(cat, set, prec)] = dep
	}

	var cands []curatedDeployCandidate
	for _, cat := range state.Categories {
		for _, set := range cat.RuleSets {
			precisions := make([]string, 0, len(set.Deployments))
			for prec := range set.Deployments {
				precisions = append(precisions, prec)
			}
			sort.Strings(precisions)
			for _, prec := range precisions {
				want := set.Deployments[prec]
				key := curatedDeployKey(cat.ID, set.ID, prec)
				have, ok := liveByKey[key]
				if !ok {
					return nil, fmt.Errorf("curated deployment %s/%s/%s is not present live; run `secopsctl pull curated` first", cat.ID, set.ID, prec)
				}
				if want.Enabled == have.Enabled && want.Alerting == have.Alerting {
					continue
				}
				cands = append(cands, curatedDeployCandidate{
					CategoryID:      cat.ID,
					CategoryDisplay: cat.DisplayName,
					RuleSetID:       set.ID,
					RuleSetDisplay:  set.DisplayName,
					Precision:       prec,
					Want:            want,
					Have:            have,
				})
			}
		}
	}

	sort.SliceStable(cands, func(i, j int) bool {
		a := sortKey(cands[i].CategoryDisplay, cands[i].CategoryID) + "\x00" +
			sortKey(cands[i].RuleSetDisplay, cands[i].RuleSetID) + "\x00" + cands[i].Precision
		b := sortKey(cands[j].CategoryDisplay, cands[j].CategoryID) + "\x00" +
			sortKey(cands[j].RuleSetDisplay, cands[j].RuleSetID) + "\x00" + cands[j].Precision
		return a < b
	})
	return cands, nil
}

func curatedDeployKey(categoryID, ruleSetID, precision string) string {
	return categoryID + "\x00" + ruleSetID + "\x00" + precision
}

func curatedDeployPair(enabled, alerting bool) string {
	return fmt.Sprintf("en=%v al=%v", enabled, alerting)
}
