package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/auth"
	"danny.vn/secops/config"
	"danny.vn/secops/internal/mirror"
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

func addHostCronReferences(refs map[string][]cronReference, opts cronInfoOptions) []cronHostScanRow {
	var sources []cronLineSource
	var scans []cronHostScanRow
	if opts.IncludeHost {
		var loaded []cronLineSource
		loaded, scans = hostSchedulerSources(baseContext())
		sources = append(sources, loaded...)
	}
	if len(opts.hostSources) > 0 {
		sources = append(sources, opts.hostSources...)
		scans = append(scans, cronHostScanRow{
			Source: "test-host-sources",
			Status: "scanned",
			Files:  len(opts.hostSources),
		})
	}
	if len(sources) == 0 {
		return scans
	}
	hostRefs := scanCronLineSources(sources)
	mergeCronRefs(refs, hostRefs)
	for i := range scans {
		scans[i].References = countCronReferencesForScan(hostRefs, scans[i])
	}
	return scans
}

func scanCronLineSources(sources []cronLineSource) map[string][]cronReference {
	out := make(map[string][]cronReference)
	for _, src := range sources {
		for i, line := range src.Lines {
			for _, command := range parseSecopsctlCommands(line) {
				out[command] = append(out[command], cronReference{File: src.Ref, Line: i + 1})
			}
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
	return out
}

func mergeCronRefs(dst, src map[string][]cronReference) {
	for command, refs := range src {
		dst[command] = append(dst[command], refs...)
		sort.Slice(dst[command], func(i, j int) bool {
			if dst[command][i].File != dst[command][j].File {
				return dst[command][i].File < dst[command][j].File
			}
			return dst[command][i].Line < dst[command][j].Line
		})
	}
}

func hostSchedulerSources(ctx context.Context) ([]cronLineSource, []cronHostScanRow) {
	var sources []cronLineSource
	var scans []cronHostScanRow

	if src, row := currentUserCrontabSource(ctx); row.Source != "" {
		if src.Ref != "" {
			sources = append(sources, src)
		}
		scans = append(scans, row)
	}
	systemdSources, systemdRow := userSystemdSources()
	sources = append(sources, systemdSources...)
	scans = append(scans, systemdRow)
	return sources, scans
}

func currentUserCrontabSource(ctx context.Context) (cronLineSource, cronHostScanRow) {
	row := cronHostScanRow{Source: "user-crontab"}
	if _, err := exec.LookPath("crontab"); err != nil {
		row.Status = "unavailable"
		return cronLineSource{}, row
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "crontab", "-l").CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			row.Status = "timeout"
			return cronLineSource{}, row
		}
		if strings.Contains(strings.ToLower(string(out)), "no crontab") {
			row.Status = "no_entries"
			return cronLineSource{}, row
		}
		row.Status = "read_failed"
		return cronLineSource{}, row
	}
	lines := splitCronLines(string(out))
	if len(lines) == 0 {
		row.Status = "no_entries"
		return cronLineSource{}, row
	}
	row.Status = "scanned"
	row.Files = 1
	return cronLineSource{Ref: "host:user-crontab", Lines: lines}, row
}

func userSystemdSources() ([]cronLineSource, cronHostScanRow) {
	row := cronHostScanRow{Source: "user-systemd"}
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		row.Status = "unavailable"
		return nil, row
	}
	root := filepath.Join(configDir, "systemd", "user")
	var sources []cronLineSource
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			row.Status = "not_found"
			return nil, row
		}
		row.Status = "read_failed"
		return nil, row
	}
	for _, entry := range entries {
		if entry.IsDir() || !entry.Type().IsRegular() {
			continue
		}
		base := entry.Name()
		if !strings.HasSuffix(base, ".service") && !strings.HasSuffix(base, ".timer") {
			continue
		}
		p := filepath.Join(root, entry.Name())
		raw, err := os.ReadFile(p)
		if err != nil {
			row.Status = "read_failed"
			return nil, row
		}
		lines := splitCronLines(string(raw))
		if len(lines) == 0 {
			continue
		}
		sources = append(sources, cronLineSource{
			Ref:   "host:systemd-user/" + filepath.ToSlash(entry.Name()),
			Lines: lines,
		})
	}
	if len(sources) == 0 {
		row.Status = "no_entries"
		return nil, row
	}
	row.Status = "scanned"
	row.Files = len(sources)
	return sources, row
}

