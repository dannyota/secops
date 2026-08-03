// Package cli builds the secopsctl cobra command tree. Each subcommand lives in
// its own file and self-registers via init(), so adding a command never edits
// this file.
package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/config"
	"danny.vn/secops/soar"
)

// defaultRequestTimeout bounds each individual API request (the whole HTTP
// exchange) so a slow or blocked endpoint fails fast with an actionable error
// instead of hanging. It is PER-REQUEST, not per-command: a command making many
// calls (e.g. `pull all`, paginated reads) is not capped in aggregate, and the
// timer never spans an interactive confirm prompt. Override with --timeout
// (0 disables). Generous enough not to cut a normal single request.
const defaultRequestTimeout = 60 * time.Second

// bulkRequestTimeout is the per-request timeout for known long-running bulk
// search fetches (`search udm --all` / `--raw` / `--count-only`): the search
// runs server-side and streams the complete result in ONE request, so the
// 60-second default would cut large result sets mid-download. Applied only when
// the operator did not set --timeout explicitly; large but finite so a genuinely
// hung call still fails.
const bulkRequestTimeout = 10 * time.Minute

// Global persistent-flag values, shared across subcommands.
var (
	cfgFile        string        // --config
	jsonOut        bool          // --json
	outputFormat   string        // --output: table | json | csv ("" = per-command default)
	forceLegacy    bool          // --legacy: force the legacy AppKey path, skip modern v1alpha
	nonInteractive bool          // --non-interactive: never prompt (no TTY confirmation)
	requestTimeout time.Duration // --timeout: per-request HTTP timeout (0 = none)
)

var rootCmd = &cobra.Command{
	Use:   "secopsctl",
	Short: "Operate Google SecOps (Chronicle SIEM + Siemplify SOAR) as code",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		return normalizeOutputFlags()
	},
	Long: "secopsctl operates a Google SecOps instance — Chronicle SIEM and\n" +
		"Siemplify SOAR — as code, for any tenant.\n\n" +
		"Config as code: pull live state -> review in `git diff` -> push back\n" +
		"(rules, feeds, parsers, dashboards, playbooks, webhooks, ...).\n" +
		"Live operations: UDM query, alerts, cases, per-alert triage verbs,\n" +
		"AI investigations and case summaries, playbook authoring and runs.\n\n" +
		"Every mutation is guarded: dry-run by default, --yes to apply, and a\n" +
		"hard read-only mode for automation (--read-only / SECOPS_READONLY=1).\n\n" +
		"Getting started: first run `secopsctl config` then `secopsctl doctor`.\n" +
		"Discover every command with `secopsctl commands`; every API surface with\n" +
		"`secopsctl surfaces`; register as an MCP server with `secopsctl mcp install`.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Command groups for the root help screen, so `secopsctl --help` renders titled
// sections instead of one flat list. Every top-level command is assigned a
// GroupID in its registration (or here for cobra's built-in help/completion).
const (
	groupSetup     = "setup"
	groupRead      = "read"
	groupAsCode    = "ascode"
	groupSOAR      = "soar"
	groupUtilities = "util"
)

// Execute runs the CLI and returns a process exit code: 0 success, 2 divergence
// (drift / would-change), 1 any other error.
func Execute() int {
	// Cobra adds `help` and `completion` lazily during Execute; materialize them
	// first so the guard below covers them too (else `completion bogus` exits 0).
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
	assignCommandGroups(rootCmd)
	requireSubcommand(rootCmd)
	err := rootCmd.Execute()
	if err != nil {
		// A hit per-request timeout is far more actionable with the knob that
		// controls it. http.Client.Timeout surfaces as a deadline/timeout error.
		// (The bulk search paths may run on bulkRequestTimeout rather than
		// --timeout, so the hint names the knob, not a value.)
		if requestTimeout > 0 && (errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err)) {
			err = fmt.Errorf("%w (a request exceeded its per-request deadline; raise --timeout or set it to 0 to disable)", err)
		}
		// A 429 that survived the transport's hint-honoring retries means the quota
		// is genuinely exhausted — point at the actionable knobs rather than the raw
		// RESOURCE_EXHAUSTED body.
		if rateLimited(err) {
			err = fmt.Errorf("%w\nhint: the API quota is exhausted (HTTP 429) and the request kept being rate-limited after automatic retries. "+
				"Wait ~a minute and retry; for bulk/multi-call operations reduce the call volume (e.g. lower --concurrency)", err)
		}
		// Under --json a failure is emitted as a structured envelope on stdout so
		// an agent/script branches on {code,message,retryable,request_id} instead
		// of regexing the stderr prose. The exit code is unchanged.
		if jsonOut && renderErrorJSON(err) {
			var ec *exitCoder
			if errors.As(err, &ec) {
				return ec.ExitCode()
			}
			return 1
		}
		var ec *exitCoder
		if errors.As(err, &ec) {
			fmt.Fprintf(os.Stderr, "secopsctl: %v\n", err)
			return ec.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "secopsctl: error: %v\n", err)
		return 1
	}
	return 0
}

