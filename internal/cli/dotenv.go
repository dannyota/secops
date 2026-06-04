package cli

import (
	"bufio"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/internal/userdir"
)

// loadDotEnv populates the process environment from optional dotenv files
// WITHOUT overriding variables already set in the real environment. It lets a
// long-lived secret such as the SOAR AppKey ($SECOPS_SOAR_APP_KEY) live in a
// 0600 user-level file (~/.secopsctl/.env, written by `secopsctl config
// set-soar-key`) instead of being exported by hand in every shell.
//
// The mintable OAuth/ADC SIEM token is NOT loaded this way — it is minted
// in-process by the Google auth library (see package auth) and never written to
// disk.
//
// Precedence, highest first: real environment > ./.env > ~/.secopsctl/.env.
func loadDotEnv() {
	merged := map[string]string{}
	// User-level loaded first so a project-local ./.env overrides it.
	for _, path := range []string{filepath.Join(userdir.Dir(), ".env"), ".env"} {
		maps.Copy(merged, parseEnvFile(path))
	}
	for k, v := range merged {
		if _, ok := os.LookupEnv(k); !ok {
			_ = os.Setenv(k, v)
		}
	}
}

// parseEnvFile reads KEY=VALUE lines from a dotenv file. A missing file yields an
// empty map (dotenv files are optional). Blank lines and # comments are skipped,
// an optional leading "export " is stripped, and matching surrounding single or
// double quotes are removed from the value.
func parseEnvFile(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if k != "" {
			out[k] = v
		}
	}
	// Best-effort: dotenv files are optional, so a read error mid-file just
	// yields whatever parsed cleanly before it.
	_ = sc.Err()
	return out
}
