package mirror

import (
	"context"
	"net/http"
	"testing"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// fakeFailRT fails every request, so a test can prove a code path short-circuits
// before any HTTP call.
type fakeFailRT struct{ t *testing.T }

func (r fakeFailRT) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Fatal("unexpected HTTP call — the guard should short-circuit before the wire")
	return nil, nil
}

func metricsTestClient(t *testing.T) *chronicle.Client {
	t.Helper()
	c, err := chronicle.NewClient(
		chronicle.Settings{ProjectID: "p", ProjectNumber: "0", Region: "r", CustomerID: "c"},
		auth.OAuth(), chronicle.WithHTTPClient(&http.Client{Transport: fakeFailRT{t}}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestMetricStateDefault: a blank state canonicalizes as ENABLED (the server's
// create default), so a pulled metric and a state-omitting local file match.
func TestMetricStateDefault(t *testing.T) {
	if got := metricState(""); got != "ENABLED" {
		t.Errorf("metricState(\"\") = %q, want ENABLED", got)
	}
	if got := metricState("disabled"); got != "DISABLED" {
		t.Errorf("metricState(disabled) = %q, want DISABLED (upper-cased)", got)
	}
}

// TestMetricObjectRoundTrips: a live metric builds a canonical that a state-omitting
// on-disk file (default ENABLED) reproduces, so a fresh pull diffs as Unchanged.
func TestMetricObjectRoundTrips(t *testing.T) {
	live, err := metricObject(chronicle.MetricDefinition{
		Name:           "projects/p/locations/r/instances/c/metricDefinitions/my_metric",
		State:          chronicle.MetricEnabled,
		TextDefinition: "rule m {\n  meta:\n  events:\n  outcome:\n  condition:\n}",
		Description:    "derived — must not enter the diff basis",
		Author:         "someone@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if live.Slug != "my_metric" {
		t.Errorf("slug = %q, want my_metric", live.Slug)
	}
	// An on-disk file omitting state (defaults ENABLED) must canonicalize equal.
	disk, err := canonicalMetric(metricSpec{
		DisplayName:    "my_metric",
		State:          metricState(""),
		TextDefinition: "rule m {\n  meta:\n  events:\n  outcome:\n  condition:\n}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(live.Canonical) != string(disk) {
		t.Errorf("canonical mismatch:\n live=%s\n disk=%s", live.Canonical, disk)
	}
}

// TestMetricUpdateRefusesTextChange: textDefinition is immutable, so Update must
// refuse a text edit (before any HTTP call) rather than silently no-op it.
func TestMetricUpdateRefusesTextChange(t *testing.T) {
	s := metricDefinitionsSurface(metricsTestClient(t))
	local := mustCanonMetric(t, metricSpec{DisplayName: "m", State: "ENABLED", TextDefinition: "NEW text"})
	live := mustCanonMetric(t, metricSpec{DisplayName: "m", State: "ENABLED", TextDefinition: "OLD text"})
	if _, err := s.Update(context.Background(), local, live); err == nil {
		t.Fatal("Update should refuse a textDefinition change (immutable)")
	}
}

func mustCanonMetric(t *testing.T, spec metricSpec) reconcile.Object {
	t.Helper()
	canon, err := canonicalMetric(spec)
	if err != nil {
		t.Fatal(err)
	}
	return reconcile.Object{Slug: Slugify(spec.DisplayName), ServerID: spec.DisplayName, Canonical: canon}
}
