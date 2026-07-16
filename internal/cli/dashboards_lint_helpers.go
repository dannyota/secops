package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Pure helpers behind `dashboards lint`/`inspect`: chart-shape parsing,
// reserved-name and layout checks. No API calls.

func chartRefsAndLayouts(dashRaw json.RawMessage) (refs []string, layouts map[string]json.RawMessage) {
	layouts = map[string]json.RawMessage{}
	var def struct {
		Definition struct {
			Charts []struct {
				DashboardChart string          `json:"dashboardChart"`
				ChartLayout    json.RawMessage `json:"chartLayout"`
			} `json:"charts"`
		} `json:"definition"`
	}
	if json.Unmarshal(dashRaw, &def) != nil {
		return nil, layouts
	}
	for _, ch := range def.Definition.Charts {
		if ch.DashboardChart != "" {
			refs = append(refs, ch.DashboardChart)
			layouts[lastSegment(ch.DashboardChart)] = ch.ChartLayout
		}
	}
	return refs, layouts
}

func filterRefs(refs []string, id string) []string {
	want := lastSegment(id)
	for _, r := range refs {
		if lastSegment(r) == want {
			return []string{r}
		}
	}
	return nil
}

// parseChartFields extracts the typed fields from a chart's raw JSON.
func parseChartFields(raw json.RawMessage) (title, tileType string, dataSources []string, viz json.RawMessage) {
	var ch struct {
		DisplayName     string          `json:"displayName"`
		TileType        string          `json:"tileType"`
		Visualization   json.RawMessage `json:"visualization"`
		ChartDatasource struct {
			DataSources []string `json:"dataSources"`
		} `json:"chartDatasource"`
	}
	_ = json.Unmarshal(raw, &ch)
	return ch.DisplayName, ch.TileType, ch.ChartDatasource.DataSources, ch.Visualization
}

// extractRaw pulls a top-level key from raw JSON as a RawMessage.
func extractRaw(raw json.RawMessage, key string) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) == nil {
		return m[key]
	}
	return nil
}

// indentJSONPrefixed pretty-prints raw JSON with a per-line prefix.
func indentJSONPrefixed(raw json.RawMessage, prefix string) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return prefix + string(raw)
	}
	b, _ := json.MarshalIndent(v, prefix, "  ")
	return prefix + string(b)
}

// hasNoneLegend detects a legends array on a single-match-variable chart.
func hasNoneLegend(viz json.RawMessage, query string) bool {
	if len(viz) == 0 {
		return false
	}
	var v struct {
		Legends []json.RawMessage `json:"legends"`
	}
	if json.Unmarshal(viz, &v) != nil || len(v.Legends) == 0 {
		return false
	}
	matchVars := countMatchVars(query)
	return matchVars <= 1
}

// countMatchVars counts the variables in the match: section of a YARA-L stats query.
func countMatchVars(query string) int {
	lines := strings.Split(query, "\n")
	inMatch := false
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "match:") {
			inMatch = true
			continue
		}
		if inMatch {
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "outcome:") || strings.HasPrefix(trimmed, "order:") ||
				strings.HasPrefix(trimmed, "limit:") || strings.HasPrefix(trimmed, "condition:") {
				break
			}
			if strings.HasPrefix(trimmed, "$") || strings.Contains(trimmed, ",") {
				for p := range strings.SplitSeq(trimmed, ",") {
					if strings.TrimSpace(p) != "" {
						count++
					}
				}
			}
		}
	}
	return count
}

// emailMatchRE detects a match variable assigned from an email field without re.capture.
var emailMatchRE = regexp.MustCompile(
	`\$\w+\s*=\s*(?:principal|target|src|observer|intermediary|about)\.\w*\.?(?:email_addresses|userid|email)\b`)

// reCaptureRE detects re.capture already wrapping the assignment.
var reCaptureRE = regexp.MustCompile(`re\.capture\(`)

// hasLongEmailLabels checks if a query has email match variables without re.capture.
func hasLongEmailLabels(query string) bool {
	if query == "" {
		return false
	}
	for line := range strings.SplitSeq(query, "\n") {
		if emailMatchRE.MatchString(line) && !reCaptureRE.MatchString(line) {
			return true
		}
	}
	return false
}

// dashboardGlobalTimeFilter extracts the normalized time range string from a
// dashboard's global filter, e.g. "14-DAY" or "24-HOUR".
func dashboardGlobalTimeFilter(dashRaw json.RawMessage) string {
	var def struct {
		Definition struct {
			Filters []struct {
				ID                           string `json:"id"`
				IsStandardTimeRangeFilter    bool   `json:"isStandardTimeRangeFilter"`
				FilterOperatorAndFieldValues []struct {
					FieldValues []string `json:"fieldValues"`
				} `json:"filterOperatorAndFieldValues"`
			} `json:"filters"`
		} `json:"definition"`
	}
	if json.Unmarshal(dashRaw, &def) != nil {
		return ""
	}
	for _, f := range def.Definition.Filters {
		if f.IsStandardTimeRangeFilter || f.ID == "GlobalTimeFilter" {
			for _, op := range f.FilterOperatorAndFieldValues {
				if len(op.FieldValues) >= 2 {
					return op.FieldValues[0] + "-" + op.FieldValues[1]
				}
			}
		}
	}
	return ""
}

// normalizeTimeRange turns a query input.relativeTime into "N-UNIT", e.g. "1-DAY".
func normalizeTimeRange(input json.RawMessage) string {
	var inp struct {
		RelativeTime struct {
			TimeUnit     string `json:"timeUnit"`
			StartTimeVal string `json:"startTimeVal"`
		} `json:"relativeTime"`
	}
	if json.Unmarshal(input, &inp) != nil {
		return ""
	}
	if inp.RelativeTime.TimeUnit == "" || inp.RelativeTime.StartTimeVal == "" {
		return ""
	}
	return inp.RelativeTime.StartTimeVal + "-" + inp.RelativeTime.TimeUnit
}

// checkOverlaps detects charts whose grid rectangles overlap.
func checkOverlaps(refs []string, layouts, charts map[string]json.RawMessage) []lintFinding {
	type rect struct {
		id, title                    string
		startX, startY, spanX, spanY int
	}
	var rects []rect
	for _, ref := range refs {
		cid := lastSegment(ref)
		lay, ok := layouts[cid]
		if !ok || len(lay) == 0 {
			continue
		}
		var r struct {
			StartX int `json:"startX"`
			StartY int `json:"startY"`
			SpanX  int `json:"spanX"`
			SpanY  int `json:"spanY"`
		}
		if json.Unmarshal(lay, &r) != nil {
			continue
		}
		title := ""
		if raw, ok := charts[cid]; ok {
			title, _, _, _ = parseChartFields(raw)
		}
		rects = append(rects, rect{cid, title, r.StartX, r.StartY, r.SpanX, r.SpanY})
	}

	var findings []lintFinding
	for i := range rects {
		for j := i + 1; j < len(rects); j++ {
			a, b := rects[i], rects[j]
			if a.startX < b.startX+b.spanX && a.startX+a.spanX > b.startX &&
				a.startY < b.startY+b.spanY && a.startY+a.spanY > b.startY {
				findings = append(findings, lintFinding{
					ChartID: a.id, Title: a.title, Check: "overlap",
					Message: fmt.Sprintf("overlaps with chart %s (%s)", truncate(b.id, 12), b.title),
					Fixable: false,
				})
			}
		}
	}
	return findings
}
