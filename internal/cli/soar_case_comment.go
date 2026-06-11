package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// `soar case comment` — case comments, the canonical triage-rationale record on
// the case wall (distinct from `soar case chat`, the analyst messaging surface):
// comments land in the case timeline and case reports, so this is where an
// analyst — or an agent — records verdict justification and investigation notes.
// `list` reads; `add` is a guarded mutation on the standard case-verb scaffold.

func newCaseCommentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment <verb>",
		Short: "Case comments: list (read) + guarded add — the triage-rationale record",
		Long: "Read and write case comments — the case-wall record where triage rationale\n" +
			"and investigation notes belong (case *chat* is the separate analyst-messaging\n" +
			"surface; close-time notes ride `close --comment`).",
	}
	cmd.AddCommand(newCaseCommentListCmd(), newCaseCommentAddCmd())
	return cmd
}

func newCaseCommentAddCmd() *cobra.Command {
	var (
		caseID      int
		alert, text string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "add --id N --text <s>",
		Short: "Add a comment to a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(text) == "" {
				return fmt.Errorf("--text must not be empty")
			}
			body := caseBody(caseID, alert)
			body["comment"] = text
			return caseAction(fmt.Sprintf("add comment to case %d", caseID), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.AddCaseComment(ctx, body)
				})
		},
	}
	caseGuardFlags(cmd, &caseID, &alert, &dryRun, &yes, true)
	cmd.Flags().StringVar(&text, "text", "", "comment text (required)")
	_ = cmd.MarkFlagRequired("text")
	return markJSON(cmd)
}

// caseCommentView is the subset of a comment record we render in `list`. The
// payload is schema-unstable across revisions, so the fields decode tolerantly
// and --json carries the raw records.
type caseCommentView struct {
	Comment        string `json:"comment"`
	Creator        string `json:"creatorUserId"`
	CreatorName    string `json:"creatorFullName"`
	CreationTimeMs int64  `json:"creationTimeUnixTimeInMs"`
	ModifiedTimeMs int64  `json:"modificationTimeUnixTimeInMs"`
}

func newCaseCommentListCmd() *cobra.Command {
	var (
		caseID int
		alert  string
	)
	cmd := &cobra.Command{
		Use:   "list --id N",
		Short: "Read-only: list a case's comments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			q := url.Values{"CaseId": {strconv.Itoa(caseID)}}
			if alert != "" {
				q.Set("AlertIdentifier", alert)
			}
			raw, err := lc.CaseXListComments(baseContext(), q)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			return emitCaseComments(os.Stdout, raw)
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id (required)")
	f.StringVar(&alert, "alert", "", "optional alert identifier to scope the listing")
	_ = cmd.MarkFlagRequired("id")
	return markJSON(cmd)
}

// emitCaseComments renders the comment records compactly. The live endpoint
// returns a bare array; an {items|comments: []} wrap is accepted as a fallback
// because sibling legacy list endpoints have shipped wrapped shapes across
// revisions (e.g. the case-values objectsList wrap).
func emitCaseComments(w io.Writer, raw json.RawMessage) error {
	var records []caseCommentView
	if err := json.Unmarshal(raw, &records); err != nil {
		var wrap struct {
			Items    []caseCommentView `json:"items"`
			Comments []caseCommentView `json:"comments"`
		}
		if err := json.Unmarshal(raw, &wrap); err != nil {
			return fmt.Errorf("decode comments: %w", err)
		}
		records = wrap.Items
		if len(records) == 0 {
			records = wrap.Comments
		}
	}
	if len(records) == 0 {
		fmt.Fprintln(w, "no comments.")
		return nil
	}
	for i, c := range records {
		author := c.CreatorName
		if author == "" {
			author = c.Creator
		}
		fmt.Fprintf(w, "%d. %s  —  %s\n", i+1, msToUTC(c.CreationTimeMs), orDash(author))
		fmt.Fprintf(w, "   %s\n", strings.TrimSpace(c.Comment))
	}
	fmt.Fprintf(w, "\n%d comment(s).\n", len(records))
	return nil
}
