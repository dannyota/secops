package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func init() {
	cmd := newMCPCmd()
	rootCmd.AddCommand(cmd)
}

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Model Context Protocol: serve secopsctl as an MCP server",
	}
	cmd.AddCommand(newMCPServeCmd(), newMCPInstallCmd())
	return cmd
}

func newMCPInstallCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register secopsctl in Claude Code MCP settings",
		Long: "Add secopsctl as an MCP server in Claude Code settings so every\n" +
			"session gets secopsctl tools automatically. Writes to\n" +
			".claude/settings.json (project, default) or ~/.claude/settings.json (--global).\n" +
			"Idempotent — updates the entry if it already exists.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runMCPInstall(global)
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "install into ~/.claude/settings.json instead of project-level")
	return markJSON(cmd)
}

func runMCPInstall(global bool) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve secopsctl binary path: %w", err)
	}
	self, _ = filepath.Abs(self)

	settingsPath := filepath.Join(".claude", "settings.json")
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot resolve home directory: %w", err)
		}
		settingsPath = filepath.Join(home, ".claude", "settings.json")
	}

	settings := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	}

	servers, _ := settings["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["secopsctl"] = map[string]any{
		"command": self,
		"args":    []string{"mcp", "serve"},
	}
	settings["mcpServers"] = servers

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o750); err != nil {
		return err
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, append(out, '\n'), 0o644); err != nil {
		return err
	}

	if jsonOut {
		return emitJSON(map[string]string{
			"path":    settingsPath,
			"binary":  self,
			"status":  "installed",
			"command": "secopsctl mcp serve",
		})
	}
	fmt.Printf("Registered secopsctl MCP server in %s\n", settingsPath)
	fmt.Printf("  command: %s mcp serve\n", self)
	fmt.Println("Restart Claude Code to pick up the new server.")
	return nil
}
