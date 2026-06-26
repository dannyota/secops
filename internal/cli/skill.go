package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	skill "danny.vn/secops/skills/secopsctl"
)

// skillCommand is the invocation that prints the embedded operating guide. It is
// referenced from root help and capabilities so the advertised name stays in one place.
const skillCommand = "secopsctl skill"

// `secopsctl skill` makes the agent operating guide self-served from the binary.
// An install via `go install danny.vn/secops/cmd/secopsctl@latest` ships only the
// binary, not the repo's skills/ tree, so an agent has no other way to obtain the
// guide. `skill` prints it; `skill install` writes it into an agent skills
// directory so the harness can detect it as a first-class skill.

func init() {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Print the agent operating guide embedded in the binary (offline)",
		Long: "Emit the secopsctl agent operating guide baked into the binary: the\n" +
			"two-auth-plane model, the mutation ritual, the config-as-code loop, the\n" +
			"self-discovery commands, and the gotchas the per-command --help can't express.\n\n" +
			"Self-contained — `go install danny.vn/secops/cmd/secopsctl@latest` ships only\n" +
			"the binary, so this is how an agent retrieves the guide without the repo:\n" +
			"  secopsctl skill            print the guide (--json wraps {name,description,body})\n" +
			"  secopsctl skill install    write it into an agent skills dir for auto-detection\n\n" +
			"Detect-then-install: an agent reads `secopsctl skill --json` to discover the\n" +
			"skill, then (once the user approves) runs `secopsctl skill install`.",
		Args: cobra.NoArgs,
		RunE: runSkill,
	}
	cmd.AddCommand(newSkillInstallCmd())
	rootCmd.AddCommand(markJSON(cmd))
}

func runSkill(cmd *cobra.Command, _ []string) error {
	if jsonOut {
		return emitJSON(skill.Parse())
	}
	// Print the embedded bytes verbatim so the on-screen guide matches the
	// installed file exactly (SKILL.md ends with a newline).
	fmt.Print(skill.Markdown())
	return nil
}

func newSkillInstallCmd() *cobra.Command {
	var dir string
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the operating guide into an agent skills directory",
		Long: "Write the embedded guide to <skills-dir>/secopsctl/SKILL.md so an agent\n" +
			"harness detects it as a first-class skill. The default skills directory is\n" +
			"$CLAUDE_CONFIG_DIR/skills (or ~/.claude/skills); --dir overrides it (e.g.\n" +
			"--dir ./.claude/skills to install into the current project). Writing a local\n" +
			"file only — no network, no live instance. Idempotent: a no-op when the file\n" +
			"already matches; if it exists with different content it is left untouched\n" +
			"unless --force (so a hand-tuned copy is never clobbered silently).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			base := dir
			if base == "" {
				base = defaultSkillsDir()
			}
			dest := filepath.Join(base, "secopsctl", "SKILL.md")
			doc := skill.Parse()
			name := doc.Name
			if name == "" {
				name = "secopsctl" // defensive: never emit an empty `/` invoke hint
			}

			status := "installed"
			if existing, err := os.ReadFile(dest); err == nil {
				switch {
				case string(existing) == skill.Markdown():
					status = "unchanged"
				case !force:
					return fmt.Errorf("%s already exists with different content; pass --force to overwrite", dest)
				default:
					status = "updated"
				}
			}

			if status != "unchanged" {
				if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
					return fmt.Errorf("create skills dir: %w", err)
				}
				if err := os.WriteFile(dest, []byte(skill.Markdown()), 0o644); err != nil {
					return fmt.Errorf("write skill: %w", err)
				}
			}

			if jsonOut {
				return emitJSON(map[string]string{"name": name, "path": dest, "status": status})
			}
			fmt.Printf("Skill %q %s -> %s\n", name, status, dest)
			fmt.Printf("The agent harness picks it up on its next session; invoke it as /%s.\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "skills directory to install into (default $CLAUDE_CONFIG_DIR/skills or ~/.claude/skills)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing SKILL.md whose content differs")
	return markJSON(cmd)
}

// defaultSkillsDir resolves the agent skills directory: $CLAUDE_CONFIG_DIR/skills
// if set, else ~/.claude/skills. Falls back to ./.claude/skills if no home dir.
func defaultSkillsDir() string {
	if cfg := os.Getenv("CLAUDE_CONFIG_DIR"); cfg != "" {
		return filepath.Join(cfg, "skills")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".claude", "skills")
	}
	return filepath.Join(home, ".claude", "skills")
}
