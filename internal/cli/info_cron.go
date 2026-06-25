package cli

// Types, command, report assembly, emit, and format helpers for info cron.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/auth"
	"danny.vn/secops/config"
)

type cronCommandRow struct {
	Command    string          `json:"command"`
	Kind       string          `json:"kind"`
	Referenced bool            `json:"referenced"`
	References []cronReference `json:"references,omitempty"`
}

type cronReference struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type cronSOARScheduleRow struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	Enabled       *bool  `json:"enabled,omitempty"`
	CronSchedule  string `json:"cron_schedule"`
	LastRunStatus string `json:"last_run_status,omitempty"`
	LastRunTime   string `json:"last_run_time,omitempty"`
	File          string `json:"file"`
	Line          int    `json:"line,omitempty"`
}

type cronHostScanRow struct {
	Source     string `json:"source"`
	Status     string `json:"status"`
	Files      int    `json:"files,omitempty"`
	References int    `json:"references"`
}

type cronHeartbeatRow struct {
	Label      string `json:"label"`
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
}

type cronInfoReport struct {
	Root           string                `json:"root"`
	SchedulerFiles int                   `json:"scheduler_files"`
	Commands       []cronCommandRow      `json:"commands"`
	SOARSchedules  []cronSOARScheduleRow `json:"soar_schedules"`
	HostScans      []cronHostScanRow     `json:"host_scans,omitempty"`
	Heartbeats     []cronHeartbeatRow    `json:"heartbeats,omitempty"`
}

type cronInfoOptions struct {
	Root        string
	IncludeHost bool
	Heartbeats  []cronHeartbeatSpec

	hostSources []cronLineSource
	httpClient  *http.Client
}

type cronLineSource struct {
	Ref   string
	Lines []string
}

type cronHeartbeatSpec struct {
	Label string
	URL   string
}

func newInfoCronCmd() *cobra.Command {
	var root string
	var includeHost bool
	var heartbeatFlags []string
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Report local scheduler references to secopsctl automation (offline)",
		Long: "Scan local scheduler-like files for secopsctl push/drift references.\n" +
			"Also scan pulled SOAR jobs/playbooks for cronSchedule values. With\n" +
			"--host, inspect the current user's crontab and user systemd files.\n" +
			"With --heartbeat-status, check explicit read-only heartbeat status\n" +
			"endpoints. Raw scheduler lines and heartbeat URLs are not echoed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			heartbeats, err := parseCronHeartbeatSpecs(heartbeatFlags)
			if err != nil {
				return err
			}
			report, err := buildCronInfoReportWithOptions(cronInfoOptions{
				Root:        root,
				IncludeHost: includeHost,
				Heartbeats:  heartbeats,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(report)
			}
			emitCronInfo(os.Stdout, report)
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", ".", "repository root to scan")
	cmd.Flags().BoolVar(&includeHost, "host", false, "also inspect current user's crontab and user systemd unit files")
	cmd.Flags().StringArrayVar(&heartbeatFlags, "heartbeat-status", nil, "check a read-only heartbeat status endpoint as <label>=<url> (repeatable; URL is not printed)")
	return markJSON(cmd)
}

func buildCronInfoReport(root string) (cronInfoReport, error) {
	return buildCronInfoReportWithOptions(cronInfoOptions{Root: root})
}

func buildCronInfoReportWithOptions(opts cronInfoOptions) (cronInfoReport, error) {
	root := opts.Root
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return cronInfoReport{}, err
	}
	files, err := schedulerFiles(absRoot)
	if err != nil {
		return cronInfoReport{}, err
	}
	refs, err := scanCronReferences(absRoot, files)
	if err != nil {
		return cronInfoReport{}, err
	}
	hostScans := addHostCronReferences(refs, opts)
	schedules, err := scanSOARSchedules(absRoot)
	if err != nil {
		return cronInfoReport{}, err
	}
	if schedules == nil {
		schedules = []cronSOARScheduleRow{}
	}
	rows := knownCronCommandRows()
	for i := range rows {
		rows[i].References = refs[rows[i].Command]
		rows[i].Referenced = len(rows[i].References) > 0
	}
	heartbeats := checkCronHeartbeats(baseContext(), opts.Heartbeats, opts.httpClient)
	return cronInfoReport{
		Root:           absRoot,
		SchedulerFiles: len(files),
		Commands:       rows,
		SOARSchedules:  schedules,
		HostScans:      hostScans,
		Heartbeats:     heartbeats,
	}, nil
}

