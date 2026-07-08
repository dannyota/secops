package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/soar"
)

// TestUDMSummarySnakeCase locks in the snake_case fallback: the legacy tool
// honored event_timestamp/event_type as well as the camelCase keys.
func TestUDMSummarySnakeCase(t *testing.T) {
	cases := []struct {
		name        string
		event       string
		when, etype string
	}{
		{
			"camel nested", `{"udm":{"metadata":{"eventTimestamp":"2026-01-01T00:00:00Z","eventType":"USER_LOGIN"}}}`,
			"2026-01-01T00:00:00Z", "USER_LOGIN",
		},
		{
			"snake nested", `{"udm":{"metadata":{"event_timestamp":"2026-02-02T00:00:00Z","event_type":"NETWORK_DNS"}}}`,
			"2026-02-02T00:00:00Z", "NETWORK_DNS",
		},
		{
			"snake top-level", `{"metadata":{"event_timestamp":"2026-03-03T00:00:00Z","event_type":"FILE_OPEN"}}`,
			"2026-03-03T00:00:00Z", "FILE_OPEN",
		},
		{"missing", `{"udm":{}}`, "?", "?"},
	}
	for _, tc := range cases {
		when, etype := udmSummary(json.RawMessage(tc.event))
		if when != tc.when || etype != tc.etype {
			t.Errorf("%s: udmSummary = (%q,%q), want (%q,%q)", tc.name, when, etype, tc.when, tc.etype)
		}
	}
}

// TestCommandsRegistered verifies each subcommand self-registered via its init()
// so the CLI tree is wired without touching the network or credentials.
func TestCommandsRegistered(t *testing.T) {
	want := []string{"info", "pull", "push", "search", "soar", "doctor"}
	have := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("command %q not registered on root", w)
		}
	}
}

// TestQueryHasUDMSubcommand verifies the nested query udm command exists.
func TestQueryHasUDMSubcommand(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		if c.Name() != "search" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() == "udm" {
				return
			}
		}
		t.Fatal("query command has no udm subcommand")
	}
	t.Fatal("query command not found")
}

// runCmd executes a freshly-built command with args, capturing both the cobra
// out/err streams AND os.Stdout (the guarded-mutation banners write directly to
// os.Stdout) so the returned string is the full combined output and the test
// log stays clean. SilenceUsage/Errors keep cobra from re-printing the error.
func runCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	// Redirect os.Stdout for the duration of Execute, then merge it into buf.
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	err := cmd.Execute()

	_ = w.Close()
	os.Stdout = orig
	buf.WriteString(<-done)
	return buf.String(), err
}

// TestDivergenceExitCode locks the git-style code: a divergence is exit 2.
func TestDivergenceExitCode(t *testing.T) {
	var ec *exitCoder
	if !errors.As(divergence("x %d", 1), &ec) {
		t.Fatal("divergence() is not an *exitCoder")
	}
	if ec.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", ec.ExitCode())
	}
}

func newTestParent() *cobra.Command {
	p := &cobra.Command{Use: "parent", SilenceUsage: true, SilenceErrors: true}
	p.AddCommand(&cobra.Command{Use: "child", RunE: func(*cobra.Command, []string) error { return nil }})
	requireSubcommand(p)
	p.SetOut(io.Discard)
	p.SetErr(io.Discard)
	return p
}

// TestRequireSubcommandRejectsUnknown: a typo'd subcommand exits non-zero.
func TestRequireSubcommandRejectsUnknown(t *testing.T) {
	p := newTestParent()
	p.SetArgs([]string{"bogus"})
	if err := p.Execute(); err == nil {
		t.Error("unknown subcommand should return an error (non-zero exit)")
	}
}

// TestRequireSubcommandBareParentOK: a bare parent prints help, no error.
func TestRequireSubcommandBareParentOK(t *testing.T) {
	p := newTestParent()
	p.SetArgs(nil)
	if err := p.Execute(); err != nil {
		t.Errorf("bare parent should print help without error, got %v", err)
	}
}

// TestRequireSubcommandKnownChildOK: a real subcommand still runs.
func TestRequireSubcommandKnownChildOK(t *testing.T) {
	p := newTestParent()
	p.SetArgs([]string{"child"})
	if err := p.Execute(); err != nil {
		t.Errorf("known subcommand should run, got %v", err)
	}
}

