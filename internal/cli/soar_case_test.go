package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCaseBody(t *testing.T) {
	b := caseBody(7, "")
	if b["caseId"] != 7 {
		t.Errorf("caseId = %v, want 7", b["caseId"])
	}
	if _, ok := b["alertIdentifier"]; ok {
		t.Error("empty alert must not be added to the body")
	}
	b = caseBody(7, "A1")
	if b["alertIdentifier"] != "A1" {
		t.Errorf("alertIdentifier = %v, want A1", b["alertIdentifier"])
	}
}

// runCaseDryRun executes a leaf case verb in dry-run mode (no credentials
// needed) and returns its stdout, which includes the JSON request body.
func runCaseDryRun(t *testing.T, cmd *cobra.Command, args ...string) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	cmd.SetArgs(append(args, "--dry-run"))
	err := cmd.Execute()
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	if !strings.Contains(string(out), "DRY RUN") {
		t.Fatalf("expected a dry run, got:\n%s", out)
	}
	return string(out)
}

// TestCaseVerbBodies pins the swagger-verified request field names so a typo
// can't silently ship a body the live API rejects.
func TestCaseVerbBodies(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
		args []string
		want []string
	}{
		{
			"assign", newCaseAssignCmd(),
			[]string{"--id", "7", "--user", "analyst1"},
			[]string{`"caseId": 7`, `"userId": "analyst1"`},
		},
		{
			"rename", newCaseRenameCmd(),
			[]string{"--id", "7", "--title", "renamed"},
			[]string{`"caseId": 7`, `"title": "renamed"`},
		},
		{
			"stage", newCaseStageCmd(),
			[]string{"--id", "7", "--stage", "Triage"},
			[]string{`"stage": "Triage"`},
		},
		{
			"tag", newCaseTagCmd(false),
			[]string{"--id", "5", "--tag", "phishing", "--alert", "A1"},
			[]string{`"tag": "phishing"`, `"alertIdentifier": "A1"`},
		},
		{
			"describe", newCaseDescribeCmd(),
			[]string{"--id", "7", "--description", "d"},
			[]string{`"description": "d"`},
		},
		{
			"importance", newCaseImportanceCmd(),
			[]string{"--id", "7", "--important=false"},
			[]string{`"isImportant": false`},
		},
		{
			"close", newCaseCloseCmd(),
			[]string{"--id", "9", "--reason", "Malicious", "--root-cause", "RC", "--comment", "done"},
			[]string{`"caseId": 9`, `"reason": "Malicious"`, `"rootCause": "RC"`, `"comment": "done"`},
		},
		{
			"merge", newCaseMergeCmd(),
			[]string{"--ids", "1,2", "--into", "3"},
			[]string{`"casesIds"`, `"caseToMergeWith": 3`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runCaseDryRun(t, tc.cmd, tc.args...)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("body missing %q in:\n%s", w, out)
				}
			}
		})
	}
}
