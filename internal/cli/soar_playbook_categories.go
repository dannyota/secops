package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

type categoryRow struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

func newSOARPlaybookCategoriesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "categories",
		Aliases: []string{"folders"},
		Short:   "Manage playbook categories (folders)",
	}
	cmd.AddCommand(
		newSOARPlaybookCategoryListCmd(),
		newSOARPlaybookCategoryCreateCmd(),
		newSOARPlaybookCategoryRenameCmd(),
		newSOARPlaybookCategoryDeleteCmd(),
	)
	return cmd
}

func newSOARPlaybookCategoryListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List playbook categories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.ListWorkflowCategories(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			rows := parseCategoryRows(raw)
			printCategoryRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	return markJSON(cmd)
}

func newSOARPlaybookCategoryCreateCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "MUTATING (guarded): create a playbook category",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			isDry, _ := soarGuard("create playbook category "+name, dryRun, yes)
			if isDry {
				fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] would create category %q\n", name)
				return nil
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.AddOrUpdatePlaybookCategory(baseContext(), map[string]any{
				"name": name,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			var created struct {
				ID int `json:"id"`
			}
			_ = json.Unmarshal(raw, &created)
			fmt.Fprintf(cmd.OutOrStdout(), "created category %q (id=%d)\n", name, created.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview without mutating")
	f.BoolVar(&yes, "yes", false, "skip confirmation prompt")
	return markJSON(cmd)
}

func newSOARPlaybookCategoryRenameCmd() *cobra.Command {
	var (
		newName string
		dryRun  bool
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "rename <name|id> --name <new-name>",
		Short: "MUTATING (guarded): rename a playbook category",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if newName == "" {
				return fmt.Errorf("--name is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			id, oldName, err := resolveCategory(lc, args[0])
			if err != nil {
				return err
			}
			isDry, _ := soarGuard(fmt.Sprintf("rename category %q → %q", oldName, newName), dryRun, yes)
			if isDry {
				fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] would rename category %q (id=%d) to %q\n", oldName, id, newName)
				return nil
			}
			_, err = lc.AddOrUpdatePlaybookCategory(baseContext(), map[string]any{
				"id":   id,
				"name": newName,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "renamed category %q → %q\n", oldName, newName)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&newName, "name", "", "new category name (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview without mutating")
	f.BoolVar(&yes, "yes", false, "skip confirmation prompt")
	return cmd
}

func newSOARPlaybookCategoryDeleteCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "delete <name|id>",
		Short: "MUTATING (guarded): delete a playbook category",
		Long:  "Delete a playbook category (folder). Fails if the category still contains playbooks.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			id, name, err := resolveCategory(lc, args[0])
			if err != nil {
				return err
			}
			isDry, _ := soarGuard(fmt.Sprintf("delete category %q (id=%d)", name, id), dryRun, yes)
			if isDry {
				fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] would delete category %q (id=%d)\n", name, id)
				return nil
			}
			_, err = lc.RemovePlaybookCategories(baseContext(), map[string]any{
				"ids": []int{id},
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted category %q (id=%d)\n", name, id)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview without mutating")
	f.BoolVar(&yes, "yes", false, "skip confirmation prompt")
	return cmd
}

func newSOARPlaybookMoveCmd() *cobra.Command {
	var (
		folder string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "move <name|identifier> --folder <category>",
		Short: "MUTATING (guarded): move a playbook to a different category",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if folder == "" {
				return fmt.Errorf("--folder is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			identifier := args[0]
			if !looksLikeUUID(identifier) {
				identifier, err = resolvePlaybookDefinition(ctx, lc, args[0])
				if err != nil {
					return err
				}
			}

			catID, catName, err := resolveCategory(lc, folder)
			if err != nil {
				return err
			}

			isDry, _ := soarGuard(fmt.Sprintf("move playbook %s → category %q", args[0], catName), dryRun, yes)
			if isDry {
				fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] would move %s to category %q (id=%d)\n", args[0], catName, catID)
				return nil
			}

			_, err = lc.MoveDefinitionsToCategory(ctx, map[string]any{
				"category":    catID,
				"identifiers": []string{identifier},
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "moved %s to category %q\n", args[0], catName)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&folder, "folder", "", "target category name or id (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview without mutating")
	f.BoolVar(&yes, "yes", false, "skip confirmation prompt")
	return cmd
}

// resolveCategory maps a category name or numeric id to (id, name).
func resolveCategory(lc *legacy.Client, nameOrID string) (int, string, error) {
	return resolveCategoryCtx(baseContext(), lc, nameOrID)
}

func resolveCategoryCtx(ctx context.Context, lc *legacy.Client, nameOrID string) (int, string, error) {
	type catEntry struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	raw, err := lc.ListWorkflowCategories(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("list categories: %w", err)
	}
	var cats []catEntry
	if err := json.Unmarshal(raw, &cats); err != nil {
		return 0, "", fmt.Errorf("decode categories: %w", err)
	}

	if numID, err := strconv.Atoi(nameOrID); err == nil {
		for _, c := range cats {
			if c.ID == numID {
				return c.ID, c.Name, nil
			}
		}
		return 0, "", fmt.Errorf("category id %d not found", numID)
	}

	lower := strings.ToLower(nameOrID)
	for _, c := range cats {
		if strings.ToLower(c.Name) == lower {
			return c.ID, c.Name, nil
		}
	}
	var names []string
	for _, c := range cats {
		names = append(names, c.Name)
	}
	return 0, "", fmt.Errorf("category %q not found (available: %s)", nameOrID, strings.Join(names, ", "))
}

func parseCategoryRows(raw json.RawMessage) []categoryRow {
	var cats []struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		Type      int    `json:"type"`
		IsDefault bool   `json:"isDefaultCategory"`
	}
	_ = json.Unmarshal(raw, &cats)
	rows := make([]categoryRow, len(cats))
	for i, c := range cats {
		typeName := "Regular"
		switch c.Type {
		case 2:
			typeName = "SystemDefault"
		case 3:
			typeName = "GeneratedFromAlertPlaybook"
		}
		rows[i] = categoryRow{
			ID:        c.ID,
			Name:      c.Name,
			Type:      typeName,
			IsDefault: c.IsDefault,
		}
	}
	return rows
}

func printCategoryRows(w io.Writer, rows []categoryRow) {
	fmt.Fprintln(w, "ID\tNAME\tTYPE\tDEFAULT")
	for _, r := range rows {
		fmt.Fprintf(w, "%d\t%s\t%s\t%v\n", r.ID, r.Name, r.Type, r.IsDefault)
	}
}
