// Package config locates, loads, and validates a SecOps instance configuration.
//
// A config is a small YAML document describing one Chronicle instance, written
// by `secopsctl config`. Nothing is read at import time; call Load.
//
// Per-value resolution, highest priority first:
//
//  1. real environment variables (SECOPS_*) — never a .env file; secopsctl does
//     not read .env
//  2. the config file at an explicit path (the --config flag / $SECOPSCTL_CONFIG)
//  3. the config file at ~/.secopsctl/instance.yaml (the default), then the
//     legacy ./config/instance.yaml and ~/.config/secopsctl/instance.yaml
//
// A file value is overlaid by the matching environment variable when that
// variable is set, so the environment always wins.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/userdir"
)

// Instance is one Chronicle/SecOps instance configuration. Field order is the
// on-disk YAML order written by Save.
type Instance struct {
	ProjectID     string  `yaml:"project_id"`
	ProjectNumber flexStr `yaml:"project_number"`
	Region        string  `yaml:"region"`
	CustomerID    string  `yaml:"customer_id"`
	SOARURL       string  `yaml:"soar_url,omitempty"`     // SOAR host, e.g. https://<tenant>.siemplify-soar.com
	SOARAppKey    string  `yaml:"soar_app_key,omitempty"` // SOAR AppKey (plaintext, v1); never committed (file is git-ignored)
	BaseURL       string  `yaml:"base_url,omitempty"`     // derived from region when empty
	UIURL         string  `yaml:"ui_url,omitempty"`
	ForceIPv4     bool    `yaml:"force_ipv4,omitempty"` // pin the dialer to IPv4 (corporate-VPN fix; also via SECOPS_FORCE_IPV4)

	source string // file Load read this from ("" = environment only); unexported → never persisted
}

// SourcePath returns the config file this instance was loaded from, or "" when
// the configuration came only from the environment.
func (i *Instance) SourcePath() string { return i.source }

// flexStr is a string that also accepts a YAML scalar written as a bare number,
// so project_number: 000000000000 (unquoted) loads without error and keeps its
// literal text (leading zeros preserved). It is written back double-quoted.
type flexStr string

func (f *flexStr) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("project_number must be a scalar (a number or quoted string), got %s", nodeKindName(value.Kind))
	}
	*f = flexStr(value.Value)
	return nil
}

// nodeKindName names a YAML node kind for error messages.
func nodeKindName(k yaml.Kind) string {
	switch k {
	case yaml.SequenceNode:
		return "a list"
	case yaml.MappingNode:
		return "a map"
	case yaml.AliasNode:
		return "an alias"
	default:
		return "a non-scalar value"
	}
}

func (f flexStr) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(f), Style: yaml.DoubleQuotedStyle}, nil
}

// KnownRegions are the Google SecOps regions secopsctl recognizes. The region
// is the hostname prefix of the Chronicle API (`<region>-chronicle.googleapis.com`)
// and cannot be discovered via the API, so it is validated against this list.
// Extend it when Google adds a region.
var KnownRegions = []string{
	"us",
	"europe",
	"europe-west2",  // London
	"europe-west3",  // Frankfurt
	"europe-west6",  // Zurich
	"europe-west9",  // Paris
	"europe-west12", // Turin
	"asia-southeast1",
	"asia-south1",
	"australia-southeast1",
	"me-central1",
	"me-central2",
	"me-west1",
	"northamerica-northeast2",
}

// IsKnownRegion reports whether region (trimmed) is in KnownRegions.
func IsKnownRegion(region string) bool {
	return slices.Contains(KnownRegions, strings.TrimSpace(region))
}

// SearchPaths returns the ordered candidate config locations (file discovery
// only; environment overrides are applied separately by Load).
func SearchPaths(explicit string) []string {
	var paths []string
	if explicit != "" {
		paths = append(paths, explicit)
	}
	if env := os.Getenv("SECOPSCTL_CONFIG"); env != "" {
		paths = append(paths, env)
	}
	paths = append(paths, userdir.InstanceConfigPath()) // ~/.secopsctl/instance.yaml (default)
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, "config", "instance.yaml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "secopsctl", "instance.yaml"))
	}
	return paths
}

