package soar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"danny.vn/secops/auth"
	"danny.vn/secops/config"
	"danny.vn/secops/soar/internal/transport"
)

// TestProbeSOARHostVersions checks which API version (v1>v1beta>v1alpha) the SOAR
// host (siemplify-soar.com) serves for each surface. Discovery aid (logs only),
// not a regression test. Read-only.
func TestProbeSOARHostVersions(t *testing.T) {
	if os.Getenv("SECOPS_SOAR_SMOKE") != "1" {
		t.Skip("set SECOPS_SOAR_SMOKE=1")
	}
	inst, _ := config.Load("")
	key := inst.SOARAppKey
	if key == "" {
		key = auth.FromEnv("SECOPS_SOAR_APP_KEY", "SECOPS_API_KEY")
	}
	c, _ := NewClient(Settings{
		BaseURL: inst.SOARURL, ProjectNumber: inst.ProjectNumberString(),
		Region: inst.Region, CustomerID: inst.CustomerID, ForceIPv4: inst.ForceIPv4,
	}, auth.SOARAppKey(key))
	ctx := context.Background()
	versions := []string{"v1", "v1beta", "v1alpha"}
	for _, res := range []string{"cases", "alertGroupingRules", "marketplaceIntegrations", "integrations", "environments", "socRoles"} {
		parts := make([]string, 0, len(versions)+1)
		parts = append(parts, fmt.Sprintf("%-24s", res))
		for _, v := range versions {
			var raw json.RawMessage
			err := c.t.V1Alpha(ctx, "GET", res, nil, &raw, transport.Version(v), transport.Query(nil))
			tag := "ok"
			if err != nil {
				if e, ok := errors.AsType[*transport.Error](err); ok {
					tag = fmt.Sprintf("%d", e.Status)
				} else {
					tag = "err"
				}
			}
			parts = append(parts, fmt.Sprintf("%s=%s", v, tag))
		}
		t.Log(strings.Join(parts, "  "))
	}
}
