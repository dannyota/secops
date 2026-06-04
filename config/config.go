// Package config locates, loads, and validates a SecOps instance configuration.
//
// A config is a small YAML document describing one Chronicle instance. Nothing
// is read at import time; call Load. Discovery order (first match wins):
//
//  1. explicit path (e.g. the --config flag)
//  2. $SECOPSCTL_CONFIG
//  3. ./config/instance.yaml
//  4. ~/.config/secopsctl/instance.yaml
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/userdir"
)

// Instance is one Chronicle/SecOps instance configuration.
type Instance struct {
	ProjectID     string  `yaml:"project_id"`
	ProjectNumber flexStr `yaml:"project_number"`
	Region        string  `yaml:"region"`
	CustomerID    string  `yaml:"customer_id"`
	BaseURL       string  `yaml:"base_url,omitempty"`
	UIURL         string  `yaml:"ui_url,omitempty"`
	SOARURL       string  `yaml:"soar_url,omitempty"` // SOAR host, e.g. https://<tenant>.siemplify-soar.com
}

// flexStr is a string that also accepts a YAML scalar written as a bare number,
// so project_number: 000000000000 (unquoted) loads without error and keeps its
// literal text (leading zeros preserved).
type flexStr string

func (f *flexStr) UnmarshalYAML(value *yaml.Node) error {
	*f = flexStr(value.Value)
	return nil
}

// SearchPaths returns the ordered candidate config locations.
func SearchPaths(explicit string) []string {
	var paths []string
	if explicit != "" {
		paths = append(paths, explicit)
	}
	if env := os.Getenv("SECOPSCTL_CONFIG"); env != "" {
		paths = append(paths, env)
	}
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, "config", "instance.yaml"))
	}
	paths = append(paths, userdir.InstanceConfigPath()) // ~/.secopsctl/instance.yaml
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

// Load reads, validates, and fills region-derived defaults for an instance
// config. explicit may be empty to use discovery.
func Load(explicit string) (*Instance, error) {
	path := Find(explicit)
	if path == "" {
		return nil, fmt.Errorf("no secopsctl config found; searched:\n  %s",
			strings.Join(SearchPaths(explicit), "\n  "))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inst Instance
	if err := yaml.Unmarshal(data, &inst); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

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
		return nil, fmt.Errorf("config %s is missing required key(s): %s",
			path, strings.Join(missing, ", "))
	}

	if inst.BaseURL == "" {
		inst.BaseURL = fmt.Sprintf("https://%s-chronicle.googleapis.com/%s",
			inst.Region, chronicle.DefaultAPIVersion)
	}
	return &inst, nil
}

// Settings maps the loaded config to chronicle.Settings.
func (i *Instance) Settings() chronicle.Settings {
	return chronicle.Settings{
		ProjectID:     i.ProjectID,
		ProjectNumber: string(i.ProjectNumber),
		Region:        i.Region,
		CustomerID:    i.CustomerID,
		BaseURL:       i.BaseURL,
	}
}

// AsMap returns the resolved config as a string map for display (no secrets).
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
	return m
}
