package mirror

import "os"

// ruleCompanion is the on-disk `<slug>.yaml` that accompanies each `<slug>.yaral`
// rule source. It is the contract shared by PullRules (writer) and the push
// operations (reader/writer): a `.yaral` with NO companion `.yaml` is treated as
// a brand-new rule not yet created in the tenant.
//
// DEVIATION: the legacy Python tool stored the raw API deployment dict here; we
// store a typed, stable subset so the file round-trips deterministically.
type ruleCompanion struct {
	DisplayName           string         `yaml:"display_name"`
	RuleID                string         `yaml:"rule_id"`
	Name                  string         `yaml:"name"`
	Etag                  string         `yaml:"etag,omitempty"`
	Type                  string         `yaml:"type,omitempty"`
	Severity              string         `yaml:"severity,omitempty"`
	AllowedRunFrequencies []string       `yaml:"allowed_run_frequencies,omitempty"`
	TimeWindowDuration    string         `yaml:"time_window_duration,omitempty"`
	Deployment            deploymentMeta `yaml:"deployment"`
}

// deploymentMeta is the deployment state recorded in a rule companion file.
type deploymentMeta struct {
	Name           string `yaml:"name,omitempty"`
	Enabled        bool   `yaml:"enabled"`
	Alerting       bool   `yaml:"alerting"`
	Archived       bool   `yaml:"archived"`
	RunFrequency   string `yaml:"runFrequency,omitempty"`
	ExecutionState string `yaml:"executionState,omitempty"`
}

// readRuleCompanion loads a companion `.yaml` from path.
func readRuleCompanion(path string) (*ruleCompanion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m ruleCompanion
	if err := yamlUnmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// write saves the companion to path as deterministic YAML.
func (m *ruleCompanion) write(path string) error {
	return writeYAML(path, m)
}
