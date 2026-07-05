package soar_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveJobRevisionRoundTrip validates the full revision lifecycle:
// list integrations → find one with jobs → create a throwaway revision →
// verify it appears in the list → delete it → verify it's gone.
// Write-smoke gated on SECOPS_SOAR_WRITE_SMOKE=1.
func TestLiveJobRevisionRoundTrip(t *testing.T) {
	if os.Getenv("SECOPS_SOAR_WRITE_SMOKE") != "1" {
		t.Skip("write smoke — set SECOPS_SOAR_WRITE_SMOKE=1 (and SECOPS_SOAR_SMOKE=1) to run")
	}
	c, ctx := liveClient(t)

	// 1. Find an integration that has at least one job definition.
	ints, err := c.ListIntegrations(ctx)
	if err != nil {
		t.Fatalf("ListIntegrations: %v", err)
	}
	var integration, jobID string
	for _, in := range ints {
		if !in.Custom {
			continue
		}
		jobs, err := c.ListJobs(ctx, in.Identifier)
		if err != nil || len(jobs) == 0 {
			continue
		}
		integration = in.Identifier
		jobID = jobs[0].PathID()
		break
	}
	if integration == "" || jobID == "" {
		t.Skip("no integration with jobs found")
	}

	// 2. Get the current job definition to use as the revision snapshot.
	jobDef, err := c.GetJobDef(ctx, integration, jobID)
	if err != nil {
		t.Fatalf("GetJobDef(%s/%s): %v", integration, jobID, err)
	}

	// 3. Create a throwaway revision with a unique self-identifying comment.
	comment := fmt.Sprintf("secopsctl-smoke-%d", time.Now().UnixNano())
	rev, err := c.CreateJobRevision(ctx, integration, jobID, map[string]any{
		"job":     jobDef.Raw,
		"comment": comment,
	})
	if err != nil {
		t.Fatalf("CreateJobRevision: %v", err)
	}
	t.Logf("created revision: name=%s comment=%s", rev.Name, rev.Comment)

	// Extract the revision ID from the name for cleanup.
	_, _, revisionID, revOK := parseRevisionNameSegments(rev.Name)
	if !revOK {
		t.Fatalf("cannot parse revision name %q for cleanup", rev.Name)
	}

	// Safety-net cleanup: delete even if assertions below fail.
	t.Cleanup(func() {
		if err := c.DeleteJobRevision(ctx, integration, jobID, revisionID); err != nil {
			t.Errorf("cleanup: delete revision %s: %v — remove manually", revisionID, err)
		}
	})

	// 4. Verify the revision appears in the list.
	revisions, err := c.ListJobRevisions(ctx, integration, jobID)
	if err != nil {
		t.Fatalf("ListJobRevisions: %v", err)
	}
	found := false
	for _, r := range revisions {
		if r.Comment == comment {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created revision with comment %q not found in list of %d revisions", comment, len(revisions))
	}

	// 5. Delete the revision (the Cleanup call will be a harmless no-op or
	// a second delete that errors cleanly if the revision is already gone).
	if err := c.DeleteJobRevision(ctx, integration, jobID, revisionID); err != nil {
		t.Fatalf("DeleteJobRevision: %v", err)
	}

	// 6. Verify it's gone.
	revisions, err = c.ListJobRevisions(ctx, integration, jobID)
	if err != nil {
		t.Fatalf("ListJobRevisions after delete: %v", err)
	}
	for _, r := range revisions {
		if r.Comment == comment {
			t.Errorf("revision with comment %q still present after delete", comment)
		}
	}
	t.Logf("OK revision round-trip: created %s, verified in list, deleted, verified gone", revisionID)
}

// parseRevisionNameSegments extracts integration, jobID, revisionID from a
// resource name containing .../jobs/{job}/revisions/{revision}.
func parseRevisionNameSegments(name string) (integration, jobID, revisionID string, ok bool) {
	parts := strings.Split(name, "/")
	for i := range len(parts) - 5 {
		if parts[i] == "integrations" && parts[i+2] == "jobs" && parts[i+4] == "revisions" {
			return parts[i+1], parts[i+3], parts[i+5], true
		}
	}
	return "", "", "", false
}
