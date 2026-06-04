package userdir

import "testing"

func TestDirRespectsEnv(t *testing.T) {
	t.Setenv(envHome, "/tmp/sc-home")
	if Dir() != "/tmp/sc-home" {
		t.Errorf("Dir() = %q", Dir())
	}
	if InstanceConfigPath() != "/tmp/sc-home/instance.yaml" {
		t.Errorf("InstanceConfigPath() = %q", InstanceConfigPath())
	}
	if CacheDir() != "/tmp/sc-home/cache" {
		t.Errorf("CacheDir() = %q", CacheDir())
	}
}

func TestDirDefault(t *testing.T) {
	t.Setenv(envHome, "")
	// Default resolves under the user home (or the cwd-relative fallback); either
	// way it must end in ".secopsctl".
	if d := Dir(); d != ".secopsctl" && !endsWith(d, "/.secopsctl") {
		t.Errorf("Dir() = %q, want it to end in .secopsctl", d)
	}
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
