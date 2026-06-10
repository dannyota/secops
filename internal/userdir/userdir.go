// Package userdir resolves the secopsctl user directory.
//
// By default this is ~/.secopsctl (override with $SECOPSCTL_HOME). It provides
// the default config location and a cache directory for NON-SECRET data —
// additive to the --config flag and the existing config discovery.
//
// The mintable OAuth access token is NEVER written here: it is minted in-process
// by the Google auth library (see package danny.vn/secops/auth). The instance
// config (~/.secopsctl/instance.yaml, written by `secopsctl config`) may hold a
// long-lived, non-mintable secret such as the SOAR AppKey; that file is created
// 0600 by the config package and is git-ignored, never committed.
//
//	~/.secopsctl/
//	├── instance.yaml   # the default config location (0600 when it holds a key)
//	├── audit.jsonl     # guard-decision audit log for live mutations (0600)
//	└── cache/          # non-secret cached data (reserved)
package userdir

import (
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

// InstanceConfigPath is the default config location under the home dir.
func InstanceConfigPath() string { return filepath.Join(Dir(), "instance.yaml") }

// CacheDir is the non-secret cache directory (reserved for cached data such as
// log-type catalogs or last-pull state — never credentials).
func CacheDir() string { return filepath.Join(Dir(), "cache") }
