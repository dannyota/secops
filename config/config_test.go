package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const exampleConfig = "instance.example.yaml"

func TestLoadExample(t *testing.T) {
	if _, err := os.Stat(exampleConfig); err != nil {
		t.Fatalf("example config missing: %v", err)
	}

	inst, err := Load(exampleConfig)
	if err != nil {
		t.Fatalf("Load(%q) error: %v", exampleConfig, err)
	}

	if inst.ProjectID == "" || inst.Region == "" || inst.CustomerID == "" {
		t.Errorf("required fields not populated: %+v", inst)
	}
	// project_number is written quoted with leading zeros; it must round-trip.
	if string(inst.ProjectNumber) != "000000000000" {
		t.Errorf("ProjectNumber = %q, want %q", inst.ProjectNumber, "000000000000")
	}
}

func TestBaseURLDerivedFromRegion(t *testing.T) {
	inst, err := Load(exampleConfig)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	// base_url is commented out in the example, so it must be derived.
	if !strings.HasPrefix(inst.BaseURL, "https://") {
		t.Errorf("BaseURL = %q, want https:// prefix", inst.BaseURL)
	}
	if !strings.Contains(inst.BaseURL, inst.Region) {
		t.Errorf("BaseURL %q does not contain region %q", inst.BaseURL, inst.Region)
	}
	if !strings.Contains(inst.BaseURL, "chronicle.googleapis.com") {
		t.Errorf("BaseURL %q missing chronicle host", inst.BaseURL)
	}
}

func TestMissingRequiredKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instance.yaml")
	// Missing project_id.
	body := "project_number: \"1\"\nregion: us\ncustomer_id: x\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing project_id, got nil")
	}
	if !strings.Contains(err.Error(), "project_id") {
		t.Errorf("error %q should name the missing key project_id", err)
	}
}

func TestSettingsMapping(t *testing.T) {
	inst, err := Load(exampleConfig)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	s := inst.Settings()
	if s.ProjectID != inst.ProjectID || s.Region != inst.Region ||
		s.CustomerID != inst.CustomerID || s.ProjectNumber != string(inst.ProjectNumber) {
		t.Errorf("Settings() mismatch: %+v vs %+v", s, inst)
	}
}

func TestSearchPathsIncludeUserDir(t *testing.T) {
	t.Setenv("SECOPSCTL_HOME", "/tmp/sc-cfg")
	paths := SearchPaths("")
	want := filepath.Join("/tmp/sc-cfg", "instance.yaml")
	found := false
	for _, p := range paths {
		if p == want {
			found = true
		}
	}
	if !found {
		t.Errorf("~/.secopsctl config path %q not in search paths: %v", want, paths)
	}
}