// TestEnsureDataDir: a live push into a missing data dir is refused; dry-run is
// allowed but returns a warning when the dir is missing.
func TestEnsureDataDir(t *testing.T) {
	dir := t.TempDir()
	if warn, err := ensureDataDir("t", dir, false); err != nil {
		t.Errorf("existing dir, live: unexpected error %v", err)
	} else if warn != "" {
		t.Errorf("existing dir, live: unexpected warning %q", warn)
	}
	missing := filepath.Join(dir, "nope")
	if _, err := ensureDataDir("t", missing, false); err == nil {
		t.Error("missing dir, live push: expected an error")
	}
	if warn, err := ensureDataDir("t", missing, true); err != nil {
		t.Errorf("missing dir, dry-run: should be allowed, got %v", err)
	} else if warn == "" {
		t.Error("missing dir, dry-run: expected a warning")
	}
	if warn, err := ensureDataDir("t", dir, true); err != nil {
		t.Errorf("existing dir, dry-run: unexpected error %v", err)
	} else if warn != "" {
		t.Errorf("existing dir, dry-run: unexpected warning %q", warn)
	}
}

// TestConfirmPushNonInteractive: --non-interactive never auto-confirms.
func TestConfirmPushNonInteractive(t *testing.T) {
	old := nonInteractive
	defer func() { nonInteractive = old }()
	nonInteractive = true
	if confirmPush("t") {
		t.Error("--non-interactive must not auto-confirm a guarded mutation")
	}
}

// TestConfirmPushJSON: --json must not prompt (the y/N would corrupt stdout JSON).
func TestConfirmPushJSON(t *testing.T) {
	old := jsonOut
	defer func() { jsonOut = old }()
	jsonOut = true
	if confirmPush("t") {
		t.Error("--json must not prompt for confirmation")
	}
}

// TestTimedHTTPClientAppliesTimeout verifies --timeout is wired as a PER-REQUEST
// http.Client.Timeout (the correct altitude) rather than a context deadline that
// would span a confirm prompt or cap a multi-call command in aggregate.
func TestTimedHTTPClientAppliesTimeout(t *testing.T) {
	saved := requestTimeout
	defer func() { requestTimeout = saved }()

	creds := auth.SOARAppKey("k")

	requestTimeout = 45 * time.Second
	if got := timedHTTPClient(creds, false).Timeout; got != 45*time.Second {
		t.Errorf("client.Timeout = %v, want 45s", got)
	}

	// 0 disables the per-request bound (http.Client.Timeout == 0 means no limit).
	requestTimeout = 0
	if got := timedHTTPClient(creds, false).Timeout; got != 0 {
		t.Errorf("--timeout 0 should leave client.Timeout unbounded, got %v", got)
	}
}

// TestBaseContextHasNoDeadline guards the altitude fix: baseContext must NOT carry
// a deadline (timeouts live on the HTTP client), so a confirm prompt or a long
// multi-call command is never on a context clock.
func TestBaseContextHasNoDeadline(t *testing.T) {
	saved := requestTimeout
	defer func() { requestTimeout = saved }()
	requestTimeout = 30 * time.Second
	if _, ok := baseContext().Deadline(); ok {
		t.Error("baseContext must not carry a deadline; per-request timeout belongs on the HTTP client")
	}
}

// TestDefaultRequestTimeoutReasonable guards the default: present (fail-fast) but
// generous enough not to cut a normal single request.
func TestDefaultRequestTimeoutReasonable(t *testing.T) {
	if defaultRequestTimeout < 30*time.Second || defaultRequestTimeout > 5*time.Minute {
		t.Errorf("defaultRequestTimeout = %v, want a generous-but-bounded fail-fast default", defaultRequestTimeout)
	}
}

// renamePairs is the v0.5.1 command-clarity rename map: each old name is kept as
// a hidden back-compat alias of the new canonical command. Keep this in lock-step
// with the Use/Aliases declarations and docs/design/cli-naming.md.
var renamePairs = []struct{ canonical, alias string }{}

