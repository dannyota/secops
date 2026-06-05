package legacy

import (
	"testing"
)

// TestLiveJobsReads exercises the read-only jobs endpoints (safe). It runs under
// SECOPS_SOAR_SMOKE=1 and only calls zero-argument list endpoints that succeed
// on a tenant with no prior setup.
func TestLiveJobsReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "jobs/GetInstalledJobs", func() (RawJSON, error) { return lc.ListInstalledJobs(ctx) })
	readProbe(t, "jobs/GetJobTemplates", func() (RawJSON, error) { return lc.ListJobTemplates(ctx) })
	readProbe(t, "jobs/instances", func() (RawJSON, error) { return lc.ListJobInstances(ctx) })
}

// GROUP E (operational config) — jobs. A job is scheduled background automation,
// so a throwaway one is kept inert by creating it DISABLED (isEnabled:false) with
// a long interval: a disabled job is never scheduled and runs nothing. The create
// body is cloned from a real job TEMPLATE so it carries a valid jobDefinitionId,
// integration, script and parameter set; only the name/enabled/interval change.

// TestLiveJobCRUD runs the full create -> list -> read -> edit -> read -> delete
// -> list lifecycle on a throwaway DISABLED job. Write-gated; deletes the job on
// cleanup even on failure.
func TestLiveJobCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	requireWrite(t)

	raw, err := lc.ListJobTemplates(ctx)
	templates := objects(t, "job-templates", raw, err)
	if len(templates) == 0 {
		t.Skip("no job templates to clone as a create body")
	}

	// Prefer the template with the fewest parameters (simplest, valid create body).
	src := templates[0]
	for _, tpl := range templates {
		if lenField("parameters")(tpl) < lenField("parameters")(src) {
			src = tpl
		}
	}

	// Build a MINIMAL create body: only the definition-identity + scheduling
	// fields the save endpoint needs. Echoing the template's read-only/audit
	// fields (id, version, creationTime, lastRun*) makes the server 500.
	label := smokeLabel("job")
	tmpl := map[string]any{
		"jobDefinitionId":      src["jobDefinitionId"],
		"jobDefinitionName":    src["jobDefinitionName"],
		"integration":          src["integration"],
		"script":               src["script"],
		"description":          "secopsctl smoke test (disabled)",
		"parameters":           src["parameters"],
		"name":                 label,
		"isCustom":             true,
		"isEnabled":            false, // never scheduled
		"runIntervalInSeconds": 86400,
	}

	find := func(name string) map[string]any {
		raw, err := lc.ListInstalledJobs(ctx)
		return findBy(objects(t, "installed-jobs", raw, err), func(o map[string]any) bool {
			return strField("name")(o) == name
		})
	}

	// 1. baseline — absent.
	if find(label) != nil {
		t.Fatalf("smoke job %q unexpectedly already exists", label)
	}

	// 2. create (disabled).
	if _, err := lc.SaveOrUpdateJob(ctx, tmpl); err != nil {
		t.Fatalf("create job: %v", err)
	}

	// 3. list -> capture the server object; register cleanup immediately.
	created := find(label)
	if created == nil {
		t.Fatalf("created job %q not found after create", label)
	}
	cleanup := cloneObj(created)
	done := false
	t.Cleanup(func() {
		if done {
			return
		}
		if _, err := lc.DeleteJobData(ctx, cleanup); err != nil {
			t.Logf("cleanup: could not delete throwaway job %q: %v", label, err)
		}
	})
	if en, _ := created["isEnabled"].(bool); en {
		t.Fatalf("created job is enabled; expected disabled (inert)")
	}

	// 4. read — the job carries a server uniqueIdentifier.
	if strField("uniqueIdentifier")(created) == "" {
		t.Fatalf("read#1: created job has no uniqueIdentifier")
	}

	// 5. edit — rename, still disabled.
	edited := cloneObj(created)
	edited["name"] = label + "-edited"
	edited["isEnabled"] = false
	if _, err := lc.SaveOrUpdateJob(ctx, edited); err != nil {
		t.Fatalf("update job: %v", err)
	}
	cleanup = cloneObj(edited)

	// 6. read -> verify the edit.
	if find(label+"-edited") == nil {
		t.Fatalf("read#2: edit not reflected for job %q", label)
	}

	// 7. delete.
	if _, err := lc.DeleteJobData(ctx, edited); err != nil {
		t.Fatalf("delete job: %v", err)
	}
	done = true

	// 8. list -> gone.
	if find(label+"-edited") != nil {
		t.Fatalf("job %q still present after delete", label+"-edited")
	}
}
