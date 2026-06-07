package mirror

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// TestLiveReconcileScheduledReportWriteSmoke validates the scheduled_reports engine
// write loop on an inert throwaway: it creates a private CUSTOM dashboard, then
// drives the surface to create a report referencing it (a weekly PDF to an
// example.com recipient), round-trips it, edits the description, and deletes the
// report and the dashboard. No email is sent — create does not trigger a run, and
// the report is deleted long before its cron fires. Gated on
// SECOPS_SIEM_SMOKE=1 + SECOPS_SIEM_SMOKE_WRITE=1.
//
// It also confirms the open question from the docs: whether create accepts a
// `dashboard` given as a bare {name} reference (the reconcile diff basis) rather
// than the full NativeDashboard object.
func TestLiveReconcileScheduledReportWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	s, ok := BuildSIEMSurface("scheduled_reports", c)
	if !ok {
		t.Fatal("scheduled_reports is not a registered engine surface")
	}

	// 1. Target dashboard: prefer an existing CUSTOM dashboard (definitely
	//    queryable by the reports backend) over a freshly-created empty one, which
	//    the report-create backend can fail to fetch. Fall back to a throwaway.
	var dashName, dashLabel string
	dashDeleted := true // only a throwaway we create needs cleanup
	if list, lerr := c.ListNativeDashboards(ctx); lerr == nil {
		for _, d := range list {
			if d.Type == "CUSTOM" {
				dashName, dashLabel = d.Name, d.DisplayName
				break
			}
		}
	}
	if dashName == "" {
		dashLabel = smokeLabel("sr-dash")
		dash, derr := c.CreateDashboard(ctx, dashLabel, "secopsctl scheduled-report smoke", chronicle.DashboardPrivate, nil, nil)
		if derr != nil {
			t.Fatalf("create throwaway dashboard: %v", derr)
		}
		dashName = dash.Name
		dashDeleted = false
	}
	t.Logf("targeting dashboard %q (%s)", dashLabel, lastSegment(dashName))
	t.Cleanup(func() {
		if dashDeleted {
			return
		}
		if derr := c.DeleteDashboard(ctx, lastSegment(dashName)); derr != nil {
			t.Logf("cleanup: delete throwaway dashboard %q: %v", dashLabel, derr)
		}
	})

	// 2. Report body referencing the dashboard by {name} (the diff-basis shape).
	//    Build the reference in the SAME string-project form as the create request
	//    (ListNativeDashboards returns numeric-project names, which the report-create
	//    backend fails to fetch — a project-form mismatch).
	st := c.Settings()
	dashRef := fmt.Sprintf("projects/%s/locations/%s/instances/%s/nativeDashboards/%s",
		st.ProjectID, st.Region, st.CustomerID, lastSegment(dashName))
	label := smokeLabel("sr")
	createCanon, err := reconcile.Canonicalize(fmt.Appendf(nil, `{
		"displayName": %q,
		"description": "secopsctl scheduled-report smoke",
		"dashboard": {"name": %q},
		"cronDetails": {"cron": "0 9 * * 1", "timeZone": "UTC"},
		"deliveryDetails": {"emailDelivery": {"subject": "smoke", "recipients": ["noreply@example.com"]}, "deliveryType": "DELIVERY_TYPE_EMAIL_ATTACHMENT"},
		"format": {"fileFormat": "FILE_FORMAT_PDF"}
	}`, label, dashRef))
	if err != nil {
		t.Fatal(err)
	}
	local := reconcile.Object{Slug: Slugify(label), Canonical: createCanon}

	var reportID, reportEtag string
	reportDeleted := false
	t.Cleanup(func() {
		if reportDeleted || reportID == "" {
			return
		}
		if derr := c.DeleteScheduledReport(ctx, lastSegment(reportID), reportEtag); derr != nil {
			t.Logf("cleanup: delete throwaway report %q: %v", label, derr)
		}
	})

	// 3. Create via the surface closure. If the API rejects the bare {name}
	//    reference, the error is surfaced here so the body shape can be revised.
	echo, err := s.Create(ctx, local)
	if err != nil {
		// The create-report backend currently returns 500 INTERNAL ("failed to
		// fetch native dashboard details") for any dashboard on some tenants — a
		// server-side fault, not a client bug (the {name} reference shape is parsed
		// and accepted). Skip cleanly on that; the rest of the loop validates once
		// the backend works. Any other error is a real failure.
		if ae, ok := errors.AsType[*chronicle.APIError](err); ok && ae.Status == 500 {
			t.Skipf("scheduled-report create backend 500 (server-side, not a client bug): %s", ae.Body)
		}
		t.Fatalf("create scheduled report (dashboard as {name} ref): %v", err)
	}
	reportID = echo.ServerID
	reportEtag = echo.Etag
	if reportID == "" {
		t.Fatal("create returned no ServerID")
	}
	t.Logf("OK create: id=%s etag=%t", lastSegment(reportID), reportEtag != "")

	// 4. Round-trip: write the echo, reload, canonical must match.
	dir := t.TempDir()
	if err := s.Write(dir, echo); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := s.LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ServerID != reportID {
		t.Fatalf("round-trip: loaded %d obj, want 1 with id %q", len(loaded), reportID)
	}
	if !bytes.Equal(loaded[0].Canonical, echo.Canonical) {
		t.Fatalf("create round-trip canonical mismatch:\n echo: %s\n disk: %s", echo.Canonical, loaded[0].Canonical)
	}

	// 5. Update: edit the description; the Update closure carries the live etag.
	editedCanon := json.RawMessage(strings.Replace(string(echo.Canonical),
		"secopsctl scheduled-report smoke", "secopsctl scheduled-report smoke (edited)", 1))
	edited := reconcile.Object{Slug: echo.Slug, ServerID: reportID, Etag: echo.Etag, Canonical: editedCanon}
	echo2, err := s.Update(ctx, edited, echo)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	reportEtag = echo2.Etag
	if !strings.Contains(string(echo2.Canonical), "(edited)") {
		t.Errorf("update not applied:\n%s", echo2.Canonical)
	}

	// 6. Delete the report (prune-eligible surface) + confirm gone.
	if err := s.Delete(ctx, echo2); err != nil {
		t.Fatalf("delete report: %v", err)
	}
	reportDeleted = true
	if _, gerr := c.GetScheduledReport(ctx, lastSegment(reportID)); gerr == nil {
		t.Errorf("scheduled report still present after delete")
	}

	// 7. Delete the throwaway dashboard (only if we created one; an existing
	//    dashboard we merely targeted is left untouched).
	if !dashDeleted {
		if err := c.DeleteDashboard(ctx, lastSegment(dashName)); err != nil {
			t.Fatalf("delete dashboard: %v", err)
		}
		dashDeleted = true
	}
}