// TestRenamedCommandsCanonicalAndAlias verifies every renamed top-level command is
// registered under its canonical name and that the old name resolves to the SAME
// command as a cobra alias — so existing invocations keep working unchanged.
func TestRenamedCommandsCanonicalAndAlias(t *testing.T) {
	for _, p := range renamePairs {
		canon, _, err := rootCmd.Find([]string{p.canonical})
		if err != nil || canon == nil || canon.Name() != p.canonical {
			t.Errorf("canonical command %q not registered (got %v, err %v)", p.canonical, canon, err)
			continue
		}
		alias, _, err := rootCmd.Find([]string{p.alias})
		if err != nil || alias == nil {
			t.Errorf("alias %q does not resolve (err %v)", p.alias, err)
			continue
		}
		if alias != canon {
			t.Errorf("alias %q resolves to %q, want canonical %q", p.alias, alias.Name(), p.canonical)
		}
		if !canon.HasAlias(p.alias) {
			t.Errorf("canonical %q is missing alias %q in its Aliases list", p.canonical, p.alias)
		}
	}
}

// TestRenamedGroupsNotCatalogRows verifies the renamed navigation-only groups stay
// OUT of the `commands` catalog (it lists only runnable verbs): neither the
// canonical group name nor its alias is a row. The old→new mapping is exposed via
// `capabilities --json` instead (TestCommandAliasesInCapabilities).
func TestRenamedGroupsNotCatalogRows(t *testing.T) {
	byPath := map[string]commandRow{}
	for _, r := range collectCommands(rootCmd) {
		byPath[r.Path] = r
	}
	for _, p := range renamePairs {
		if _, ok := byPath[p.canonical]; ok {
			t.Errorf("renamed group %q is a navigation parent and must not be a catalog row", p.canonical)
		}
		if _, ok := byPath[p.alias]; ok {
			t.Errorf("alias %q must not be a catalog row", p.alias)
		}
	}
}

// TestCommandAliasesInCapabilities verifies each rename's old→new mapping is
// discoverable in the capabilities alias map.
func TestCommandAliasesInCapabilities(t *testing.T) {
	aliases := collectCommandAliases(rootCmd)
	for _, p := range renamePairs {
		got, ok := aliases[p.alias]
		if !ok {
			t.Errorf("capabilities alias map missing %q", p.alias)
			continue
		}
		if got != p.canonical {
			t.Errorf("alias %q maps to %q, want %q", p.alias, got, p.canonical)
		}
	}
}

// TestCommandsCatalogJSONColumn asserts the per-command --json support reported
// by `secopsctl commands` (the JSON field) is accurate for a representative
// sample: commands whose output honors --json are marked, and ones that never
// emit JSON (pull writes files; config is interactive) are not.
func TestCommandsCatalogJSONColumn(t *testing.T) {
	byPath := map[string]commandRow{}
	for _, r := range collectCommands(rootCmd) {
		byPath[r.Path] = r
	}

	wantJSON := []string{
		"alerts list",          // emits a JSON snapshot under --json
		"cases counts",         // structured counts under --json
		"commands",             // the catalog itself
		"info",                 // resolved config as JSON
		"doctor",               // {ok, version, checks[]}
		"drift",                // per-surface drift report
		"cases close",          // guarded verb: dry-run/apply metadata under --json
		"rules alerts",         // always raw JSON regardless of the flag
		"search udm",           // raw event array under --json
		"cases get",            // raw case object under --json
		"lists watchlists get", // always JSON
		"ingest parsers run",   // parsed UDM is always JSON
	}
	for _, path := range wantJSON {
		r, ok := byPath[path]
		if !ok {
			t.Errorf("command %q missing from the catalog", path)
			continue
		}
		if !r.JSON {
			t.Errorf("%q should be marked as honoring --json", path)
		}
	}

	wantNoJSON := []string{
		"pull",   // text-only: its output is the files it writes
		"config", // interactive form; never emits JSON
	}
	for _, path := range wantNoJSON {
		r, ok := byPath[path]
		if !ok {
			t.Errorf("command %q missing from the catalog", path)
			continue
		}
		if r.JSON {
			t.Errorf("%q must not be marked as honoring --json", path)
		}
	}
}

