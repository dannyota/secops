package legacy

import (
	"encoding/json"
	"testing"
)

// TestLiveReportsReads exercises the advanced-reports read endpoints (safe).
// Runs under SECOPS_SOAR_SMOKE=1.
//
// ReportGetTemplates is a zero-arg list that is green on a tenant with no prior
// setup. ReportGetSchedules needs an array of report-template ids as its body,
// so its argument is DERIVED from the template list above (an empty array on a
// bare tenant, which the endpoint accepts) — no id is ever guessed.
//
// RefreshAdvancedReports / GenerateReportTemplate / ShareAdvancedReport and the
// AddOrUpdate*/Remove*/Delete* methods are recomputes or live mutations and are
// deliberately excluded from the read test.
func TestLiveReportsReads(t *testing.T) {
	lc, ctx := liveClient(t)

	raw := readProbe(t, "reports/GetReportTemplates", func() (RawJSON, error) {
		return lc.ReportGetTemplates(ctx)
	})

	// Derive template ids from the list, then read the schedules for them. On a
	// bare tenant this is an empty []int, which GetReportSchedules accepts.
	ids := reportTemplateIDs(raw)
	readProbe(t, "reports/GetReportSchedules", func() (RawJSON, error) {
		return lc.ReportGetSchedules(ctx, ids)
	})
}

// reportTemplateIDs pulls the integer ids out of a GetReportTemplates response
// (a JSON array of ReportTemplateDetails). Returns an empty, non-nil slice when
// the tenant has no templates so the derived call still sends a valid array.
func reportTemplateIDs(raw RawJSON) []int {
	ids := []int{}
	var arr []struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(raw, &arr) == nil {
		for _, t := range arr {
			ids = append(ids, int(t.ID))
		}
	}
	return ids
}

// no CRUD test: the Reports tag's mutable resources (report templates, widgets,
// schedules) carry deeply-nested, schema-unstable legacy payloads — and the list
// includes isSystem templates — so cloning one as a create template is not a
// clearly-safe cosmetic lifecycle. Reads only.
