package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// newRulesTestCmd dry-runs a YARA-L rule file against historical data WITHOUT
// creating the rule — preview the detections it would produce before deploying.
// Read-only (nothing is stored). Goes beyond `rules validate`, which only
// compile-checks: this previews real matches over a window.
//
// By default it STREAMS the test, showing scan progress and detections as they
// arrive (time-to-first-result, not a single print at the end); --no-stream uses
// the buffered path.
func newRulesTestCmd() *cobra.Command {
	var (
		hours, maxResults int
		fromTS, toTS      string
		noStream          bool
	)
	cmd := &cobra.Command{
		Use:   "test <file.yaral>",
		Short: "Read-only: dry-run a YARA-L rule against historical data (streams detections, no deploy)",
		Long: "Run a YARA-L rule file over a window WITHOUT creating the rule, and report\n" +
			"the detections it would have produced (and any compile errors). Unlike\n" +
			"`rules validate` (compile-check only), this previews real matches — size a\n" +
			"rule's coverage and false-positive load before `rules promote`. Streams scan\n" +
			"progress and detections as they arrive; --no-stream buffers instead. The\n" +
			"window is the last --hours, or an explicit --from/--to. Read-only: nothing is\n" +
			"stored.",
		Example: "  secopsctl rules test detections/new-rule.yaral --hours 24\n" +
			"  secopsctl rules test detections/new-rule.yaral --from 2026-06-01T00:00:00Z --to 2026-06-02T00:00:00Z --json",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if fromTS == "" {
				if err := checkHours(hours); err != nil {
					return err
				}
			}
			start, end, err := resolveWindow(hours, fromTS, toTS)
			if err != nil {
				return err
			}
			ruleText, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}

			if noStream {
				res, err := c.RunTestRule(baseContext(), string(ruleText), start, end, maxResults)
				if err != nil {
					return err
				}
				return reportRuleTest(args[0], res.Detections, res.CompilationErrors, res.RuntimeErrors)
			}

			var (
				detections  []json.RawMessage
				compileErrs []json.RawMessage
				runtimeErrs []json.RawMessage
				truncated   bool
				lastPct     = -1
			)
			err = c.StreamTestRule(baseContext(), string(ruleText), start, end, maxResults, func(ch chronicle.RuleTestChunk) error {
				if ch.TooManyDetections {
					truncated = true
				}
				switch {
				case len(ch.Detection) > 0:
					detections = append(detections, ch.Detection)
					if !jsonOut {
						fmt.Fprintf(os.Stderr, "  detection %d\n", len(detections))
					}
				case len(ch.CompilationError) > 0:
					compileErrs = append(compileErrs, ch.CompilationError)
				case len(ch.RuntimeError) > 0:
					runtimeErrs = append(runtimeErrs, ch.RuntimeError)
				case ch.ProgressPercent >= 0 && !jsonOut:
					// Report progress at ~20% steps (and 100%) to avoid spam.
					if ch.ProgressPercent >= lastPct+20 || ch.ProgressPercent == 100 {
						fmt.Fprintf(os.Stderr, "scanning… %d%%\n", ch.ProgressPercent)
						lastPct = ch.ProgressPercent
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			if truncated && !jsonOut {
				fmt.Fprintf(os.Stderr, "warning: hit --max-results %d — detections truncated; raise --max-results for the full set.\n", maxResults)
			}
			return reportRuleTest(args[0], detections, compileErrs, runtimeErrs)
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 24, "look-back window in hours when --from is not given")
	f.StringVar(&fromTS, "from", "", "explicit start time (RFC3339 / ISO-8601); overrides --hours")
	f.StringVar(&toTS, "to", "", "explicit end time (RFC3339 / ISO-8601); default: now")
	f.IntVar(&maxResults, "max-results", 100, "max detections to return (1-10000)")
	f.BoolVar(&noStream, "no-stream", false, "buffer the whole result instead of streaming it")
	return markJSON(cmd)
}

// reportRuleTest emits the shared rule-test result: a compile or runtime error
// fails the run; otherwise the detection count (or the full JSON). The JSON shape
// is identical for the buffered and streaming paths.
func reportRuleTest(file string, detections, compileErrs, runtimeErrs []json.RawMessage) error {
	if jsonOut {
		if err := emitJSON(map[string]any{
			"detection_count":    len(detections),
			"detections":         detections,
			"compilation_errors": compileErrs,
			"runtime_errors":     runtimeErrs,
		}); err != nil {
			return err
		}
		// Still signal failure via a non-zero exit (matches `rules validate --json`),
		// after emitting the structured result a consumer can inspect.
		if len(compileErrs) > 0 {
			return fmt.Errorf("rule did not compile")
		}
		if len(runtimeErrs) > 0 {
			return fmt.Errorf("rule hit a runtime error during the test")
		}
		return nil
	}
	if len(compileErrs) > 0 {
		fmt.Fprintf(os.Stderr, "%d compilation error(s):\n", len(compileErrs))
		for _, e := range compileErrs {
			fmt.Fprintf(os.Stderr, "  %s\n", string(e))
		}
		return fmt.Errorf("rule did not compile — fix the YARA-L and re-run")
	}
	if len(runtimeErrs) > 0 {
		fmt.Fprintf(os.Stderr, "%d runtime error(s):\n", len(runtimeErrs))
		for _, e := range runtimeErrs {
			fmt.Fprintf(os.Stderr, "  %s\n", string(e))
		}
		return fmt.Errorf("rule hit a runtime error during the test")
	}
	fmt.Printf("%s: %d detection(s) (use --json for the full detections).\n", file, len(detections))
	return nil
}