// TestCommandRowJSONFieldRoundTrips confirms the catalog row's new json field is
// present and round-trips in the --json output shape (so an agent reading
// `secopsctl commands --json` sees a stable boolean key).
func TestCommandRowJSONFieldRoundTrips(t *testing.T) {
	row := commandRow{Path: "alerts list", Kind: "read", JSON: true, Short: "x"}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		JSON *bool `json:"json"`
	}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.JSON == nil {
		t.Fatalf("json field absent from marshaled row: %s", b)
	}
	if *back.JSON != true {
		t.Errorf("json field = %v, want true", *back.JSON)
	}

	// A read-only-with-no-JSON row marshals the field as false (not omitted), so
	// the column is unambiguous for every row.
	noJSON := commandRow{Path: "pull", Kind: "read"}
	b2, err := json.Marshal(noJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b2, &back); err != nil {
		t.Fatal(err)
	}
	if back.JSON == nil || *back.JSON != false {
		t.Errorf("json field for a non-JSON row = %v, want false (present)", back.JSON)
	}
}

// TestNoLocalJSONFlag is the Wave 63 drift guard: the global persistent --json
// (on the root) is the single mechanism, so no other command may declare a LOCAL
// --json flag that would shadow it. cobra's LocalFlags() excludes flags inherited
// from a parent's persistent set, so before Execute the persistent --json shows
// up only in the root's LocalFlags(); any non-root command whose LocalFlags()
// carries "json" has re-introduced a local flag and fails this test.
func TestNoLocalJSONFlag(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c != rootCmd && c.LocalFlags().Lookup("json") != nil {
			t.Errorf("%q declares a LOCAL --json flag; use the global persistent --json (jsonOut) instead", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}

// TestErrorEnvelopeClassifies asserts the structured --json error envelope maps
// each SDK error type to the right canonical code, request id, and retryable flag.
func TestErrorEnvelopeClassifies(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantCode  string
		wantRetry bool
		wantRID   string
	}{
		{
			name:     "chronicle 404 GET not retryable",
			err:      &chronicle.APIError{Method: http.MethodGet, Status: 404, RequestID: "abc"},
			wantCode: "NOT_FOUND", wantRetry: false, wantRID: "abc",
		},
		{
			name:     "chronicle 500 GET retryable (idempotent)",
			err:      &chronicle.APIError{Method: http.MethodGet, Status: 500},
			wantCode: "INTERNAL", wantRetry: true,
		},
		{
			name:     "chronicle 500 POST not retryable (mutation)",
			err:      &chronicle.APIError{Method: http.MethodPost, Status: 500},
			wantCode: "INTERNAL", wantRetry: false,
		},
		{
			name:     "soar 429 any method retryable",
			err:      &soar.Error{Method: http.MethodPost, Status: 429, RequestID: "r1"},
			wantCode: "RESOURCE_EXHAUSTED", wantRetry: true, wantRID: "r1",
		},
		{
			name:     "drift sentinel",
			err:      divergence("surface x drifted"),
			wantCode: "DRIFT", wantRetry: false,
		},
		{
			name:     "generic error",
			err:      errors.New("boom"),
			wantCode: "ERROR", wantRetry: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newErrorEnvelope(tc.err)
			if env.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", env.Code, tc.wantCode)
			}
			if env.Retryable != tc.wantRetry {
				t.Errorf("retryable = %v, want %v", env.Retryable, tc.wantRetry)
			}
			if env.RequestID != tc.wantRID {
				t.Errorf("request_id = %q, want %q", env.RequestID, tc.wantRID)
			}
		})
	}
}

// TestEnumFromUsage pins the help-text enum extractor used by `commands --json`.
func TestEnumFromUsage(t *testing.T) {
	cases := []struct {
		usage string
		want  []string
	}{
		{"curated precision (precise|broad)", []string{"precise", "broad"}},
		{"reason: malicious | not-malicious | maintenance", []string{"malicious", "not-malicious", "maintenance"}},
		{"a plain description with no enum", nil},
		{"just one|", nil}, // single token → not an enum
		{"replace a base step with a mold: <step-name|id>=<step.json>", nil}, // placeholder grammar, not an enum
		{"indicator type: md5|sha1|sha256 (default: auto-detect)", []string{"md5", "sha1", "sha256"}},
	}
	for _, tc := range cases {
		if got := enumFromUsage(tc.usage); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("enumFromUsage(%q) = %v, want %v", tc.usage, got, tc.want)
		}
	}
}

