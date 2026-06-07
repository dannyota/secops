package chronicle_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/config"
)

// TestProbeVersions reports which API version (v1 > v1beta > v1alpha) answers for
// each SIEM surface, to drive the per-surface version pins. This is a discovery
// aid (logs only), not a regression test — re-run it when auditing versions.
// Read-only.
func TestProbeVersions(t *testing.T) {
	liveChronicle(t) // gate: skips unless SECOPS_SIEM_SMOKE=1 (+ ADC)
	inst, _ := config.Load("")
	mk := func(ver string) *chronicle.Client {
		s := inst.Settings()
		s.BaseURL = fmt.Sprintf("https://%s-chronicle.googleapis.com/%s", inst.Region, ver)
		cl, _ := chronicle.NewClient(s, auth.OAuth(auth.WithForceIPv4(inst.ForceIPv4)))
		return cl
	}
	versions := []string{"v1", "v1beta", "v1alpha"}
	clients := map[string]*chronicle.Client{}
	for _, v := range versions {
		clients[v] = mk(v)
	}
	ctx := t.Context()

	probe := func(name string, call func(*chronicle.Client) error) {
		parts := make([]string, 0, len(versions)+1)
		parts = append(parts, name)
		for _, v := range versions {
			err := call(clients[v])
			tag := "ok"
			if err != nil {
				if e, ok := errors.AsType[*chronicle.APIError](err); ok {
					tag = fmt.Sprintf("%d", e.Status)
				} else {
					tag = "err"
				}
			}
			parts = append(parts, fmt.Sprintf("%s=%s", v, tag))
		}
		t.Log(strings.Join(parts, "  "))
	}

	probe("threatCollections", func(c *chronicle.Client) error {
		_, e := c.ListThreatCollections(ctx, chronicle.ThreatCollectionQuery{PageSize: 1, MaxPages: 1})
		return e
	})
	probe("curatedRules", func(c *chronicle.Client) error { _, e := c.ListCuratedRules(ctx); return e })
	probe("riskConfig", func(c *chronicle.Client) error { _, e := c.GetRiskConfig(ctx); return e })
	probe("forwarders", func(c *chronicle.Client) error { _, e := c.ListForwarders(ctx); return e })
	probe("dataAccessLabels", func(c *chronicle.Client) error { _, e := c.ListDataAccessLabels(ctx); return e })
	probe("iocs:find", func(c *chronicle.Client) error {
		_, e := c.FindIoCs(ctx, chronicle.FieldAndValue{Value: "example.com", ValueType: chronicle.IoCValueDomain})
		return e
	})
}