// Find returns the first existing config path, or "".
func Find(explicit string) string {
	for _, p := range SearchPaths(explicit) {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// requireExplicitExists errors when an explicitly requested config path (--config,
// else $SECOPSCTL_CONFIG) is set but does not exist — rather than silently falling
// through to discovery and loading a different (possibly wrong-tenant) config. Only
// the highest-precedence explicit path is enforced; the other is shadowed by it.
func requireExplicitExists(explicit string) error {
	path, src := explicit, "--config"
	if path == "" {
		path, src = os.Getenv("SECOPSCTL_CONFIG"), "$SECOPSCTL_CONFIG"
	}
	if path == "" {
		return nil
	}
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		return fmt.Errorf("config %q (set via %s) not found — fix the path or unset it", path, src)
	}
	return nil
}

// ResolvedSource returns the config file Load would read (after enforcing that an
// explicit path, if given, exists), or "" when the config comes only from the
// environment. Used by `info` / `config --show-path` to confirm the active file.
func ResolvedSource(explicit string) (string, error) {
	if err := requireExplicitExists(explicit); err != nil {
		return "", err
	}
	return Find(explicit), nil
}

// applyEnvOverrides overlays real environment variables onto inst; a set
// variable always wins over the file value. .env files are never consulted.
func applyEnvOverrides(inst *Instance) {
	set := func(dst *string, env string) {
		if v := os.Getenv(env); v != "" {
			*dst = v
		}
	}
	set(&inst.ProjectID, "SECOPS_PROJECT_ID")
	if v := os.Getenv("SECOPS_PROJECT_NUMBER"); v != "" {
		inst.ProjectNumber = flexStr(v)
	}
	set(&inst.Region, "SECOPS_REGION")
	set(&inst.CustomerID, "SECOPS_CUSTOMER_ID")
	set(&inst.SOARURL, "SECOPS_SOAR_URL")
	set(&inst.SOARAppKey, "SECOPS_SOAR_APP_KEY")
	set(&inst.BaseURL, "SECOPS_BASE_URL")
	set(&inst.UIURL, "SECOPS_UI_URL")
	if auth.ForceIPv4Env() {
		inst.ForceIPv4 = true
	}
}

// readFile loads an instance from a YAML file without validation or env overlay.
func readFile(path string) (*Instance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inst Instance
	if err := yaml.Unmarshal(data, &inst); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &inst, nil
}

// Load resolves a config: the discovered file (if any) overlaid with SECOPS_*
// environment variables, then validated and filled with region-derived defaults.
// explicit may be empty to use discovery. A fully env-provided config needs no
// file.
func Load(explicit string) (*Instance, error) {
	// An explicitly named config path must exist — never silently fall through to
	// a different file (a typo'd --config could point pull/push at the wrong tenant).
	if err := requireExplicitExists(explicit); err != nil {
		return nil, err
	}
	var inst Instance
	path := Find(explicit)
	if path != "" {
		fileInst, err := readFile(path)
		if err != nil {
			return nil, err
		}
		inst = *fileInst
	}
	inst.source = path
	applyEnvOverrides(&inst)

	var missing []string
	if inst.ProjectID == "" {
		missing = append(missing, "project_id")
	}
	if inst.ProjectNumber == "" {
		missing = append(missing, "project_number")
	}
	if inst.Region == "" {
		missing = append(missing, "region")
	}
	if inst.CustomerID == "" {
		missing = append(missing, "customer_id")
	}
	if len(missing) > 0 {
		hint := "run `secopsctl config` to set them, or export the matching SECOPS_* variables"
		if path == "" {
			return nil, fmt.Errorf("no secopsctl config found (searched:\n  %s)\nand required key(s) missing: %s\n%s",
				strings.Join(SearchPaths(explicit), "\n  "), strings.Join(missing, ", "), hint)
		}
		return nil, fmt.Errorf("config %s is missing required key(s): %s\n%s",
			path, strings.Join(missing, ", "), hint)
	}

	if inst.BaseURL == "" {
		inst.BaseURL = fmt.Sprintf("https://%s-chronicle.googleapis.com/%s",
			inst.Region, chronicle.DefaultAPIVersion)
	}
	return &inst, nil
}

// DefaultPath is the file `secopsctl config` writes when no --config is given:
// ~/.secopsctl/instance.yaml.
func DefaultPath() string { return userdir.InstanceConfigPath() }

// ReadForEdit reads the config file at path for the interactive `config` command
// to pre-fill prompts, WITHOUT env overlay or validation so it shows what is
// actually persisted. A missing or unparseable file yields an empty Instance.
func ReadForEdit(path string) *Instance {
	if path == "" {
		return &Instance{}
	}
	if inst, err := readFile(path); err == nil {
		return inst
	}
	return &Instance{}
}

// Save writes inst as YAML to path (0600, dir 0700). An empty path writes the
// default ~/.secopsctl/instance.yaml. The file may hold the SOAR AppKey, so it
// is created owner-only; it is git-ignored and never committed. Returns the path.
func Save(inst *Instance, path string) (string, error) {
	if path == "" {
		path = userdir.InstanceConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(inst)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	// WriteFile only applies the mode when CREATING the file; an existing config
	// (possibly 0644 from an older write or hand-edit) keeps its old perms. The
	// file may carry the SOAR AppKey, so tighten it to owner-only either way.
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ProjectNumberString returns the project number as a plain string.
func (i *Instance) ProjectNumberString() string { return string(i.ProjectNumber) }

// SetProjectNumber sets the project number from a string (keeps leading zeros).
func (i *Instance) SetProjectNumber(s string) { i.ProjectNumber = flexStr(s) }

// Settings maps the loaded config to chronicle.Settings.
func (i *Instance) Settings() chronicle.Settings {
	return chronicle.Settings{
		ProjectID:     i.ProjectID,
		ProjectNumber: string(i.ProjectNumber),
		Region:        i.Region,
		CustomerID:    i.CustomerID,
		BaseURL:       i.BaseURL,
		ForceIPv4:     i.ForceIPv4,
	}
}

// AsMap returns the resolved config as a string map for display. The AppKey is
// never included verbatim — only a redacted "(set)" marker.
func (i *Instance) AsMap() map[string]string {
	m := map[string]string{
		"project_id":     i.ProjectID,
		"project_number": string(i.ProjectNumber),
		"region":         i.Region,
		"customer_id":    i.CustomerID,
		"base_url":       i.BaseURL,
	}
	if i.UIURL != "" {
		m["ui_url"] = i.UIURL
	}
	if i.SOARURL != "" {
		m["soar_url"] = i.SOARURL
	}
	if i.SOARAppKey != "" {
		m["soar_app_key"] = "(set)"
	}
	if i.ForceIPv4 {
		m["force_ipv4"] = "true"
	}
	return m
}