// TestCommandsCatalogRichFlags asserts the catalog now carries per-flag type and
// the parsed enum for a representative guarded verb.
func TestCommandsCatalogRichFlags(t *testing.T) {
	byPath := map[string]commandRow{}
	for _, r := range collectCommands(rootCmd) {
		byPath[r.Path] = r
	}
	r, ok := byPath["curated set"]
	if !ok {
		t.Fatal("`curated set` missing from catalog")
	}
	var precision *flagInfo
	for i := range r.Flags {
		if r.Flags[i].Name == "precision" {
			precision = &r.Flags[i]
		}
	}
	if precision == nil {
		t.Fatal("curated set has no --precision flag in catalog")
	}
	if precision.Type != "string" {
		t.Errorf("--precision type = %q, want string", precision.Type)
	}
	if !reflect.DeepEqual(precision.Enum, []string{"precise", "broad"}) {
		t.Errorf("--precision enum = %v, want [precise broad]", precision.Enum)
	}
}

func TestReadOnlyMode(t *testing.T) {
	t.Setenv("SECOPS_READONLY", "")
	if readOnlyMode() {
		t.Fatal("read-only must be off by default")
	}
	// Fail-closed: any value other than an explicit falsy enables the cap —
	// a mis-spelled truthy must never silently leave a session write-capable.
	for _, v := range []string{"1", "true", "YES", "on", "enabled", "y", "anything"} {
		t.Setenv("SECOPS_READONLY", v)
		if !readOnlyMode() {
			t.Errorf("SECOPS_READONLY=%s must enable read-only mode (fail closed)", v)
		}
	}
	for _, v := range []string{"0", "false", "no", "off", "  "} {
		t.Setenv("SECOPS_READONLY", v)
		if readOnlyMode() {
			t.Errorf("SECOPS_READONLY=%q must not enable read-only mode", v)
		}
	}
	readOnlyFlag = true
	t.Cleanup(func() { readOnlyFlag = false })
	if !readOnlyMode() {
		t.Error("--read-only flag must enable read-only mode")
	}
}

func TestSOARGuardReadOnlyDegrades(t *testing.T) {
	t.Setenv("SECOPSCTL_HOME", t.TempDir())
	t.Setenv("SECOPS_READONLY", "1")
	dr, ay := soarGuard("test action", false, true) // --yes passed
	if !dr || ay {
		t.Errorf("read-only soarGuard(--yes) = dryRun %v, assumeYes %v; want true, false", dr, ay)
	}
}

