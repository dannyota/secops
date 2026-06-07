package soar_test

import (
	"errors"
	"testing"

	"danny.vn/secops/soar/internal/transport"
)

// statusOf reports the HTTP status behind a SOAR transport error, or 0 if the
// error is not a transport error (i.e. a genuine decode/usage bug).
func statusOf(err error) int {
	if e, ok := errors.AsType[*transport.Error](err); ok {
		return e.Status
	}
	return 0
}

// TestLiveWave15LifecycleRead validates the READ paths of the modern SOAR
// v1alpha lifecycle surfaces whose writes Wave 15 completes: alert-grouping
// rules, connector instances, and job instances. Read-only.
//
// These ride the flaky v1alpha SOAR plane, so a server-side 5xx is logged (a
// reliability signal, not a test failure); only a non-transport error — a
// decode/shape bug, which is what this test guards — fails. Gated on
// SECOPS_SOAR_SMOKE=1.
func TestLiveWave15LifecycleRead(t *testing.T) {
	c, ctx := liveClient(t)

	// 1. alertGroupingRules: list, then GET one back (round-trip).
	rules, err := c.ListAlertGroupingRules(ctx)
	switch {
	case err == nil:
		t.Logf("OK alertGroupingRules: %d", len(rules))
		if len(rules) > 0 && rules[0].ID != "" {
			if got, err := c.GetAlertGroupingRule(ctx, rules[0].ID); err != nil {
				if s := statusOf(err); s != 0 {
					t.Logf("GetAlertGroupingRule: HTTP %d (flaky backend)", s)
				} else {
					t.Errorf("GetAlertGroupingRule decode bug: %v", err)
				}
			} else {
				t.Logf("OK GetAlertGroupingRule: id=%s", got.ID)
			}
		}
	case statusOf(err) != 0:
		t.Logf("ListAlertGroupingRules: HTTP %d (flaky backend)", statusOf(err))
	default:
		t.Errorf("ListAlertGroupingRules decode bug: %v", err)
	}

	// 2. connectorInstances: walk integrations→connectors→instances, GET one.
	ints, err := c.ListIntegrations(ctx)
	if err != nil {
		if s := statusOf(err); s != 0 {
			t.Logf("ListIntegrations: HTTP %d (flaky backend) — skipping instance reads", s)
			return
		}
		t.Fatalf("ListIntegrations decode bug: %v", err)
	}

	connDone, jobDone := false, false
	for _, in := range ints {
		if !connDone {
			if defs, err := c.ListConnectors(ctx, in.Identifier); err == nil {
				for _, d := range defs {
					insts, err := c.ListConnectorInstances(ctx, in.Identifier, d.PathID())
					if err != nil {
						if s := statusOf(err); s == 0 {
							t.Errorf("ListConnectorInstances decode bug: %v", err)
						}
						continue
					}
					t.Logf("OK connectorInstances %s/%s: %d", in.Identifier, d.PathID(), len(insts))
					if len(insts) > 0 {
						id := lastPathSeg(insts[0].Name)
						if _, err := c.GetConnectorInstance(ctx, in.Identifier, d.PathID(), id); err != nil {
							if s := statusOf(err); s == 0 {
								t.Errorf("GetConnectorInstance decode bug: %v", err)
							} else {
								t.Logf("GetConnectorInstance: HTTP %d (flaky backend)", s)
							}
						} else {
							t.Logf("OK GetConnectorInstance: %s", id)
						}
						connDone = true
						break
					}
				}
			}
		}
		if !jobDone {
			if jobs, err := c.ListJobs(ctx, in.Identifier); err == nil {
				for _, j := range jobs {
					insts, err := c.ListJobInstances(ctx, in.Identifier, j.PathID())
					if err != nil {
						if s := statusOf(err); s == 0 {
							t.Errorf("ListJobInstances decode bug: %v", err)
						}
						continue
					}
					t.Logf("OK jobInstances %s/%s: %d", in.Identifier, j.PathID(), len(insts))
					jobDone = true
					break
				}
			}
		}
		if connDone && jobDone {
			break
		}
	}
	if !connDone {
		t.Log("no connector instance available to exercise GET (none configured)")
	}
	if !jobDone {
		t.Log("no job instance list exercised (no jobs found)")
	}
}

// lastPathSeg returns the final '/'-separated segment of a resource name.
func lastPathSeg(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			return name[i+1:]
		}
	}
	return name
}
