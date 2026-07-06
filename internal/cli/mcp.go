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
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register secopsctl in the project .mcp.json",
		Long: "Add secopsctl as an MCP server in the project-level .mcp.json so\n" +
			"every Claude Code session in this directory gets secopsctl tools\n" +
			"automatically. Idempotent — updates the entry if it already exists.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runMCPInstall()
		},
	}
	return markJSON(cmd)
}

func runMCPInstall() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve secopsctl binary path: %w", err)
	}
	self, _ = filepath.Abs(self)

	const mcpFile = ".mcp.json"

	config := map[string]any{}
	if data, err := os.ReadFile(mcpFile); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("parse %s: %w", mcpFile, err)
		}
	}

	servers, _ := config["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["secopsctl"] = map[string]any{
		"command": self,
		"args":    []string{"mcp", "serve"},
	}
	config["mcpServers"] = servers

	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(mcpFile, append(out, '\n'), 0o644); err != nil {
		return err
	}

	if jsonOut {
		return emitJSON(map[string]string{
			"path":    mcpFile,
			"binary":  self,
			"status":  "installed",
			"command": "secopsctl mcp serve",
		})
	}
	fmt.Printf("Registered secopsctl MCP server in %s\n", mcpFile)
	fmt.Printf("  command: %s mcp serve\n", self)
	fmt.Println("Restart Claude Code to pick up the new server.")
	return nil
}