func TestAuditMutationWritesJSONL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SECOPSCTL_HOME", home)
	auditMutation("close case 1 (reason=Maintenance)", "confirmed")
	auditMutation("push rules-deploy", "read-only")

	path := filepath.Join(home, "audit.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("audit log not written: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("audit log mode = %o, want 0600", perm)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 records, got %d:\n%s", len(lines), b)
	}
	var rec struct {
		Time     string `json:"time"`
		Action   string `json:"action"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("record not JSON: %v", err)
	}
	if rec.Action != "close case 1 (reason=Maintenance)" || rec.Decision != "confirmed" || rec.Time == "" {
		t.Errorf("unexpected record: %+v", rec)
	}
}

func TestCommandsCatalog(t *testing.T) {
	rows := collectCommands(rootCmd)
	if len(rows) < 100 {
		t.Fatalf("suspiciously few commands: %d", len(rows))
	}
	byPath := map[string]commandRow{}
	for _, r := range rows {
		if _, dup := byPath[r.Path]; dup {
			t.Errorf("duplicate command path %q", r.Path)
		}
		byPath[r.Path] = r
	}
	// The kind heuristic: the --dry-run/--yes pair marks a guarded live mutation.
	// `info` is a runnable parent (it has subcommands AND real work of its own)
	// and must appear; navigation-only parents must not.
	wantKind := map[string]string{
		"alerts list":   "read",
		"alerts update": "guarded-mutation",
		"cases get":     "read",
		"cases close":   "guarded-mutation",
		"pull":          "read",
		"push":          "guarded-mutation",
		"commands":      "read",
		"info":          "read",
	}
	for path, want := range wantKind {
		r, ok := byPath[path]
		if !ok {
			t.Errorf("command %q missing from the catalog", path)
			continue
		}
		if r.Kind != want {
			t.Errorf("%q kind = %q, want %q", path, r.Kind, want)
		}
		if r.Short == "" {
			t.Errorf("%q has no short description", path)
		}
	}
	// Group parents must not appear (they are navigation, not verbs).
	for _, parent := range []string{"soar", "cases", "rules"} {
		if _, ok := byPath[parent]; ok {
			t.Errorf("group parent %q must not be a catalog row", parent)
		}
	}
}

// TestGuardFlagPairInvariant asserts the convention everything else keys off:
// a command that can apply a live mutation (--yes) always carries the
// --dry-run half of the gate too. A future verb with a bare --yes would be
// misclassified as `read` by the catalog AND uncovered by read-only mode — this
// is the tripwire.
func TestGuardFlagPairInvariant(t *testing.T) {
	for _, r := range collectCommands(rootCmd) {
		hasYes, hasDry := false, false
		for _, f := range r.Flags {
			switch f.Name {
			case "yes":
				hasYes = true
			case "dry-run":
				hasDry = true
			}
		}
		if hasYes != hasDry {
			t.Errorf("%q has yes=%v dry-run=%v — the guard pair must travel together", r.Path, hasYes, hasDry)
		}
	}
}

// TestAlertsUpdateEnumErrorVerbatimAndValidSet: an invalid alerts-update enum
// quotes the OPERATOR'S verbatim input (not the transformed wire token) and
// lists the valid set, matching the `soar case close --reason` gold standard.
func TestAlertsUpdateEnumErrorVerbatimAndValidSet(t *testing.T) {
	cases := []struct {
		flag, val string
		want      []string // substrings the error must contain
	}{
		{"--reason", "totallywrong", []string{`"totallywrong"`, "not-malicious", "malicious", "maintenance"}},
		{"--status", "bogus", []string{`"bogus"`, "new|reviewed|closed|open"}},
		{"--verdict", "nope", []string{`"nope"`, "true-positive|false-positive"}},
		{"--priority", "huge", []string{`"huge"`, "info|low|medium|high|critical"}},
		{"--reputation", "meh", []string{`"meh"`, "useful|not-useful"}},
	}
	for _, tc := range cases {
		out, err := runCmd(t, newAlertsUpdateCmd(), "alert-1", tc.flag, tc.val, "--dry-run")
		if err == nil {
			t.Fatalf("%s %s: expected an error, got nil (out=%q)", tc.flag, tc.val, out)
		}
		msg := err.Error()
		for _, w := range tc.want {
			if !strings.Contains(msg, w) {
				t.Errorf("%s %s: error %q must contain %q", tc.flag, tc.val, msg, w)
			}
		}
		// The transformed wire token must NOT leak into the message.
		if strings.Contains(msg, "REASON_") || strings.Contains(msg, "PRIORITY_") {
			t.Errorf("%s %s: error leaks a wire token: %q", tc.flag, tc.val, msg)
		}
	}
}

// A valid short enum value passes the CLI-side check (it then reaches the
// guard, which dry-runs without a client). "informative" is accepted as a
// priority synonym but is omitted from the hint.
func TestAlertsUpdateEnumAcceptsValid(t *testing.T) {
	// A valid value passes the enum check and reaches the guard, which dry-runs
	// to success (the dry-run banner is printed to os.Stdout, not captured here —
	// success is signalled by a nil error).
	out, err := runCmd(t, newAlertsUpdateCmd(), "alert-1", "--reason", "not-malicious", "--dry-run")
	if err != nil {
		t.Fatalf("valid reason should pass, got %v (out=%q)", err, out)
	}
	if got := alertEnumHint("priority"); strings.Contains(got, "informative") {
		t.Errorf("priority hint must omit the 'informative' synonym, got %q", got)
	}
}

// TestHoursMustBePositive: windowed commands reject --hours <= 0 before any work.
func TestHoursMustBePositive(t *testing.T) {
	for _, h := range []int{0, -1, -24} {
		if err := checkHours(h); err == nil {
			t.Errorf("checkHours(%d) must error", h)
		} else if !strings.Contains(err.Error(), "positive number of hours") {
			t.Errorf("checkHours(%d) message = %q", h, err.Error())
		}
	}
	if err := checkHours(1); err != nil {
		t.Errorf("checkHours(1) must pass, got %v", err)
	}

	// End-to-end on a windowed command: search udm --hours 0 fails fast, before
	// any client is constructed.
	out, err := runCmd(t, newAlertsListCmd(), "--hours", "0")
	if err == nil || !strings.Contains(err.Error(), "positive number of hours") {
		t.Errorf("alerts list --hours 0 must reject; got err=%v out=%q", err, out)
	}
}

// TestCaseIDFlagAliasBothWork: --id is the primary case-id flag on run-action
// and context set, with --case-id a hidden alias; both spellings set the same value.
func TestCaseIDFlagAliasBothWork(t *testing.T) {
	type builder struct {
		name string
		make func() *cobra.Command
		args func(idFlag string) []string
	}
	builders := []builder{
		{
			name: "run-action",
			make: newCaseRunActionCmd,
			args: func(idFlag string) []string {
				return []string{idFlag, "7", "--action", "Ping", "--instance", "uuid-1", "--dry-run"}
			},
		},
		{
			name: "context set",
			make: newCaseContextSetCmd,
			args: func(idFlag string) []string {
				return []string{idFlag, "7", "--key", "k", "--value", "v", "--dry-run"}
			},
		},
	}
	for _, b := range builders {
		for _, idFlag := range []string{"--id", "--case-id"} {
			// A nil error + dry-run preview proves the flag set caseID (>0):
			// otherwise the RunE short-circuits with "--id is required".
			out, err := runCmd(t, b.make(), b.args(idFlag)...)
			if err != nil {
				t.Errorf("%s %s: unexpected error %v (out=%q)", b.name, idFlag, err, out)
			}
			if !strings.Contains(out, "DRY RUN") {
				t.Errorf("%s %s: expected a dry-run preview, got %q", b.name, idFlag, out)
			}
		}
		// --case-id must be hidden, --id visible.
		cmd := b.make()
		if f := cmd.Flags().Lookup("case-id"); f == nil || !f.Hidden {
			t.Errorf("%s: --case-id must exist and be hidden", b.name)
		}
		if f := cmd.Flags().Lookup("id"); f == nil || f.Hidden {
			t.Errorf("%s: --id must exist and be visible", b.name)
		}
	}
}

// TestDeprecatedFlagAliasesHidden: deprecated aliases stay hidden, primaries visible.
func TestDeprecatedFlagAliasesHidden(t *testing.T) {
	// alerts list: --filter primary, --query hidden alias.
	al := newAlertsListCmd()
	if f := al.Flags().Lookup("filter"); f == nil || f.Hidden {
		t.Error("alerts list --filter must exist and be visible")
	}
	if f := al.Flags().Lookup("query"); f == nil || !f.Hidden {
		t.Error("alerts list --query must exist and be hidden")
	}
	// integration uninstall: --key primary, --name hidden alias.
	un := newSOARIntegrationUninstallCmd()
	if f := un.Flags().Lookup("key"); f == nil || f.Hidden {
		t.Error("integration uninstall --key must exist and be visible")
	}
	if f := un.Flags().Lookup("name"); f == nil || !f.Hidden {
		t.Error("integration uninstall --name must exist and be hidden")
	}
}

// TestLegacyFlagRendersWithoutValueToken: the global --legacy flag renders as a
// plain bool — no value token in its usage line.
func TestLegacyFlagRendersWithoutValueToken(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("legacy")
	if f == nil {
		t.Fatal("--legacy persistent flag not registered")
	}
	if f.Value.Type() != "bool" {
		t.Errorf("--legacy must be a bool flag, got type %q", f.Value.Type())
	}
	// cobra derives the value placeholder from the first backtick-quoted word in
	// the usage string; a bool with none renders no value token.
	name, usage := pflag.UnquoteUsage(f)
	if name != "" {
		t.Errorf("--legacy must render with no value placeholder, got %q", name)
	}
	if !strings.Contains(usage, "legacy AppKey path") {
		t.Errorf("--legacy usage lost its description: %q", usage)
	}
}

// TestCobraSuggestionsEnabled: cobra "Did you mean" suggestions are enabled — a
// one-edit typo on a top-level command is suggested, and suggestions are not
// globally disabled.
func TestCobraSuggestionsEnabled(t *testing.T) {
	if rootCmd.DisableSuggestions {
		t.Fatal("rootCmd.DisableSuggestions must be false")
	}
	// rejectUnknownSubcommand is the validator help-only parents get; it must
	// error AND suggest the near-match.
	err := rejectUnknownSubcommand(rootCmd, []string{"pll"})
	if err == nil {
		t.Fatal("a typo'd command must error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown command "pll"`) {
		t.Errorf("missing unknown-command message: %q", msg)
	}
	if !strings.Contains(msg, "Did you mean this?") || !strings.Contains(msg, "pull") {
		t.Errorf("expected a 'Did you mean ... pull' suggestion, got %q", msg)
	}
	// No extra args is fine (a bare parent prints help).
	if err := rejectUnknownSubcommand(rootCmd, nil); err != nil {
		t.Errorf("no-args must not error, got %v", err)
	}
}

