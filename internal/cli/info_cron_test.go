package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInfoCronCommandRegistered(t *testing.T) {
	info := commandChild(rootCmd, "info")
	if info == nil {
		t.Fatal("info command not registered")
	}
	if commandChild(info, "cron") == nil {
		t.Fatal("info cron command not registered")
	}
}

func TestBuildCronInfoReport(t *testing.T) {
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"name: secops",
		"jobs:",
		"  sync:",
		"    steps:",
		"      - run: secopsctl push rules-deploy --rule example --yes",
		"      - run: secopsctl soar push jobs --yes",
		"      - run: go run ./cmd/secopsctl drift --soar",
	}
	workflow := filepath.Join(workflowDir, "secops.yaml")
	if err := os.WriteFile(workflow, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	playbookDir := filepath.Join(root, "soar", "playbooks")
	if err := os.MkdirAll(playbookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	playbook := strings.Join([]string{
		`{`,
		`  "name": "Case Hygiene",`,
		`  "enabled": true,`,
		`  "trigger": {`,
		`    "cronSchedule": "0 8 * * *"`,
		`  }`,
		`}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(playbookDir, "Case_Hygiene.json"), []byte(playbook+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobDir := filepath.Join(root, "soar", "jobs")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	job := strings.Join([]string{
		`{`,
		`  "name": "Nightly Sync",`,
		`  "enabled": false,`,
		`  "cronSchedule": "*/15 * * * *",`,
		`  "lastRunStatus": "Success",`,
		`  "lastRunTime": "1700000000"`,
		`}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(jobDir, "Nightly_Sync.json"), []byte(job+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := buildCronInfoReport(root)
	if err != nil {
		t.Fatalf("buildCronInfoReport: %v", err)
	}
	if report.SchedulerFiles != 1 {
		t.Fatalf("SchedulerFiles = %d, want 1", report.SchedulerFiles)
	}
	rows := map[string]cronCommandRow{}
	for _, row := range report.Commands {
		rows[row.Command] = row
	}

	assertRef := func(command string, line int) {
		t.Helper()
		row, ok := rows[command]
		if !ok {
			t.Fatalf("missing command row %q", command)
		}
		if !row.Referenced {
			t.Fatalf("%s not marked referenced", command)
		}
		want := []cronReference{{File: ".github/workflows/secops.yaml", Line: line}}
		if !reflect.DeepEqual(row.References, want) {
			t.Fatalf("%s refs = %#v, want %#v", command, row.References, want)
		}
	}
	assertRef("push rules-deploy", 5)
	assertRef("soar push jobs", 6)
	assertRef("drift", 7)

	row, ok := rows["push rules-create"]
	if !ok {
		t.Fatal("missing push rules-create row")
	}
	if row.Referenced || len(row.References) != 0 {
		t.Fatalf("push rules-create row = %+v, want unreferenced", row)
	}

	schedules := map[string]cronSOARScheduleRow{}
	for _, row := range report.SOARSchedules {
		schedules[row.Type+"/"+row.Name] = row
	}
	playbookRow, ok := schedules["soar_playbook_trigger/Case Hygiene"]
	if !ok {
		t.Fatalf("missing playbook trigger schedule: %#v", report.SOARSchedules)
	}
	if playbookRow.CronSchedule != "0 8 * * *" || playbookRow.File != "soar/playbooks/Case_Hygiene.json" || playbookRow.Line != 5 {
		t.Fatalf("playbook trigger row = %+v", playbookRow)
	}
	if playbookRow.Enabled == nil || !*playbookRow.Enabled {
		t.Fatalf("playbook enabled = %#v, want true", playbookRow.Enabled)
	}
	jobRow, ok := schedules["soar_job/Nightly Sync"]
	if !ok {
		t.Fatalf("missing job schedule: %#v", report.SOARSchedules)
	}
	if jobRow.CronSchedule != "*/15 * * * *" || jobRow.File != "soar/jobs/Nightly_Sync.json" || jobRow.Line != 4 {
		t.Fatalf("job row = %+v", jobRow)
	}
	if jobRow.Enabled == nil || *jobRow.Enabled {
		t.Fatalf("job enabled = %#v, want false", jobRow.Enabled)
	}
	if jobRow.LastRunStatus != "Success" || jobRow.LastRunTime != "1700000000" {
		t.Fatalf("job last run = (%q,%q)", jobRow.LastRunStatus, jobRow.LastRunTime)
	}
}

func TestParseSecopsctlCommands(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{
			line: `run: secopsctl --config config/example.yaml --json push feeds --yes`,
			want: []string{"push feeds"},
		},
		{
			line: `run: secopsctl push --out ./state feeds --dry-run`,
			want: []string{"push feeds"},
		},
		{
			line: `command: ["/usr/local/bin/secopsctl", "soar", "push", "--out", "./state", "webhooks", "--yes"]`,
			want: []string{"soar push webhooks"},
		},
		{
			line: `run: go run ./cmd/secopsctl --json drift --soar`,
			want: []string{"drift"},
		},
		{
			line: `run: secopsctl --json info cron`,
			want: nil,
		},
		{
			line: `run: secopsctl query udm 'metadata.event_type = "PUSH" AND target = "feeds"'`,
			want: nil,
		},
	}

	for _, tt := range tests {
		if got := parseSecopsctlCommands(tt.line); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseSecopsctlCommands(%q) = %#v, want %#v", tt.line, got, tt.want)
		}
	}
}

func TestSchedulerFiles(t *testing.T) {
	root := t.TempDir()
	files := []string{
		".github/workflows/secops.yml",
		"cron/nightly.cron",
		"systemd/secops.service",
		"docs/_site/cron/ignored.cron",
		"third_party/cron/ignored.cron",
		"notes.txt",
	}
	for _, rel := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("secopsctl drift\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	gotFiles, err := schedulerFiles(root)
	if err != nil {
		t.Fatalf("schedulerFiles: %v", err)
	}
	got := make([]string, 0, len(gotFiles))
	for _, file := range gotFiles {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.ToSlash(rel))
	}
	want := []string{
		".github/workflows/secops.yml",
		"cron/nightly.cron",
		"systemd/secops.service",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schedulerFiles = %#v, want %#v", got, want)
	}
}
