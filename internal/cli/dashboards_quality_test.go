package cli

import (
	"encoding/json"
	"testing"
)

func TestHasNoneLegend(t *testing.T) {
	withLegend := json.RawMessage(`{"legends":[{"right":12}],"series":[{"seriesType":"BAR"}]}`)
	singleMatch := "match:\n  $user\noutcome:\n  $count = count(metadata.id)"
	if !hasNoneLegend(withLegend, singleMatch) {
		t.Error("single-match chart with legends should flag")
	}

	multiMatch := "match:\n  $user, $ip\noutcome:\n  $count = count(metadata.id)"
	if hasNoneLegend(withLegend, multiMatch) {
		t.Error("multi-match chart with legends should NOT flag")
	}

	noLegend := json.RawMessage(`{"series":[{"seriesType":"BAR"}]}`)
	if hasNoneLegend(noLegend, singleMatch) {
		t.Error("chart without legends array should NOT flag")
	}
}

func TestHasLongEmailLabels(t *testing.T) {
	withEmail := "$user = principal.user.email_addresses"
	if !hasLongEmailLabels(withEmail) {
		t.Error("email match without re.capture should flag")
	}
	withCapture := `$user = re.capture(principal.user.email_addresses, "^([^@]+)")`
	if hasLongEmailLabels(withCapture) {
		t.Error("email match with re.capture should NOT flag")
	}
	noEmail := "$host = principal.hostname"
	if hasLongEmailLabels(noEmail) {
		t.Error("non-email match should NOT flag")
	}
}

func TestWrapEmailsInCapture(t *testing.T) {
	input := "$user = principal.user.email_addresses\n$host = principal.hostname"
	got := wrapEmailsInCapture(input)
	if got == input {
		t.Error("should have rewritten the email line")
	}
	if hasLongEmailLabels(got) {
		t.Error("after wrapping, should no longer flag as long labels")
	}
}

func TestDashboardGlobalTimeFilter(t *testing.T) {
	raw := json.RawMessage(`{"definition":{"filters":[{"id":"GlobalTimeFilter","isStandardTimeRangeFilter":true,"filterOperatorAndFieldValues":[{"filterOperator":"PAST","fieldValues":["14","DAY"]}]}]}}`)
	if got := dashboardGlobalTimeFilter(raw); got != "14-DAY" {
		t.Errorf("got %q, want 14-DAY", got)
	}
}

func TestNormalizeTimeRange(t *testing.T) {
	input := json.RawMessage(`{"relativeTime":{"timeUnit":"HOUR","startTimeVal":"24"}}`)
	if got := normalizeTimeRange(input); got != "24-HOUR" {
		t.Errorf("got %q, want 24-HOUR", got)
	}
}

func TestCountMatchVars(t *testing.T) {
	tests := []struct {
		query string
		want  int
	}{
		{"match:\n  $user\noutcome:\n  $c = count(x)", 1},
		{"match:\n  $user, $ip\noutcome:\n  $c = count(x)", 2},
		{"outcome:\n  $c = count(x)", 0},
	}
	for _, tt := range tests {
		if got := countMatchVars(tt.query); got != tt.want {
			t.Errorf("countMatchVars(%q) = %d, want %d", tt.query, got, tt.want)
		}
	}
}

func TestBuildVisualizationNewTypes(t *testing.T) {
	tests := []struct {
		chartType  string
		x, y, by   string
		wantSeries string
	}{
		{"area", "day", "count", "", "LINE"},
		{"scatter", "x", "y", "", "SCATTER"},
		{"gauge", "", "value", "", "GAUGE"},
		{"metrics", "", "count", "", "METRICS"},
		{"map", "lat", "lng", "count", "MAP"},
	}
	for _, tt := range tests {
		b, err := buildVisualization(tt.chartType, tt.x, tt.y, tt.by)
		if err != nil {
			t.Errorf("buildVisualization(%q) error: %v", tt.chartType, err)
			continue
		}
		var v struct {
			Series []struct {
				SeriesType string `json:"seriesType"`
			} `json:"series"`
		}
		if err := json.Unmarshal(b, &v); err != nil {
			t.Errorf("unmarshal %q viz: %v", tt.chartType, err)
			continue
		}
		if len(v.Series) == 0 || v.Series[0].SeriesType != tt.wantSeries {
			t.Errorf("%q seriesType = %v, want %s", tt.chartType, v.Series, tt.wantSeries)
		}
	}
}