// TestCommandGroupsAssigned: every top-level command lands in a help group after
// assignment, so cobra never warns about an ungrouped command.
func TestCommandGroupsAssigned(t *testing.T) {
	assignCommandGroups(rootCmd)
	groups := map[string]bool{}
	for _, g := range rootCmd.Groups() {
		groups[g.ID] = true
	}
	if len(groups) == 0 {
		t.Fatal("no command groups registered on root")
	}
	for _, c := range rootCmd.Commands() {
		if c.GroupID == "" {
			t.Errorf("command %q has no GroupID", c.Name())
			continue
		}
		if !groups[c.GroupID] {
			t.Errorf("command %q has unregistered GroupID %q", c.Name(), c.GroupID)
		}
	}
}

// TestCuratedSetPrecisionFailsFast: curated set validates --precision before
// constructing a client or printing the LIVE banner, so a bad value fails fast.
func TestCuratedSetPrecisionFailsFast(t *testing.T) {
	out, err := runCmd(t, newCuratedSetCmd(),
		"--category", "C", "--ruleset", "R", "--precision", "BOGUS", "--enabled")
	if err == nil || !strings.Contains(err.Error(), "precision") {
		t.Fatalf("bad --precision must fail fast; got err=%v out=%q", err, out)
	}
	// The LIVE banner must NOT have printed before the validation error.
	if strings.Contains(out, "LIVE SIEM change") {
		t.Errorf("precision must be validated BEFORE the guard banner; out=%q", out)
	}
}

