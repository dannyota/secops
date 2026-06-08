package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

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

type cronInfoReport struct {
	Root           string                `json:"root"`
	SchedulerFiles int                   `json:"scheduler_files"`
	Commands       []cronCommandRow      `json:"commands"`
	SOARSchedules  []cronSOARScheduleRow `json:"soar_schedules"`
}

func newInfoCronCmd() *cobra.Command {
	var root string
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Report local scheduler references to secopsctl automation (offline)",
		Long: "Scan local scheduler-like files for secopsctl push/drift references.\n" +
			"Also scan pulled SOAR jobs/playbooks for cronSchedule values. This is\n" +
			"an offline orphan check: secopsctl does not own or inspect the host\n" +
			"scheduler, and raw scheduler lines are not echoed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := buildCronInfoReport(root)
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
	return cmd
}

func buildCronInfoReport(root string) (cronInfoReport, error) {
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
	return cronInfoReport{
		Root:           absRoot,
		SchedulerFiles: len(files),
		Commands:       rows,
		SOARSchedules:  schedules,
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

	fmt.Fprintf(w, "\nSOAR schedules found: %d\n", len(report.SOARSchedules))
	if len(report.SOARSchedules) == 0 {
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