func emitCronInfo(w io.Writer, report cronInfoReport) {
	fmt.Fprintf(w, "Scheduler files scanned: %d\n", report.SchedulerFiles)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "COMMAND\tKIND\tREFERENCED\tREFERENCES")
	for _, row := range report.Commands {
		fmt.Fprintf(tw, "%s\t%s\t%t\t%s\n",
			row.Command,
			row.Kind,
			row.Referenced,
			formatCronRefs(row.References),
		)
	}
	_ = tw.Flush()

	if len(report.HostScans) > 0 {
		fmt.Fprintln(w, "\nHost scheduler scans")
		tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "SOURCE\tSTATUS\tFILES\tREFERENCES")
		for _, row := range report.HostScans {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", row.Source, row.Status, row.Files, row.References)
		}
		_ = tw.Flush()
	}

	fmt.Fprintf(w, "\nSOAR schedules found: %d\n", len(report.SOARSchedules))
	if len(report.SOARSchedules) == 0 {
		emitCronHeartbeats(w, report.Heartbeats)
		return
	}
	tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "TYPE\tNAME\tENABLED\tCRON\tLAST_RUN\tREFERENCE")
	for _, row := range report.SOARSchedules {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Type,
			row.Name,
			formatCronEnabled(row.Enabled),
			row.CronSchedule,
			formatCronLastRun(row),
			formatCronFileLine(row.File, row.Line),
		)
	}
	_ = tw.Flush()
	emitCronHeartbeats(w, report.Heartbeats)
}

func emitCronHeartbeats(w io.Writer, rows []cronHeartbeatRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "\nHeartbeat status checks: %d\n", len(rows))
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "LABEL\tOK\tSTATUS\tERROR")
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%t\t%s\t%s\n",
			row.Label,
			row.OK,
			formatCronStatusCode(row.StatusCode),
			formatCronError(row.Error),
		)
	}
	_ = tw.Flush()
}

func knownCronCommandRows() []cronCommandRow {
	var rows []cronCommandRow
	add := func(kind, command string) {
		rows = append(rows, cronCommandRow{Kind: kind, Command: command})
	}
	add("read", "drift")

	if push := commandChild(rootCmd, "push"); push != nil {
		for _, target := range push.ValidArgs {
			add("siem_push", "push "+target)
		}
	}
	if soar := commandChild(rootCmd, "soar"); soar != nil {
		if push := commandChild(soar, "push"); push != nil {
			for _, child := range push.Commands() {
				add("soar_push", "soar push "+child.Name())
			}
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Command < rows[j].Command
	})
	return rows
}

