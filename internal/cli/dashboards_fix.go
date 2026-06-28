package cli

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"danny.vn/secops/chronicle"
)

// Fix helpers for `dashboards fix` — each applies one mechanical remedy to a
// single chart via the :editChart verb, re-reading the chart first (etag-safe).

// applyNoLegend removes the legends array from a chart's visualization.
func applyNoLegend(ctx context.Context, c *chronicle.Client, dashID, chartID string) error {
	chart, err := c.GetChart(ctx, chartID)
	if err != nil {
		return err
	}
	var vizMap map[string]json.RawMessage
	vizRaw := extractRaw(chart, "visualization")
	if len(vizRaw) == 0 {
		return nil
	}
	if json.Unmarshal(vizRaw, &vizMap) != nil {
		return nil
	}
	if _, ok := vizMap["legends"]; !ok {
		return nil
	}
	delete(vizMap, "legends")
	newViz, _ := json.Marshal(vizMap)

	cbody := map[string]any{
		"name":          nestedString(chart, "name"),
		"etag":          nestedString(chart, "etag"),
		"visualization": json.RawMessage(newViz),
	}
	chartJSON, _ := json.Marshal(cbody)
	_, err = c.EditChart(ctx, dashID, chronicle.EditChartInput{DashboardChart: chartJSON})
	return err
}

// applyStripDomain wraps email match variables in re.capture() in the chart's query.
func applyStripDomain(ctx context.Context, c *chronicle.Client, dashID, chartID string) error {
	chart, err := c.GetChart(ctx, chartID)
	if err != nil {
		return err
	}
	qRef := nestedString(chart, "chartDatasource", "dashboardQuery")
	if qRef == "" {
		return nil
	}
	qraw, err := c.GetQuery(ctx, qRef)
	if err != nil {
		return err
	}
	query := nestedString(qraw, "query")
	if query == "" {
		return nil
	}

	newQuery := wrapEmailsInCapture(query)
	if newQuery == query {
		return nil
	}

	body := map[string]any{
		"name":  qRef,
		"query": newQuery,
		"etag":  nestedString(qraw, "etag"),
	}
	qJSON, _ := json.Marshal(body)
	_, err = c.EditChart(ctx, dashID, chronicle.EditChartInput{DashboardQuery: qJSON})
	return err
}

// wrapEmailsInCapture rewrites email match variable assignments to use re.capture.
func wrapEmailsInCapture(query string) string {
	re := regexp.MustCompile(
		`(\$\w+\s*=\s*)((?:principal|target|src|observer|intermediary|about)\.\w*\.?(?:email_addresses|userid|email))\b`)
	lines := strings.Split(query, "\n")
	for i, line := range lines {
		if re.MatchString(line) && !reCaptureRE.MatchString(line) {
			lines[i] = re.ReplaceAllString(line, `${1}re.capture(${2}, "^([^@]+)")`)
		}
	}
	return strings.Join(lines, "\n")
}

// applySyncTime aligns a chart's query time range with the dashboard's global filter.
func applySyncTime(ctx context.Context, c *chronicle.Client, dashID, chartID string, dashRaw json.RawMessage) error {
	chart, err := c.GetChart(ctx, chartID)
	if err != nil {
		return err
	}
	qRef := nestedString(chart, "chartDatasource", "dashboardQuery")
	if qRef == "" {
		return nil
	}
	qraw, err := c.GetQuery(ctx, qRef)
	if err != nil {
		return err
	}

	dashTime := dashboardGlobalTimeFilter(dashRaw)
	if dashTime == "" {
		return nil
	}
	parts := strings.SplitN(dashTime, "-", 2)
	if len(parts) != 2 {
		return nil
	}
	newInput, _ := json.Marshal(map[string]any{
		"relativeTime": map[string]any{
			"timeUnit":     parts[1],
			"startTimeVal": parts[0],
		},
	})

	body := map[string]any{
		"name":  qRef,
		"input": json.RawMessage(newInput),
		"etag":  nestedString(qraw, "etag"),
	}
	qJSON, _ := json.Marshal(body)
	_, err = c.EditChart(ctx, dashID, chronicle.EditChartInput{DashboardQuery: qJSON})
	return err
}