// rateLimited reports whether err is (or wraps) a 429 from either plane — the
// chronicle (APIError) or SOAR (soar.Error = transport.Error) transport.
func rateLimited(err error) bool {
	var ae *chronicle.APIError
	if errors.As(err, &ae) && ae.Status == http.StatusTooManyRequests {
		return true
	}
	var se *soar.Error
	return errors.As(err, &se) && se.Status == http.StatusTooManyRequests
}

// helpOnlyParents marks the group parents whose RunE was injected by
// requireSubcommand (help-only, not real work) — so the `commands` catalog can
// tell a genuinely runnable parent (e.g. `info`) from a navigation group.
var helpOnlyParents = map[*cobra.Command]bool{}

// requireSubcommand makes every parent command (one that has subcommands and no
// run of its own) reject an unknown or extra argument, so a typo'd subcommand
// (`soar bogus`) exits non-zero instead of silently printing help with status 0.
// A bare parent (`soar`) still prints its help and exits 0.
func requireSubcommand(cmd *cobra.Command) {
	for _, c := range cmd.Commands() {
		requireSubcommand(c)
	}
	if cmd.HasSubCommands() && cmd.Run == nil && cmd.RunE == nil {
		// cobra short-circuits a non-runnable parent to "print help, exit 0" BEFORE
		// validating args, so a typo'd subcommand passes. Giving it a RunE makes it
		// runnable so arg validation runs; the validator then reports
		// `unknown command "x"` — with a "Did you mean this?" suggestion.
		// (Set the validator even when Args is already set — e.g. cobra's own
		// `completion` group — since the miss is the non-runnable short-circuit.)
		if cmd.Args == nil {
			cmd.Args = rejectUnknownSubcommand
		}
		cmd.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }
		helpOnlyParents[cmd] = true
	}
}

// commandGroupByName maps each top-level command to a help group. Commands
// self-register from their own files, so the GroupID is assigned centrally here
// (after registration) rather than threaded through every newXxxCmd. A command
// missing from this map falls into Utilities, so cobra never warns about an
// ungrouped command.
var commandGroupByName = map[string]string{
	// Setup & health.
	"config": groupSetup, "doctor": groupSetup, "info": groupSetup, "version": groupSetup,
	"status": groupSetup,
	// Read & query.
	"search": groupRead, "gemini": groupRead, "rules": groupRead, "curated": groupRead,
	"exclusions": groupRead, "mitre": groupRead, "ti": groupRead, "alerts": groupRead,
	"cases": groupRead, "lists": groupRead, "entities": groupRead,
	"data-access": groupRead, "commands": groupRead, "audit": groupRead,
	// Config as code.
	"pull": groupAsCode, "push": groupAsCode, "drift": groupAsCode,
	"dashboards": groupAsCode, "ingest": groupAsCode,
	// SOAR.
	"soar": groupSOAR, "content-hub": groupSOAR,
	// Utilities.
	"cleanup": groupUtilities, "mcp": groupUtilities,
}

// assignCommandGroups stamps a GroupID onto every top-level command so the root
// help renders titled sections. cobra's built-in help/completion and anything
// not explicitly mapped fall into Utilities (cobra warns if a command has no
// group while groups exist).
func assignCommandGroups(root *cobra.Command) {
	for _, c := range root.Commands() {
		if g, ok := commandGroupByName[c.Name()]; ok {
			c.GroupID = g
		} else {
			c.GroupID = groupUtilities
		}
	}
}

// rejectUnknownSubcommand is the Args validator for help-only parent commands:
// any extra positional argument is a mistyped subcommand, so it errors with
// `unknown command "x"` — and, unlike cobra.NoArgs, appends the Levenshtein
// "Did you mean this?" suggestion (cobra omits suggestions from NoArgs, only
// the default legacyArgs path produces them). Mirrors cobra's own message.
func rejectUnknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	var msg strings.Builder
	fmt.Fprintf(&msg, "unknown command %q for %q", args[0], cmd.CommandPath())
	// cobra's own findSuggestions defaults the minimum distance to 2 before
	// matching; replicate that so a one-edit typo (pll -> pull) is suggested.
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		msg.WriteString("\n\nDid you mean this?\n")
		for _, s := range suggestions {
			msg.WriteString("\t" + s + "\n")
		}
	}
	return fmt.Errorf("%s", msg.String())
}

