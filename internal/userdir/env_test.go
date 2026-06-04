package userdir

import (
	"os"
	"strings"
	"testing"
)

func TestEnvFilePath(t *testing.T) {
	t.Setenv(envHome, "/tmp/sc-home")
	if EnvFilePath() != "/tmp/sc-home/.env" {
		t.Errorf("EnvFilePath() = %q", EnvFilePath())
	}
}

func TestSetEnvVar(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envHome, home)

	// Pre-seed with an unrelated line and a stale assignment to be replaced.
	seed := "# keep me\nOTHER=stays\nSECOPS_SOAR_APP_KEY=old\n"
	if err := os.WriteFile(EnvFilePath(), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := SetEnvVar("SECOPS_SOAR_APP_KEY", "new-value")
	if err != nil {
		t.Fatal(err)
	}
	if path != EnvFilePath() {
		t.Errorf("path = %q, want %q", path, EnvFilePath())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "# keep me") || !strings.Contains(got, "OTHER=stays") {
		t.Errorf("unrelated lines not preserved:\n%s", got)
	}
	if strings.Contains(got, "SECOPS_SOAR_APP_KEY=old") {
		t.Errorf("stale key not replaced:\n%s", got)
	}
	if !strings.Contains(got, "SECOPS_SOAR_APP_KEY=new-value") {
		t.Errorf("new key not written:\n%s", got)
	}
	if strings.Count(got, "SECOPS_SOAR_APP_KEY=") != 1 {
		t.Errorf("want exactly one assignment of the key:\n%s", got)
	}

	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
}

func TestSetEnvVarCreatesFile(t *testing.T) {
	t.Setenv(envHome, t.TempDir())
	if _, err := SetEnvVar("SECOPS_SOAR_APP_KEY", "v"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(EnvFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "SECOPS_SOAR_APP_KEY=v\n" {
		t.Errorf("unexpected file content: %q", string(data))
	}
}
