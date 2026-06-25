package cli

// Host/systemd/crontab + SOAR-schedule scanning and cron-line parsing.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"danny.vn/secops/internal/mirror"
)

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
