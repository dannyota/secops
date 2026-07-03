package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newCaseCustomFieldsCmd() *cobra.Command {
	var caseID int
	cmd := &cobra.Command{
		Use:   "custom-fields --case-id N",
		Short: "List custom field values on a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			raw, err := mc.ListCustomFieldValues(baseContext(), caseID)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "custom field values", raw)
			return nil
		},
	}
	cmd.Flags().IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
}

func newCaseWallCmd() *cobra.Command {
	var caseID int
	cmd := &cobra.Command{
		Use:   "wall --case-id N",
		Short: "List case wall timeline records",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			raw, err := mc.ListCaseWallRecords(baseContext(), caseID)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			return emitCaseWall(cmd.OutOrStdout(), raw)
		},
	}
	cmd.Flags().IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
}

// caseWallRecord is one entry on the case wall — the case's chronological
// automation+analyst timeline. The human-readable content lives in
// activityDataJson (a nested JSON string), keyed by activityKind.
type caseWallRecord struct {
	ActivityKind     string          `json:"activityKind"`
	CreatorUserID    string          `json:"creatorUserId"`
	CreateTime       json.RawMessage `json:"createTime"` // unix-millis number OR RFC3339 string, surface-dependent
	AlertIdentifier  string          `json:"alertIdentifier"`
	ActivityDataJSON string          `json:"activityDataJson"`
}

// emitCaseWall renders the case wall as a timeline (oldest first): time · kind ·
// what happened — so playbook attachments, action results, alert grouping, and
// status/stage changes are visible from the CLI, not hidden behind a bare count.
func emitCaseWall(w io.Writer, raw json.RawMessage) error {
	var resp struct {
		CaseWallRecords []caseWallRecord `json:"caseWallRecords"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	recs := resp.CaseWallRecords
	if len(recs) == 0 {
		fmt.Fprintln(w, "no case wall records.")
		return nil
	}
	sort.SliceStable(recs, func(i, j int) bool { return wallEpoch(recs[i].CreateTime) < wallEpoch(recs[j].CreateTime) })

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tKIND\tACTIVITY")
	for i := range recs {
		r := &recs[i]
		fmt.Fprintf(tw, "%s\t%s\t%s\n", fmtWallTime(r.CreateTime), orDash(r.ActivityKind), truncate(wallActivity(r.ActivityDataJSON), 72))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\n%d record(s) (oldest first; --json for the full records).\n", len(recs))
	return nil
}

// wallActivity pulls the human-readable line from a wall record's nested
// activityDataJson (the `comment`, with `activityDescription` as a fallback).
func wallActivity(dataJSON string) string {
	if strings.TrimSpace(dataJSON) == "" {
		return ""
	}
	var d struct {
		Comment             string `json:"comment"`
		ActivityDescription string `json:"activityDescription"`
	}
	if json.Unmarshal([]byte(dataJSON), &d) != nil {
		return ""
	}
	if c := strings.TrimSpace(d.Comment); c != "" && c != "None" {
		return c
	}
	if a := strings.TrimSpace(d.ActivityDescription); a != "" && a != "None" {
		return a
	}
	return ""
}

// wallEpoch returns a sortable unix-millis value from a wall createTime, which is
// a number (unix millis) on the modern surface or an RFC3339 string elsewhere.
func wallEpoch(raw json.RawMessage) int64 {
	s := strings.Trim(string(raw), `"`)
	if s == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UnixMilli()
	}
	var n int64
	_, _ = fmt.Sscan(s, &n)
	return n
}

// fmtWallTime renders a wall record's timestamp compactly (local
// "2006-01-02 15:04") from either a unix-millis number or an RFC3339 string.
func fmtWallTime(raw json.RawMessage) string {
	ms := wallEpoch(raw)
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04")
}

func newCaseContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context <verb>",
		Short: "Case-level key-value context properties (playbook state)",
	}
	cmd.AddCommand(newCaseContextListCmd(), newCaseContextSetCmd())
	return cmd
}

func newCaseContextListCmd() *cobra.Command {
	var caseID int
	cmd := &cobra.Command{
		Use:   "list --case-id N",
		Short: "List context properties on a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			raw, err := mc.ListContextProperties(baseContext(), caseID)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "context properties", raw)
			return nil
		},
	}
	cmd.Flags().IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
}

func newCaseContextSetCmd() *cobra.Command {
	var (
		caseID int
		key    string
		value  string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "set --id N --key <k> --value <v>",
		Short: "MUTATING (guarded): set a context property on a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--id is required")
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" {
				return fmt.Errorf("--key is required")
			}
			label := fmt.Sprintf("case %d context set %s", caseID, key)
			dr, ay := soarGuard(label, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Case: %d\nKey: %s\nValue: %s\n", caseID, key, value)
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN — no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to set without confirmation (pass --yes). Aborted.")
				return nil
			}
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			body := map[string]any{"key": key, "value": value}
			_, err = mc.SetContextProperty(baseContext(), caseID, body)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "context property %q set on case %d.\n", key, caseID)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id (required) — from 'soar case list'")
	f.IntVar(&caseID, "case-id", 0, "deprecated alias of --id")
	_ = f.MarkHidden("case-id")
	f.StringVar(&key, "key", "", "property key (required)")
	f.StringVar(&value, "value", "", "property value")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}
