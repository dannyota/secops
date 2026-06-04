// Package userdir resolves the secopsctl user directory.
//
// By default this is ~/.secopsctl (override with $SECOPSCTL_HOME). It provides a
// config discovery location, a cache directory for NON-SECRET data, and an
// optional dotenv file for long-lived, user-supplied secrets — additive to
// environment variables and the existing config discovery, never a replacement.
//
// The mintable OAuth access token is NEVER written here: it is minted in-process
// by the Google auth library (see package danny.vn/secops/auth). The dotenv file
// holds only long-lived secrets the user must persist somewhere, such as the
// SOAR AppKey ($SECOPS_SOAR_APP_KEY), which cannot be minted.
//
//	~/.secopsctl/
//	├── instance.yaml   # a config discovery location
//	├── .env            # 0600; long-lived secrets (e.g. the SOAR AppKey)
//	└── cache/          # non-secret cached data (reserved)
package userdir

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const envHome = "SECOPSCTL_HOME"

// Dir returns the secopsctl home directory: $SECOPSCTL_HOME if set, else
// ~/.secopsctl. Falls back to ".secopsctl" (cwd-relative) only if the home
// directory cannot be determined.
func Dir() string {
	if h := strings.TrimSpace(os.Getenv(envHome)); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".secopsctl"
	}
	return filepath.Join(home, ".secopsctl")
}

// InstanceConfigPath is the config location discovered under the home dir.
func InstanceConfigPath() string { return filepath.Join(Dir(), "instance.yaml") }

// CacheDir is the non-secret cache directory (reserved for cached data such as
// log-type catalogs or last-pull state — never credentials).
func CacheDir() string { return filepath.Join(Dir(), "cache") }

// EnvFilePath is the user-level dotenv file (~/.secopsctl/.env). It may hold
// long-lived, user-supplied secrets such as the SOAR AppKey and is created 0600.
// The mintable OAuth token is never written here.
func EnvFilePath() string { return filepath.Join(Dir(), ".env") }

// SetEnvVar writes key=value into the user-level .env file, replacing any
// existing assignment for key and preserving every other line (comments
// included). The directory is created 0700 and the file 0600. value is written
// verbatim on a single line (no quoting), which suits token-shaped secrets.
// Returns the file path written.
func SetEnvVar(key, value string) (string, error) {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := EnvFilePath()

	var kept []string
	switch data, err := os.ReadFile(path); {
	case err == nil:
		for line := range strings.SplitSeq(string(data), "\n") {
			// Drop an existing assignment (optionally "export "-prefixed) for key;
			// keep blank-trimmed-empty trailing split artifacts out.
			t := strings.TrimSpace(line)
			if t == "" {
				continue
			}
			if k, _, ok := strings.Cut(strings.TrimPrefix(t, "export "), "="); ok && strings.TrimSpace(k) == key {
				continue
			}
			kept = append(kept, line)
		}
	case errors.Is(err, fs.ErrNotExist):
		// First write; nothing to preserve.
	default:
		return "", err
	}

	kept = append(kept, key+"="+value)
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
