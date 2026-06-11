package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Build metadata. A release sets these via the linker:
//
//	go build -ldflags "-X danny.vn/secops/internal/cli.version=v1.2.3 \
//	  -X danny.vn/secops/internal/cli.commit=$(git rev-parse HEAD) \
//	  -X danny.vn/secops/internal/cli.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// When unset (a plain `go build`/`go install`), they fall back to the VCS info the
// Go toolchain embeds in the binary (debug.ReadBuildInfo), so even an un-stamped
// build reports a useful commit/version.
var (
	version = ""
	commit  = ""
	date    = ""
)

// BuildInfo is the resolved version metadata.
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date,omitempty"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// resolveBuildInfo merges ldflags-stamped values with the toolchain-embedded VCS
// info, preferring the explicit ldflags values.
func resolveBuildInfo() BuildInfo {
	bi := BuildInfo{
		Version: version, Commit: commit, Date: date,
		GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		// Main.Version is "(devel)" for an unstamped local build — normalize that to
		// the same "dev" the empty fallback uses, but keep a real module version.
		if bi.Version == "" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			bi.Version = info.Main.Version
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if bi.Commit == "" {
					bi.Commit = s.Value
				}
			case "vcs.time":
				if bi.Date == "" {
					bi.Date = s.Value
				}
			}
		}
	}
	if bi.Version == "" {
		bi.Version = "dev"
	}
	if bi.Commit == "" {
		bi.Commit = "unknown"
	}
	return bi
}

// shortCommit trims a full git SHA to 12 chars for display.
func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// versionLine is a compact one-line version string for doctor/help.
func versionLine() string {
	bi := resolveBuildInfo()
	return fmt.Sprintf("secopsctl %s (%s, %s %s/%s)",
		bi.Version, shortCommit(bi.Commit), bi.GoVersion, bi.OS, bi.Arch)
}

func init() {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the secopsctl version, commit, and build info",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bi := resolveBuildInfo()
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(bi)
			}
			fmt.Printf("secopsctl %s\n", bi.Version)
			fmt.Printf("  commit:  %s\n", bi.Commit)
			if bi.Date != "" {
				fmt.Printf("  built:   %s\n", bi.Date)
			}
			fmt.Printf("  go:      %s\n", bi.GoVersion)
			fmt.Printf("  platform: %s/%s\n", bi.OS, bi.Arch)
			return nil
		},
	}
	// Also set the cobra Version so `secopsctl --version` works.
	rootCmd.Version = resolveBuildInfo().Version
	rootCmd.AddCommand(markJSON(cmd))
}
