package soar_test

import (
	"strings"
	"testing"
)

// parseNameSegments extracts integration, jobID, and instanceID from a v1alpha
// job instance resource name. The expected suffix pattern is
// integrations/{integration}/jobs/{job}/jobInstances/{instance}.
func parseNameSegments(name string) (integration, jobID, instanceID string, ok bool) {
	parts := strings.Split(name, "/")
	// Walk the segments looking for the integrations/…/jobs/…/jobInstances/… triple.
	for i := range len(parts) - 5 {
		if parts[i] == "integrations" && parts[i+2] == "jobs" && parts[i+4] == "jobInstances" {
			return parts[i+1], parts[i+3], parts[i+5], true
		}
	}
	return "", "", "", false
}

// TestLiveListAllJobInstances verifies ListAllJobInstances returns typed
// instances with basic fields populated. Read-only.
func TestLiveListAllJobInstances(t *testing.T) {
	c, ctx := liveClient(t)
	instances, err := c.ListAllJobInstances(ctx)
	if err != nil {
		t.Fatalf("ListAllJobInstances: %v", err)
	}
	t.Logf("got %d job instances", len(instances))
	if len(instances) == 0 {
		t.Skip("no job instances configured")
	}
	first := instances[0]
	if first.Name == "" {
		t.Error("first instance has empty Name")
	}
	if first.Integration == "" {
		t.Error("first instance has empty Integration")
	}
}

// TestLiveGetJobInstance round-trips a ListAllJobInstances → GetJobInstance
// call to verify the GET path returns a typed record. Read-only.
func TestLiveGetJobInstance(t *testing.T) {
	c, ctx := liveClient(t)
	instances, err := c.ListAllJobInstances(ctx)
	if err != nil {
		t.Fatalf("ListAllJobInstances: %v", err)
	}
	if len(instances) == 0 {
		t.Skip("no job instances")
	}
	first := instances[0]
	integration, jobID, instanceID, ok := parseNameSegments(first.Name)
	if !ok {
		t.Skipf("cannot parse resource name %q", first.Name)
	}
	got, err := c.GetJobInstance(ctx, integration, jobID, instanceID)
	if err != nil {
		t.Fatalf("GetJobInstance(%s/%s/%s): %v", integration, jobID, instanceID, err)
	}
	if got.Name != first.Name {
		t.Errorf("round-trip mismatch: list %q vs get %q", first.Name, got.Name)
	}
	if got.Integration != first.Integration {
		t.Errorf("integration mismatch: list %q vs get %q", first.Integration, got.Integration)
	}
}

// TestLiveListJobInstanceLogs verifies ListJobInstanceLogs returns log entries
// for a job instance that has run at least once. Read-only.
func TestLiveListJobInstanceLogs(t *testing.T) {
	c, ctx := liveClient(t)
	instances, err := c.ListAllJobInstances(ctx)
	if err != nil {
		t.Fatalf("ListAllJobInstances: %v", err)
	}
	if len(instances) == 0 {
		t.Skip("no job instances")
	}
	// Find an instance that has run (lastRunStatus is set).
	for _, inst := range instances {
		if inst.LastRunStatus == "" {
			continue
		}
		integration, jobID, instanceID, ok := parseNameSegments(inst.Name)
		if !ok {
			continue
		}
		logs, _, _, err := c.ListJobInstanceLogs(ctx, integration, jobID, instanceID, 5, "")
		if err != nil {
			t.Fatalf("ListJobInstanceLogs(%s/%s/%s): %v", integration, jobID, instanceID, err)
		}
		t.Logf("got %d logs for %s (status=%s)", len(logs), inst.DisplayName, inst.LastRunStatus)
		return
	}
	t.Skip("no job instance with run history found")
}
