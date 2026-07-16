package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// Case evidence — attach a forensic artifact (file/log snippet) to a case, and
// read one back. add is a guarded LIVE mutation; the evidence has no delete API,
// so attach deliberately (a wrong attach can only be lived with).
func newCaseEvidenceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evidence <verb>",
		Short: "Manage case evidence: attach a file (guarded) or get one back (read-only)",
		Long: "Attach a forensic artifact to a case and read it back. NOTE: the API has no\n" +
			"evidence-delete, so a wrong `add` cannot be removed — attach deliberately.",
	}
	cmd.AddCommand(newCaseEvidenceAddCmd(), newCaseEvidenceGetCmd())
	return cmd
}

func newCaseEvidenceAddCmd() *cobra.Command {
	var (
		caseID                   int
		file, name, evType, desc string
		dryRun, yes              bool
	)
	cmd := &cobra.Command{
		Use:   "add --id N --file <path> --name <s>",
		Short: "Attach a file as evidence to a case (guarded)",
		Long: "Attach a file to a case as evidence — the bytes are base64-encoded and sent\n" +
			"as the evidence blob. --id is the SOAR integer case id. Guarded: dry-run by\n" +
			"default, --yes to apply. The API has no delete, so this is one-way.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Stat now for the preview, encode only on apply — the dry-run
			// preview must not dump a base64 blob to stdout.
			st, err := os.Stat(file)
			if err != nil {
				return err
			}
			preview := map[string]any{
				"caseIdentifier": caseID,
				"base64Blob":     fmt.Sprintf("<%d bytes from %s — encoded on apply>", st.Size(), file),
				"name":           name,
				"type":           evType,
				"description":    desc,
				"isImportant":    false,
			}
			return caseAction(fmt.Sprintf("attach evidence %q (%d bytes) to case %d", name, st.Size(), caseID), preview, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					data, rerr := os.ReadFile(file)
					if rerr != nil {
						return nil, rerr
					}
					body := map[string]any{
						"caseIdentifier": caseID,
						"base64Blob":     base64.StdEncoding.EncodeToString(data),
						"name":           name,
						"type":           evType,
						"description":    desc,
						"isImportant":    false,
					}
					return lc.AddEvidence(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id (required)")
	f.StringVar(&file, "file", "", "file to attach (required)")
	f.StringVar(&name, "name", "", "evidence name (required)")
	f.StringVar(&evType, "type", "", "evidence type (free-text label)")
	f.StringVar(&desc, "description", "", "evidence description")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("file")
	_ = cmd.MarkFlagRequired("name")
	return markJSON(cmd)
}

func newCaseEvidenceGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <evidence-id>",
		Short: "Read-only: get one piece of case evidence by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := c.GetEvidenceData(baseContext(), args[0])
			if err != nil {
				return err
			}
			return writeRawJSON(os.Stdout, raw)
		},
	}
	return markJSON(cmd)
}
