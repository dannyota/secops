package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSOARIntegrationScaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Example")
	res, err := scaffoldSOARIntegration("Example Integration", dir, []string{"Lookup Entity"}, []string{"Nightly Sync"}, false)
	if err != nil {
		t.Fatalf("scaffoldSOARIntegration: %v", err)
	}
	want := []string{
		"manifest.json",
		"Actions/Lookup_Entity.json",
		"Actions/Lookup_Entity.py",
		"Jobs/Nightly_Sync.json",
		"Jobs/Nightly_Sync.py",
	}
	if !slices.Equal(res.Files, want) {
		t.Fatalf("files = %v, want %v", res.Files, want)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest integrationScaffoldManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "Example Integration" || !slices.Contains(manifest.Components, "action:Lookup Entity") {
		t.Fatalf("manifest = %+v", manifest)
	}
	actionPy, err := os.ReadFile(filepath.Join(dir, "Actions", "Lookup_Entity.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(actionPy), "SiemplifyAction") {
		t.Fatalf("action template missing SiemplifyAction: %s", actionPy)
	}

	zipPath := filepath.Join(t.TempDir(), "Example.zip")
	pkg, err := packageSOARIntegrationDir(dir, zipPath, false)
	if err != nil {
		t.Fatalf("package scaffold: %v", err)
	}
	if pkg.Files != len(want) || len(pkg.Warnings) != 0 {
		t.Fatalf("package result = %+v", pkg)
	}
}

func TestSOARIntegrationScaffoldRequiresComponent(t *testing.T) {
	_, err := scaffoldSOARIntegration("Example", filepath.Join(t.TempDir(), "Example"), nil, nil, false)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("err = %v, want component requirement", err)
	}
}

func TestSOARIntegrationScaffoldOverwriteGuard(t *testing.T) {
	dir := t.TempDir()
	if _, err := scaffoldSOARIntegration("Example", dir, []string{"Action"}, nil, false); err != nil {
		t.Fatalf("first scaffold: %v", err)
	}
	if _, err := scaffoldSOARIntegration("Example", dir, []string{"Action"}, nil, false); err == nil {
		t.Fatal("expected overwrite guard")
	}
	if _, err := scaffoldSOARIntegration("Example", dir, []string{"Action"}, nil, true); err != nil {
		t.Fatalf("force scaffold: %v", err)
	}
}

func TestIntegrationFileStem(t *testing.T) {
	for in, want := range map[string]string{
		"Lookup Entity": "Lookup_Entity",
		"  A/B:C  ":     "A_B_C",
		"***":           "integration",
	} {
		if got := integrationFileStem(in); got != want {
			t.Fatalf("integrationFileStem(%q) = %q, want %q", in, got, want)
		}
	}
}