func TestSlicesChunk(t *testing.T) {
	var got [][]string
	for chunk := range slicesChunk([]string{"a", "b", "c", "d", "e"}, 2) {
		got = append(got, chunk)
	}
	if len(got) != 3 || len(got[0]) != 2 || len(got[2]) != 1 || got[2][0] != "e" {
		t.Errorf("chunks = %v", got)
	}
}

func TestRefuseAIGenerationIfReadOnly(t *testing.T) {
	t.Setenv("SECOPSCTL_HOME", t.TempDir())
	t.Setenv("SECOPS_READONLY", "")
	if err := refuseAIGenerationIfReadOnly("x"); err != nil {
		t.Errorf("must allow when not read-only: %v", err)
	}
	t.Setenv("SECOPS_READONLY", "1")
	if err := refuseAIGenerationIfReadOnly("x"); err == nil {
		t.Error("must refuse AI generation in read-only mode")
	}
}

func TestHTMLToText(t *testing.T) {
	in := "<p>First line.</p><ul><li>one</li><li>two &amp; three</li></ul>"
	got := htmlToText(in)
	for _, want := range []string{"First line.", "- one", "- two & three"} {
		if !strings.Contains(got, want) {
			t.Errorf("htmlToText missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "<") {
		t.Errorf("tags leaked: %q", got)
	}
}

func TestNewRandomUUID(t *testing.T) {
	pat := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, err := newRandomUUID()
	if err != nil || !pat.MatchString(a) {
		t.Fatalf("uuid %q, %v", a, err)
	}
	b, _ := newRandomUUID()
	if a == b {
		t.Error("two mints must differ")
	}
}