func commandChild(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

func schedulerFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipCronScanDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if isSchedulerFile(rel) {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func skipCronScanDir(rel, name string) bool {
	switch name {
	case ".git", "third_party", "vendor", "node_modules":
		return true
	}
	return rel == "docs/_site"
}

func isSchedulerFile(rel string) bool {
	base := path.Base(rel)
	switch {
	case strings.HasPrefix(rel, ".github/workflows/") &&
		(strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")):
		return true
	case rel == ".gitlab-ci.yml", rel == ".circleci/config.yml", strings.HasPrefix(rel, ".buildkite/"):
		return true
	case strings.HasPrefix(rel, "cron/"), strings.HasPrefix(rel, "crontab/"):
		return true
	case base == "crontab", strings.HasSuffix(base, ".cron"), strings.HasSuffix(base, ".timer"), strings.HasSuffix(base, ".service"):
		return true
	default:
		return false
	}
}

func scanCronReferences(root string, files []string) (map[string][]cronReference, error) {
	out := make(map[string][]cronReference)
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		line := 0
		for sc.Scan() {
			line++
			for _, command := range parseSecopsctlCommands(sc.Text()) {
				rel, rerr := filepath.Rel(root, file)
				if rerr != nil {
					_ = f.Close()
					return nil, rerr
				}
				out[command] = append(out[command], cronReference{
					File: filepath.ToSlash(rel),
					Line: line,
				})
			}
		}
		scanErr := sc.Err()
		closeErr := f.Close()
		if scanErr != nil {
			return nil, scanErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	for command := range out {
		sort.Slice(out[command], func(i, j int) bool {
			if out[command][i].File != out[command][j].File {
				return out[command][i].File < out[command][j].File
			}
			return out[command][i].Line < out[command][j].Line
		})
	}
	return out, nil
}

func formatCronRefs(refs []cronReference) string {
	if len(refs) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, fmt.Sprintf("%s:%d", ref.File, ref.Line))
	}
	return strings.Join(parts, ",")
}

func formatCronEnabled(enabled *bool) string {
	if enabled == nil {
		return "unknown"
	}
	if *enabled {
		return "true"
	}
	return "false"
}

func formatCronLastRun(row cronSOARScheduleRow) string {
	switch {
	case row.LastRunStatus != "" && row.LastRunTime != "":
		return row.LastRunStatus + "@" + row.LastRunTime
	case row.LastRunStatus != "":
		return row.LastRunStatus
	case row.LastRunTime != "":
		return row.LastRunTime
	default:
		return "-"
	}
}

func formatCronFileLine(file string, line int) string {
	if line <= 0 {
		return file
	}
	return fmt.Sprintf("%s:%d", file, line)
}

func parseCronHeartbeatSpecs(values []string) ([]cronHeartbeatSpec, error) {
	specs := make([]cronHeartbeatSpec, 0, len(values))
	for _, value := range values {
		label, rawURL, ok := strings.Cut(value, "=")
		label = strings.TrimSpace(label)
		rawURL = strings.TrimSpace(rawURL)
		if !ok || label == "" || rawURL == "" {
			return nil, fmt.Errorf("heartbeat-status must be <label>=<url>")
		}
		if !isSafeCronHeartbeatLabel(label) {
			return nil, fmt.Errorf("heartbeat-status label %q must use only letters, numbers, '.', '_' or '-'", label)
		}
		u, err := url.Parse(rawURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("heartbeat-status %q has an invalid URL", label)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("heartbeat-status %q must use http or https", label)
		}
		if u.User != nil {
			return nil, fmt.Errorf("heartbeat-status %q must not include userinfo", label)
		}
		specs = append(specs, cronHeartbeatSpec{Label: label, URL: u.String()})
	}
	return specs, nil
}

func isSafeCronHeartbeatLabel(s string) bool {
	if len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return s != ""
}

func checkCronHeartbeats(ctx context.Context, specs []cronHeartbeatSpec, hc *http.Client) []cronHeartbeatRow {
	if len(specs) == 0 {
		return nil
	}
	if hc == nil {
		// Honor force_ipv4 from the config file. Read it validation-free so a
		// partial config (e.g. force_ipv4 set but SIEM keys absent) still pins
		// the dialer — `info cron` works without a complete config, and
		// SECOPS_FORCE_IPV4 alone is honored inside HTTPTransport.
		force := config.ReadForEdit(config.Find(cfgFile)).ForceIPv4
		hc = &http.Client{
			Timeout:   10 * time.Second,
			Transport: auth.HTTPTransport(force),
		}
	}
	rows := make([]cronHeartbeatRow, 0, len(specs))
	for _, spec := range specs {
		rows = append(rows, checkCronHeartbeat(ctx, hc, spec))
	}
	return rows
}

func checkCronHeartbeat(ctx context.Context, hc *http.Client, spec cronHeartbeatSpec) cronHeartbeatRow {
	row := cronHeartbeatRow{Label: spec.Label}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, spec.URL, nil)
	if err != nil {
		row.Error = "invalid_request"
		return row
	}
	req.Header.Set("User-Agent", "secopsctl")
	resp, err := hc.Do(req)
	if err != nil {
		row.Error = "request_failed"
		return row
	}
	defer func() { _ = resp.Body.Close() }()
	row.StatusCode = resp.StatusCode
	row.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
	if !row.OK {
		row.Error = "unexpected_status"
	}
	return row
}

func formatCronStatusCode(code int) string {
	if code == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", code)
}

func formatCronError(err string) string {
	if err == "" {
		return "-"
	}
	return err
}