func splitCronLines(s string) []string {
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	lines := raw[:0]
	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func countCronReferencesForScan(refs map[string][]cronReference, scan cronHostScanRow) int {
	prefix := ""
	switch scan.Source {
	case "user-crontab":
		prefix = "host:user-crontab"
	case "user-systemd":
		prefix = "host:systemd-user/"
	case "test-host-sources":
		prefix = "host:"
	default:
		prefix = scan.Source
	}
	count := 0
	for _, rows := range refs {
		for _, ref := range rows {
			if ref.File == prefix || strings.HasPrefix(ref.File, prefix) {
				count++
			}
		}
	}
	return count
}

func scanSOARSchedules(root string) ([]cronSOARScheduleRow, error) {
	var rows []cronSOARScheduleRow
	playbooks, err := scanPlaybookSchedules(root, filepath.Join(root, mirror.DirSOAR, mirror.DirSOARPlaybooks))
	if err != nil {
		return nil, err
	}
	rows = append(rows, playbooks...)
	jobs, err := scanJobSchedules(root, filepath.Join(root, mirror.DirSOAR, mirror.DirSOARJobs))
	if err != nil {
		return nil, err
	}
	rows = append(rows, jobs...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Type != rows[j].Type {
			return rows[i].Type < rows[j].Type
		}
		if rows[i].File != rows[j].File {
			return rows[i].File < rows[j].File
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}

func scanPlaybookSchedules(root, dir string) ([]cronSOARScheduleRow, error) {
	return scanSOARJSONDir(root, dir, func(rel string, line int, body map[string]any) (cronSOARScheduleRow, bool) {
		trigger, _ := body["trigger"].(map[string]any)
		cron := strings.TrimSpace(cronStringField(trigger, "cronSchedule"))
		if cron == "" {
			return cronSOARScheduleRow{}, false
		}
		name := cronStringField(body, "name")
		if name == "" {
			name = strings.TrimSuffix(path.Base(rel), ".json")
		}
		enabled := firstCronBool(
			cronBoolField(body, "enabled"),
			cronBoolField(body, "isEnabled"),
			cronBoolField(trigger, "enabled"),
			cronBoolField(trigger, "isEnabled"),
		)
		return cronSOARScheduleRow{
			Type:         "soar_playbook_trigger",
			Name:         name,
			Enabled:      enabled,
			CronSchedule: cron,
			File:         rel,
			Line:         line,
		}, true
	})
}

func scanJobSchedules(root, dir string) ([]cronSOARScheduleRow, error) {
	return scanSOARJSONDir(root, dir, func(rel string, line int, body map[string]any) (cronSOARScheduleRow, bool) {
		cron := strings.TrimSpace(cronStringField(body, "cronSchedule"))
		if cron == "" {
			return cronSOARScheduleRow{}, false
		}
		name := cronStringField(body, "name")
		if name == "" {
			name = strings.TrimSuffix(path.Base(rel), ".json")
		}
		return cronSOARScheduleRow{
			Type:          "soar_job",
			Name:          name,
			Enabled:       cronBoolField(body, "enabled"),
			CronSchedule:  cron,
			LastRunStatus: cronStringField(body, "lastRunStatus"),
			LastRunTime:   cronStringField(body, "lastRunTime"),
			File:          rel,
			Line:          line,
		}, true
	})
}

func scanSOARJSONDir(root, dir string, build func(string, int, map[string]any) (cronSOARScheduleRow, bool)) ([]cronSOARScheduleRow, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rows []cronSOARScheduleRow
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") || !e.Type().IsRegular() {
			continue
		}
		file := filepath.Join(dir, e.Name())
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var body map[string]any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		row, ok := build(rel, jsonKeyLine(raw, "cronSchedule"), body)
		if ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func parseSecopsctlCommands(line string) []string {
	fields := cronCommandFields(line)
	var out []string
	for i := 0; i < len(fields); i++ {
		start := -1
		switch {
		case i+2 < len(fields) && fields[i] == "go" && fields[i+1] == "run" && strings.HasSuffix(fields[i+2], "cmd/secopsctl"):
			start = i + 3
			i = start - 1
		case isSecopsctlExecutable(fields[i]):
			start = i + 1
		}
		if start >= 0 {
			out = append(out, parseSecopsctlCommandFields(fields[start:])...)
		}
	}
	return out
}

func parseSecopsctlCommandFields(fields []string) []string {
	fields = skipCronFlags(fields)
	if len(fields) == 0 {
		return nil
	}
	switch fields[0] {
	case "drift":
		return []string{"drift"}
	case "push":
		if target, ok := firstCronArg(fields[1:]); ok {
			return []string{"push " + target}
		}
	case "soar":
		rest := skipCronFlags(fields[1:])
		if len(rest) > 0 && rest[0] == "push" {
			if target, ok := firstCronArg(rest[1:]); ok {
				return []string{"soar push " + target}
			}
		}
	}
	return nil
}

func isSecopsctlExecutable(s string) bool {
	base := path.Base(s)
	return base == "secopsctl" || base == "secopsctl.exe"
}

func skipCronFlags(fields []string) []string {
	for len(fields) > 0 && strings.HasPrefix(fields[0], "-") {
		flag := fields[0]
		fields = fields[1:]
		if cronFlagTakesValue(flag) && !strings.Contains(flag, "=") && len(fields) > 0 {
			fields = fields[1:]
		}
	}
	return fields
}

func firstCronArg(fields []string) (string, bool) {
	fields = skipCronFlags(fields)
	if len(fields) == 0 || strings.HasPrefix(fields[0], "-") {
		return "", false
	}
	return fields[0], true
}

func cronFlagTakesValue(flag string) bool {
	name := strings.TrimLeft(flag, "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "config", "filter", "from", "hours", "ids", "out", "reason", "root", "root-cause", "rule", "rules-dir", "to":
		return true
	default:
		return false
	}
}

func cronCommandFields(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', '"', '\'', '`', ':', ',', '[', ']', '(', ')', ';', '&', '|':
			return true
		default:
			return false
		}
	})
}

func cronStringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return fmt.Sprintf("%t", v)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func cronBoolField(m map[string]any, key string) *bool {
	if m == nil {
		return nil
	}
	v, ok := m[key].(bool)
	if !ok {
		return nil
	}
	return &v
}

func firstCronBool(values ...*bool) *bool {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func jsonKeyLine(raw []byte, key string) int {
	needle := `"` + key + `"`
	sc := bufio.NewScanner(bytes.NewReader(raw))
	line := 0
	for sc.Scan() {
		line++
		if strings.Contains(sc.Text(), needle) {
			return line
		}
	}
	return 0
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
