package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// The `data-access` command manages data-access RBAC — the labels that tag data
// and the scopes that grant principals access to labelled data. These control
// who can see which events; previously console-only. Mutations are guarded.
func init() { rootCmd.AddCommand(newDataAccessCmd()) }

// daRow is the common display shape for a label or a scope.
type daRow struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

func newDataAccessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data-access <labels|scopes> <verb>",
		Short: "Manage data-access RBAC: labels (tag data) and scopes (grant access)",
		Long: "Data-access RBAC controls who can see which data:\n" +
			"  labels — tag events for access control (list/get/create/delete);\n" +
			"  scopes — grant principals access to labelled data (list/get/create/delete).\n" +
			"create/delete are live mutations (guarded: --dry-run by default, --yes to apply).",
	}
	cmd.AddCommand(newDataAccessLabelsCmd(), newDataAccessScopesCmd())
	return cmd
}

func newDataAccessLabelsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "labels <verb>", Short: "Data-access labels (tag data for access control)"}
	cmd.AddCommand(
		daListCmd("label", func() ([]daRow, []json.RawMessage, error) {
			c, err := newChronicleClient()
			if err != nil {
				return nil, nil, err
			}
			ls, err := c.ListDataAccessLabels(baseContext())
			if err != nil {
				return nil, nil, err
			}
			rows := make([]daRow, len(ls))
			raws := make([]json.RawMessage, len(ls))
			for i, l := range ls {
				rows[i] = daRow{l.ID, l.DisplayName, l.Description}
				raws[i] = l.Raw
			}
			return rows, raws, nil
		}),
		daGetCmd("label", func(id string) (json.RawMessage, error) {
			c, err := newChronicleClient()
			if err != nil {
				return nil, err
			}
			l, err := c.GetDataAccessLabel(baseContext(), id)
			if err != nil {
				return nil, err
			}
			return l.Raw, nil
		}),
		daCreateCmd("label", func(id string, body any) (json.RawMessage, error) {
			c, err := newChronicleClient()
			if err != nil {
				return nil, err
			}
			l, err := c.CreateDataAccessLabel(baseContext(), id, body)
			if err != nil {
				return nil, err
			}
			return l.Raw, nil
		}),
		daDeleteCmd("label", func(id string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			return c.DeleteDataAccessLabel(baseContext(), id)
		}),
	)
	return cmd
}

func newDataAccessScopesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "scopes <verb>", Short: "Data-access scopes (grant principals access to labelled data)"}
	cmd.AddCommand(
		daListCmd("scope", func() ([]daRow, []json.RawMessage, error) {
			c, err := newChronicleClient()
			if err != nil {
				return nil, nil, err
			}
			ss, err := c.ListDataAccessScopes(baseContext())
			if err != nil {
				return nil, nil, err
			}
			rows := make([]daRow, len(ss))
			raws := make([]json.RawMessage, len(ss))
			for i, s := range ss {
				rows[i] = daRow{s.ID, s.DisplayName, s.Description}
				raws[i] = s.Raw
			}
			return rows, raws, nil
		}),
		daGetCmd("scope", func(id string) (json.RawMessage, error) {
			c, err := newChronicleClient()
			if err != nil {
				return nil, err
			}
			s, err := c.GetDataAccessScope(baseContext(), id)
			if err != nil {
				return nil, err
			}
			return s.Raw, nil
		}),
		daCreateCmd("scope", func(id string, body any) (json.RawMessage, error) {
			c, err := newChronicleClient()
			if err != nil {
				return nil, err
			}
			s, err := c.CreateDataAccessScope(baseContext(), id, body)
			if err != nil {
				return nil, err
			}
			return s.Raw, nil
		}),
		daDeleteCmd("scope", func(id string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			return c.DeleteDataAccessScope(baseContext(), id)
		}),
	)
	return cmd
}

func daListCmd(kind string, fn func() ([]daRow, []json.RawMessage, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Read-only: list data-access " + kind + "s",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			rows, raws, err := fn()
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(raws)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tDISPLAY NAME\tDESCRIPTION")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", r.ID, orDash(r.DisplayName), orDash(r.Description))
			}
			return tw.Flush()
		},
	}
	return markJSON(cmd)
}

func daGetCmd(kind string, fn func(string) (json.RawMessage, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Read-only: get one data-access " + kind,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			raw, err := fn(args[0])
			if err != nil {
				return err
			}
			return writeRawJSON(os.Stdout, raw)
		},
	}
	return markJSON(cmd)
}

func daCreateCmd(kind string, fn func(string, any) (json.RawMessage, error)) *cobra.Command {
	var id, file string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "create --id <id> --file <def.json>",
		Short: "Create a data-access " + kind + " from a JSON definition (guarded)",
		Long: "Create a data-access " + kind + " with the given id from a JSON definition\n" +
			"file (the body the API expects — displayName, description, and the " + kind + "'s\n" +
			"rules). Guarded: dry-run by default, --yes to apply. Re-pull afterwards.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			var body any
			if err := json.Unmarshal(data, &body); err != nil {
				return fmt.Errorf("parse %s: %w", file, err)
			}
			target := fmt.Sprintf("create data-access %s %q from %s", kind, id, file)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would create %s %q from %s. Re-run with --yes.\n", kind, id, file)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to create without confirmation (pass --yes). Aborted.")
				return nil
			}
			raw, err := fn(id, body)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			fmt.Printf("Created data-access %s %q.\n", kind, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "id for the new "+kind+" (required)")
	cmd.Flags().StringVar(&file, "file", "", "JSON definition file (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("file")
	return markJSON(cmd)
}

func daDeleteCmd(kind string, fn func(string) error) *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a data-access " + kind + " (guarded)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			target := fmt.Sprintf("delete data-access %s %q", kind, id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would delete %s %q. Re-run with --yes.\n", kind, id)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to delete without confirmation (pass --yes). Aborted.")
				return nil
			}
			if err := fn(id); err != nil {
				return err
			}
			fmt.Printf("Deleted data-access %s %q.\n", kind, id)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}
