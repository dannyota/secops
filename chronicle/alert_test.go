package chronicle

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"danny.vn/secops/auth"
)

// bodyRT returns a fixed 200 body for any request — no network or credentials
// (WithHTTPClient replaces the whole client, so auth is never invoked).
type bodyRT struct{ body string }

func (r *bodyRT) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     make(http.Header),
	}, nil
}

func alertTestClient(t *testing.T, body string) *Client {
	t.Helper()
	c, err := NewClient(
		Settings{ProjectID: "pid", ProjectNumber: "000000000000", Region: "us", CustomerID: "cust"},
		auth.OAuth(),
		WithHTTPClient(&http.Client{Transport: &bodyRT{body}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestAlertGetShapes locks the legacy-alert decode tolerance: GetAlert accepts the
// alert wrapped under "alert" (live) OR bare (older wrapper shape), and the typed
// time fields fill from createdTime/detectionTime (live) or createTime/
// detectionTimestamp (older) — so the SDK supports either legacy-API shape.
func TestAlertGetShapes(t *testing.T) {
	ctx := context.Background()

	wrapped := `{"alert":{"id":"de_1","type":"RULE_DETECTION",` +
		`"createdTime":"2026-01-02T03:04:05Z","detectionTime":"2026-01-02T03:00:00Z",` +
		`"feedbackSummary":{"status":"OPEN","severityDisplay":"HIGH"}}}`
	a, err := alertTestClient(t, wrapped).GetAlert(ctx, "de_1", false)
	if err != nil {
		t.Fatalf("wrapped: %v", err)
	}
	if a.ID != "de_1" || a.CreateTime == "" || a.DetectionTime == "" {
		t.Errorf("wrapped/live decode lost fields: %+v", a)
	}
	if a.FeedbackSummary == nil || a.FeedbackSummary.SeverityDisplay != "HIGH" {
		t.Errorf("severityDisplay must decode as a string label: %+v", a.FeedbackSummary)
	}

	bare := `{"id":"de_2","type":"RULE_DETECTION",` +
		`"createTime":"2026-01-02T03:04:05Z","detectionTimestamp":"2026-01-02T03:00:00Z"}`
	b, err := alertTestClient(t, bare).GetAlert(ctx, "de_2", false)
	if err != nil {
		t.Fatalf("bare: %v", err)
	}
	if b.ID != "de_2" || b.CreateTime == "" || b.DetectionTime == "" {
		t.Errorf("bare/older-keys decode lost fields: %+v", b)
	}
}

// TestAlertsViewShapes locks GetAlerts decoding of both the live streamed JSON
// array of snapshot fragments and an older single-object response.
func TestAlertsViewShapes(t *testing.T) {
	ctx := context.Background()
	start, end := time.Now().Add(-time.Hour), time.Now()

	arr := `[{"progress":0.5,"complete":false},` +
		`{"progress":1,"complete":true,"filteredAlertsCount":1,` +
		`"alerts":{"alerts":[{"id":"de_a","createdTime":"2026-01-02T03:04:05Z"}]}}]`
	snap, err := alertTestClient(t, arr).GetAlerts(ctx, start, end, 10, "", "", nil)
	if err != nil {
		t.Fatalf("array: %v", err)
	}
	if !snap.Complete || len(snap.Alerts) != 1 || snap.Alerts[0].ID != "de_a" {
		t.Errorf("array decode wrong: complete=%v alerts=%d", snap.Complete, len(snap.Alerts))
	}

	obj := `{"complete":true,"filteredAlertsCount":1,` +
		`"alerts":{"alerts":[{"id":"de_b","createTime":"2026-01-02T03:04:05Z"}]}}`
	snap2, err := alertTestClient(t, obj).GetAlerts(ctx, start, end, 10, "", "", nil)
	if err != nil {
		t.Fatalf("object: %v", err)
	}
	if !snap2.Complete || len(snap2.Alerts) != 1 || snap2.Alerts[0].ID != "de_b" {
		t.Errorf("object decode wrong: complete=%v alerts=%d", snap2.Complete, len(snap2.Alerts))
	}
}
