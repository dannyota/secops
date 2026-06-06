package cli

import (
	"bytes"
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
)

func TestDescribeCuratedUpd(t *testing.T) {
	tr, fl := true, false
	if got := describeCuratedUpd(chronicle.CuratedDeploymentUpdate{Enabled: &tr}); got != "enabled=true" {
		t.Errorf("enabled only = %q", got)
	}
	if got := describeCuratedUpd(chronicle.CuratedDeploymentUpdate{Enabled: &fl, Alerting: &tr}); got != "enabled=false alerting=true" {
		t.Errorf("both = %q", got)
	}
}

func TestFilterCuratedRows(t *testing.T) {
	rows := []curatedRow{
		{Display: "AWS - Suspicious IAM"},
		{Display: "Azure - Sign-in"},
		{Display: "aws cloudtrail"},
	}
	got := filterCuratedRows(rows, "aws")
	if len(got) != 2 {
		t.Fatalf("filter 'aws' matched %d, want 2 (case-insensitive)", len(got))
	}
}

func TestEmitCuratedRows(t *testing.T) {
	rows := []curatedRow{
		{Category: "CAT", RuleSet: "RS", Precision: "precise", Enabled: true, Alerting: false, Display: "AWS - IAM"},
	}
	var buf bytes.Buffer
	if err := emitCuratedRows(&buf, rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"en=true", "al=false", "precise", "AWS - IAM",
		"--category CAT --ruleset RS --precision precise", "1 deployment(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
}

func TestEmitCuratedRowsEmpty(t *testing.T) {
	var buf bytes.Buffer
	_ = emitCuratedRows(&buf, nil)
	if !strings.Contains(buf.String(), "no curated deployments.") {
		t.Errorf("want empty message, got %q", buf.String())
	}
}
