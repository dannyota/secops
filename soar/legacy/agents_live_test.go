package legacy

import (
	"testing"
)

// TestLiveAgentsReads exercises the zero-argument Agents read endpoints (safe;
// no prior tenant setup required). Runs under SECOPS_SOAR_SMOKE=1.
//
// Only methods with signature (ctx) (RawJSON, error) are probed here — the rest
// of the Agents surface needs an agent/publisher identifier, an
// environment/integration pair, or a POST body we cannot safely supply on a
// fresh tenant, so they are intentionally omitted.
func TestLiveAgentsReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "agents/GetAgents", func() (RawJSON, error) { return lc.AgentList(ctx) })
	readProbe(t, "agents/GetEnabledAgents", func() (RawJSON, error) { return lc.AgentListEnabled(ctx) })
	readProbe(t, "agents/GetPublishers", func() (RawJSON, error) { return lc.AgentListPublishers(ctx) })
	readProbe(t, "agents/GetAvailableEnvironmentsForAgents", func() (RawJSON, error) {
		return lc.AgentListAvailableEnvironments(ctx)
	})
}
