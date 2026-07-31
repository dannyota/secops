package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnvOverridesFile verifies a SECOPS_* env var wins over the file value,
// while unset fields keep the file value.
func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instance.yaml")
	body := "project_id: from-file\nproject_number: \"111111111111\"\nregion: us\ncustomer_id: cust-file\nsoar_app_key: key-file\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SECOPS_PROJECT_ID", "from-env")
	t.Setenv("SECOPS_SOAR_APP_KEY", "key-env")

	inst, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if inst.ProjectID != "from-env" {
		t.Errorf("ProjectID = %q, want env value", inst.ProjectID)
	}
	if inst.SOARAppKey != "key-env" {
		t.Errorf("SOARAppKey = %q, want env value", inst.SOARAppKey)
	}
	if inst.CustomerID != "cust-file" {
		t.Errorf("CustomerID = %q, want file value (no env set)", inst.CustomerID)
	}
	if string(inst.ProjectNumber) != "111111111111" {
		t.Errorf("ProjectNumber = %q, want file value", inst.ProjectNumber)
	}
}

// TestLoadEnvOnlyNoFile verifies a fully env-provided config loads with no file.
func TestLoadEnvOnlyNoFile(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("SECOPSCTL_HOME", empty)
	t.Setenv("HOME", empty)          // isolate ~/.config/secopsctl discovery
	t.Setenv("SECOPSCTL_CONFIG", "") // no explicit file
	t.Chdir(empty)                   // no ./config/instance.yaml here

	t.Setenv("SECOPS_PROJECT_ID", "p")
	t.Setenv("SECOPS_PROJECT_NUMBER", "000000000000")
	t.Setenv("SECOPS_REGION", "eu")
	t.Setenv("SECOPS_CUSTOMER_ID", "c")

	inst, err := Load("")
	if err != nil {
		t.Fatalf("env-only Load should succeed: %v", err)
	}
	if inst.ProjectID != "p" || inst.Region != "eu" || inst.CustomerID != "c" ||
		string(inst.ProjectNumber) != "000000000000" {
		t.Errorf("env-only config not applied: %+v", inst)
	}
}

// TestSaveRoundTrip verifies Save writes a 0600 file that reads back identically,
// preserving leading zeros and the AppKey.
func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.yaml")

	in := &Instance{
		ProjectID:  "proj",
		Region:     "us",
		CustomerID: "00000000-0000-0000-0000-000000000000",
		SOARURL:    "https://example.siemplify-soar.com",
		SOARAppKey: "secret-key",
	}
	in.SetProjectNumber("000000000000")

	if _, err := Save(in, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}

	out, err := readFile(path)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	if out.ProjectID != in.ProjectID || out.Region != in.Region ||
		out.CustomerID != in.CustomerID || out.SOARURL != in.SOARURL ||
		out.SOARAppKey != in.SOARAppKey ||
		string(out.ProjectNumber) != "000000000000" {
		t.Errorf("round-trip mismatch:\n in=%+v\nout=%+v", in, out)
	}
}

// TestForceIPv4EnvOverlay verifies SECOPS_FORCE_IPV4 sets inst.ForceIPv4, so the
// resolved config (and `info`) reflect the dialer behavior the env already forces.
func TestForceIPv4EnvOverlay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instance.yaml")
	body := "project_id: p\nproject_number: \"1\"\nregion: us\ncustomer_id: c\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SECOPS_FORCE_IPV4", "1")

	inst, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !inst.ForceIPv4 {
		t.Error("ForceIPv4 = false, want true from SECOPS_FORCE_IPV4")
	}
}

// TestSaveTightensExistingPerms verifies Save chmods a pre-existing 0644 file down
// to 0600 — os.WriteFile alone leaves an existing file's perms untouched, which
// would leave the AppKey in a group/world-readable file.
func TestSaveTightensExistingPerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instance.yaml")
	if err := os.WriteFile(path, []byte("project_id: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := &Instance{ProjectID: "p", Region: "us", CustomerID: "c", SOARAppKey: "secret"}
	in.SetProjectNumber("1")
	if _, err := Save(in, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("existing file mode = %o, want 600", perm)
	}
}

// TestFlexStrRejectsNonScalar verifies a project_number written as a list/map is a
// loud parse error, not a silent empty value (which would surface as "missing").
func TestFlexStrRejectsNonScalar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instance.yaml")
	body := "project_id: p\nproject_number: [1, 2, 3]\nregion: us\ncustomer_id: c\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load should fail on a non-scalar project_number")
	}
}

func TestReadForEditStrictRejectsMalformedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.yaml")
	if err := os.WriteFile(path, []byte("project_id: [unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadForEditStrict(path); err == nil {
		t.Fatal("ReadForEditStrict accepted malformed YAML")
	}
}

func TestSavePreservesUnknownConfigKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.yaml")
	body := "project_id: old\n" +
		"project_number: \"1\"\n" +
		"region: us\n" +
		"customer_id: customer\n" +
		"domain: example.com\n" +
		"org_id: \"000123\"\n" +
		"future:\n" +
		"  enabled: true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	inst, err := ReadForEditStrict(path)
	if err != nil {
		t.Fatalf("ReadForEditStrict: %v", err)
	}
	inst.ProjectID = "new"
	if _, err := Save(inst, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := ReadForEditStrict(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if out.ProjectID != "new" {
		t.Errorf("ProjectID = %q, want new", out.ProjectID)
	}
	if out.Extra["domain"] != "example.com" {
		t.Errorf("domain = %#v, want example.com", out.Extra["domain"])
	}
	if out.Extra["org_id"] != "000123" {
		t.Errorf("org_id = %#v, want string 000123", out.Extra["org_id"])
	}
	future, ok := out.Extra["future"].(map[string]any)
	if !ok || future["enabled"] != true {
		t.Errorf("future = %#v, want nested enabled=true", out.Extra["future"])
	}
}

func TestSaveRejectsInvalidExtraWithoutPanicking(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
	}{
		{
			name:  "known key collision",
			extra: map[string]any{"project_id": "shadow"},
		},
		{
			name:  "unsupported value",
			extra: map[string]any{"future": func() {}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inst := &Instance{
				ProjectID:  "project",
				Region:     "us",
				CustomerID: "customer",
				Extra:      tc.extra,
			}
			inst.SetProjectNumber("1")
			if _, err := Save(inst, filepath.Join(t.TempDir(), "instance.yaml")); err == nil {
				t.Fatal("Save accepted invalid Extra data")
			}
		})
	}
}

// TestAsMapRedactsAppKey verifies the AppKey value is never returned by AsMap.
func TestAsMapRedactsAppKey(t *testing.T) {
	i := &Instance{ProjectID: "p", Region: "us", CustomerID: "c", SOARAppKey: "super-secret"}
	i.SetProjectNumber("1")
	m := i.AsMap()
	if m["soar_app_key"] != "(set)" {
		t.Errorf("soar_app_key marker = %q, want (set)", m["soar_app_key"])
	}
	for k, v := range m {
		if v == "super-secret" {
			t.Errorf("AsMap leaked the AppKey under %q", k)
		}
	}
}
