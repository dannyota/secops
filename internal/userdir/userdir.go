// Package userdir resolves the secopsctl user directory.
//
// By default this is ~/.secopsctl (override with $SECOPSCTL_HOME). It provides a
// config discovery location and a cache directory for NON-SECRET data — additive
// to environment variables and the existing config discovery, never a
// replacement.
//
// It deliberately stores no credentials: the OAuth access token is minted
// in-process by the Google auth library (see package danny.vn/secops/auth) and
// is never persisted to disk here.
//
//	~/.secopsctl/
//	├── instance.yaml   # a config discovery location
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

// InstanceConfigPath is the config location discovered under the home dir.
func InstanceConfigPath() string { return filepath.Join(Dir(), "instance.yaml") }

// CacheDir is the non-secret cache directory (reserved for cached data such as
// log-type catalogs or last-pull state — never credentials).
func CacheDir() string { return filepath.Join(Dir(), "cache") }