func init() {
	rootCmd.AddGroup(
		&cobra.Group{ID: groupSetup, Title: "Setup & health:"},
		&cobra.Group{ID: groupRead, Title: "Read & query:"},
		&cobra.Group{ID: groupAsCode, Title: "Config as code (pull / review / push):"},
		&cobra.Group{ID: groupSOAR, Title: "SOAR:"},
		&cobra.Group{ID: groupUtilities, Title: "Utilities:"},
	)
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&cfgFile, "config", "",
		"path to the instance config YAML (overrides $SECOPSCTL_CONFIG and discovery)")
	pf.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON where supported (alias for --output json)")
	pf.StringVar(&outputFormat, "output", "",
		"output format where a command renders tabular data: table | json | csv "+
			"(json works wherever --json does; a command-local --format overrides this)")
	pf.BoolVar(&forceLegacy, "legacy", false,
		"force the legacy AppKey path on dual-generation surfaces (currently the 'soar case list' "+
			"surface); ignored where a command has no modern/legacy split")
	pf.BoolVar(&nonInteractive, "non-interactive", false,
		"never prompt; a guarded mutation without --yes is refused rather than asking")
	pf.BoolVar(&readOnlyFlag, "read-only", false,
		"hard read-only session: every guarded mutation degrades to a dry-run preview even with --yes "+
			"(also enabled by SECOPS_READONLY=1 — set it in the environment that launches an agent)")
	pf.BoolVar(&noProgress, "no-progress", false,
		"disable progress spinners and counters on stderr (implied by --json and non-TTY stderr)")
	pf.DurationVar(&requestTimeout, "timeout", defaultRequestTimeout,
		"per-request HTTP timeout for API calls; a slow/blocked endpoint fails fast instead of hanging. "+
			"Per request for normal commands — it never spans a confirm prompt or caps a multi-call command in "+
			"aggregate; doctor and status capabilities use it as their overall health deadline "+
			"(0 disables; raise for a single very large request, e.g. --timeout 5m)")
	rootCmd.MarkFlagsMutuallyExclusive("json", "output")
}

// normalizeOutputFlags folds the global --output choice into the older --json
// switch (--output json behaves exactly like --json everywhere) and rejects an
// unknown format before any RunE runs.
func normalizeOutputFlags() error {
	switch outputFormat {
	case "", "table", "json", "csv":
	default:
		return fmt.Errorf("--output must be table, json, or csv (got %q)", outputFormat)
	}
	if outputFormat == "json" {
		jsonOut = true
	}
	return nil
}

// loadInstance resolves and loads the instance config honoring --config.
func loadInstance() (*config.Instance, error) {
	return config.Load(cfgFile)
}

// newChronicleClient builds a Chronicle SIEM client from the resolved config.
// Credentials are OAuth/ADC, minted in-process by the Google auth library and
// resolved lazily on the first request.
func newChronicleClient() (*chronicle.Client, error) {
	return newChronicleClientTimeout(requestTimeout)
}

// newChronicleClientTimeout is newChronicleClient with an explicit per-request
// timeout — for bulk single-request fetches whose default deadline differs from
// --timeout (see effectiveSearchTimeout).
func newChronicleClientTimeout(timeout time.Duration) (*chronicle.Client, error) {
	inst, err := loadInstance()
	if err != nil {
		return nil, err
	}
	creds := auth.OAuth(auth.WithForceIPv4(inst.ForceIPv4))
	return chronicle.NewClient(inst.Settings(), creds,
		chronicle.WithHTTPClient(timeoutHTTPClient(creds, inst.ForceIPv4, timeout)))
}

// effectiveSearchTimeout picks the per-request deadline for a bulk search fetch:
// an explicit --timeout always wins; otherwise the bulk default replaces the
// 60-second general default (one streamed request must carry the whole result).
func effectiveSearchTimeout() time.Duration {
	if rootCmd.PersistentFlags().Changed("timeout") {
		return requestTimeout
	}
	return bulkRequestTimeout
}

// timedHTTPClient builds the outbound *http.Client the CLI hands to every SDK
// client, applying --timeout as a PER-REQUEST deadline (http.Client.Timeout bounds
// one whole request/response exchange). 0 leaves it unbounded. Mirrors the SDK's
// own default transport wiring (auth round-tripper + shared transport), so the only
// difference from the SDK default is the configurable timeout. Per-request scope is
// deliberate: it fails a hung call fast without spanning a confirm prompt or
// capping a multi-call command (pull all, paginated reads) in aggregate.
func timedHTTPClient(creds auth.Credentials, forceIPv4 bool) *http.Client {
	return timeoutHTTPClient(creds, forceIPv4, requestTimeout)
}

// timeoutHTTPClient is timedHTTPClient with an explicit timeout.
func timeoutHTTPClient(creds auth.Credentials, forceIPv4 bool, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: auth.RoundTripper(creds, auth.HTTPTransport(forceIPv4)),
	}
}

// baseContext is the root context for API calls. Cancellation/timeout is handled
// per-request by timedHTTPClient (http.Client.Timeout), not by a context deadline,
// so a multi-call command is never capped in aggregate and a confirm prompt is
// never on the clock.
func baseContext() context.Context { return context.Background() }
