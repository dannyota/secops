package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "" +
		"# a comment\n" +
		"\n" +
		"export EXPORTED=one\n" +
		"QUOTED=\"two words\"\n" +
		"SINGLE='three'\n" +
		"PLAIN=four\n" +
		"  SPACED  =  five  \n" +
		"NOEQUALS\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got := parseEnvFile(path)
	want := map[string]string{
		"EXPORTED": "one",
		"QUOTED":   "two words",
		"SINGLE":   "three",
		"PLAIN":    "four",
		"SPACED":   "five",
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseEnvFileMissing(t *testing.T) {
	if got := parseEnvFile(filepath.Join(t.TempDir(), "nope.env")); len(got) != 0 {
		t.Errorf("missing file should yield empty map, got %v", got)
	}
}

func TestLoadDotEnvPrecedence(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()

	// User-level file: defines USER_ONLY and OVERRIDE.
	if err := os.WriteFile(filepath.Join(home, ".env"),
		[]byte("USER_ONLY=u\nOVERRIDE=from_user\nREAL_WINS=from_user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Project-level ./.env overrides the user file for OVERRIDE.
	if err := os.WriteFile(filepath.Join(proj, ".env"),
		[]byte("OVERRIDE=from_project\nREAL_WINS=from_project\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SECOPSCTL_HOME", home)
	t.Chdir(proj)
	// A real environment variable must beat both files.
	t.Setenv("REAL_WINS", "from_real_env")

	loadDotEnv()

	cases := map[string]string{
		"USER_ONLY": "u",             // only the user file has it
		"OVERRIDE":  "from_project",  // project beats user
		"REAL_WINS": "from_real_env", // real env beats every file
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}
