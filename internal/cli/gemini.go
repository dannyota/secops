package cli

import (
	"errors"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// gemini.go groups the AI / Gemini-powered features (the console's "Get the help
// of AI" / "Gemini Investigations"): generate a UDM query from natural language,
// run an NL search, and ask the Gemini assistant. Deterministic search lives under
// `secopsctl search`.

func init() {
	rootCmd.AddCommand(newGeminiCmd())
}

func newGeminiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gemini",
		Short: "Run AI (Gemini) features: generate queries, investigate alerts, summarize cases",
		Long: "All Gemini-powered features consolidated in one group:\n" +
			"  generate-query  turn a natural-language question into a UDM query (don't run it)\n" +
			"  search          turn a natural-language question into a UDM query AND run it\n" +
			"  ask             ask the Gemini assistant a question (YARA-L help, UDM fields, …)\n" +
			"  investigate     run the AI investigation for an alert (verdict, confidence, next steps)\n" +
			"  summarize       AI-powered case summary (reasons + recommended next steps)\n" +
			"  generate        generate a playbook definition from a description or case context\n\n" +
			"Deterministic search lives under `secopsctl search`.",
	}
	cmd.AddCommand(
		newGeminiGenerateQueryCmd(),
		newGeminiSearchCmd(),
		newGeminiAskCmd(),
		newAlertsInvestigateCmd(),
		newCaseSummarizeCmd(),
		newSOARPlaybookGenerateCmd(),
	)
	return cmd
}

// newGeminiGenerateQueryCmd turns natural language into a UDM query via Gemini
// and prints it (with the model's suggested window) — it does not run a search.
func newGeminiGenerateQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "generate-query <natural language>",
		Aliases: []string{"translate"},
		Short:   "Generate a UDM query from natural language (Gemini); do not run it",
		Long: "Translate a plain-English question into a UDM query via Gemini and print it,\n" +
			"along with any time window the model inferred. Use `gemini search` to also run it.",
		Example: "  secopsctl gemini generate-query 'failed logins to admin accounts in the last hour'",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			res, err := c.TranslateNLToUDMWithTimeRange(baseContext(), strings.Join(args, " "))
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Println(res.Query)
			if res.TimeRange != nil {
				fmt.Fprintf(os.Stderr, "suggested window: %s … %s\n",
					res.TimeRange.StartTime.Format(time.RFC3339), res.TimeRange.EndTime.Format(time.RFC3339))
			}
			return nil
		},
	}
	return markJSON(cmd)
}

// newGeminiSearchCmd turns natural language into a UDM query via Gemini and runs
// it; the model's suggested window is used unless --hours/--from is set.
func newGeminiSearchCmd() *cobra.Command {
	var (
		nlHours   int
		nlLimit   int
		o         resultOutput
		fieldsCSV string
	)
	cmd := &cobra.Command{
		Use:   "search <natural language>",
		Short: "Generate a UDM query from natural language (Gemini) and run it",
		Long: "Describe what you want in plain English; Gemini translates it to a UDM query\n" +
			"and runs it. If the text names a window (\"…in the last hour\"), Gemini's suggested\n" +
			"range is used unless you set --hours/--from explicitly.",
		Example: "  secopsctl gemini search 'network connections to a public IP in the last hour'",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			res, err := c.TranslateNLToUDMWithTimeRange(ctx, strings.Join(args, " "))
			if err != nil {
				return err
			}
			// Window: explicit --hours wins; else Gemini's suggested range; else last --hours.
			start, end := timeWindow(nlHours)
			if !cmd.Flags().Changed("hours") && res.TimeRange != nil &&
				!res.TimeRange.StartTime.IsZero() && !res.TimeRange.EndTime.IsZero() {
				start, end = res.TimeRange.StartTime, res.TimeRange.EndTime
			}
			events, err := c.SearchUDM(ctx, res.Query, start, end, nlLimit)
			if err != nil {
				return err
			}
			o.fields = splitFields(fieldsCSV)
			return renderEvents(events, o)
		},
	}
	f := cmd.Flags()
	f.IntVar(&nlHours, "hours", 24, "look-back window in hours (ignored when the model infers a window)")
	f.IntVar(&nlLimit, "limit", 1000, "maximum number of events to return")
	f.StringVar(&o.format, "format", "", "output format: table|json|jsonl|csv (default: table on a terminal, jsonl when piped)")
	f.StringVar(&fieldsCSV, "fields", "", "comma-separated UDM field paths to project")
	f.StringVar(&o.out, "out", "", "write results to a file instead of stdout")
	return markJSON(cmd)
}

// newGeminiAskCmd asks the SecOps Gemini assistant a question — YARA-L authoring
// help, UDM field questions, environment-grounded answers. The account must be
// opted in once (--opt-in does it in-place).
func newGeminiAskCmd() *cobra.Command {
	var optIn bool
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask the SecOps Gemini assistant a question (read-only; --opt-in once per account)",
		Long: "Ask SecOps Gemini a question — YARA-L authoring help, UDM field questions,\n" +
			"and environment-grounded answers. Read-only: it returns an answer, it makes\n" +
			"no changes. The account must be opted in to Gemini once; --opt-in performs\n" +
			"that one-time enablement and can be combined with a question in the same run.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			if optIn {
				if err := c.OptInToGemini(ctx); err != nil {
					return err
				}
			}
			resp, err := c.QueryGemini(ctx, args[0], "")
			if err != nil {
				if errors.Is(err, chronicle.ErrGeminiOptInRequired) {
					return fmt.Errorf("this account has not opted in to Gemini — re-run with --opt-in once: %w", err)
				}
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, resp.Raw)
			}
			text := resp.TextContent()
			if text == "" {
				// Live replies often carry HTML blocks instead of TEXT — render them as prose.
				var parts []string
				for _, b := range resp.HTMLBlocks() {
					parts = append(parts, htmlToText(b.Content))
				}
				text = strings.Join(parts, "\n\n")
			}
			if text != "" {
				fmt.Fprintln(os.Stdout, text)
			}
			for _, b := range resp.CodeBlocks() {
				fmt.Fprintf(os.Stdout, "\n```\n%s\n```\n", b.Content)
			}
			if len(resp.References) > 0 {
				fmt.Fprintf(os.Stdout, "\n(%d reference block(s) — full structure with --json.)\n", len(resp.References))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&optIn, "opt-in", false, "opt this account in to Gemini first (one-time)")
	return markJSON(cmd)
}

// htmlToText renders Gemini's HTML prose as readable terminal text: list items
// become bullets, block elements become line breaks, the remaining tags drop, and
// common entities decode.
func htmlToText(s string) string {
	s = regexp.MustCompile(`(?i)<li[^>]*>`).ReplaceAllString(s, "\n  - ")
	s = regexp.MustCompile(`(?i)</(p|ul|ol|div|h[1-6])>|<br[^>]*>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n"))
}
