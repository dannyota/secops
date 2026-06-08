package mirror

import (
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

func TestCuratedDeploymentChanges(t *testing.T) {
	state := curatedState{Categories: []curatedCategory{{
		DisplayName: "Cloud Threats",
		ID:          "cat_cloud",
		RuleSets: []curatedRuleSet{{
			DisplayName: "Suspicious Activity",
			ID:          "rs_suspicious",
			Deployments: map[string]curatedDeployment{
				"broad":   {Enabled: false, Alerting: false},
				"precise": {Enabled: true, Alerting: false},
			},
		}},
	}}}
	live := []chronicle.CuratedRuleSetDeployment{
		{Name: curatedLiveName("cat_cloud", "rs_suspicious", "precise"), Enabled: false, Alerting: false},
		{Name: curatedLiveName("cat_cloud", "rs_suspicious", "broad"), Enabled: false, Alerting: false},
	}

	got, err := curatedDeploymentChanges(state, live)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("changes = %d, want 1", len(got))
	}
	if got[0].CategoryID != "cat_cloud" || got[0].RuleSetID != "rs_suspicious" || got[0].Precision != "precise" {
		t.Fatalf("wrong change identity: %+v", got[0])
	}
	if !got[0].Want.Enabled || got[0].Want.Alerting || got[0].Have.Enabled {
		t.Fatalf("wrong change values: %+v", got[0])
	}
}

func TestCuratedDeploymentChangesMissingLive(t *testing.T) {
	state := curatedState{Categories: []curatedCategory{{
		ID: "cat",
		RuleSets: []curatedRuleSet{{
			ID:          "set",
			Deployments: map[string]curatedDeployment{"precise": {Enabled: true}},
		}},
	}}}

	_, err := curatedDeploymentChanges(state, nil)
	if err == nil {
		t.Fatal("missing live deployment accepted")
	}
	if !strings.Contains(err.Error(), "pull curated") {
		t.Fatalf("error = %q, want pull hint", err)
	}
}

func curatedLiveName(categoryID, ruleSetID, precision string) string {
	return "projects/p/locations/r/instances/i/curatedRuleSetCategories/" + categoryID +
		"/curatedRuleSets/" + ruleSetID + "/curatedRuleSetDeployments/" + precision
}
