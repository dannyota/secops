package legacy

import "testing"

// TestLiveCommandCenterReads exercises the War Room (CommandCenter) read
// endpoints that are safe on a tenant with no prior setup: the zero-argument
// GETs that list global War Room config and settings. Every other CommandCenter
// read needs a live incident id or a freeform request body, so they are omitted.
// Runs under SECOPS_SOAR_SMOKE=1.
func TestLiveCommandCenterReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "warroom/GetDepartments", func() (RawJSON, error) { return lc.CommandCenterGetDepartments(ctx) })
	readProbe(t, "warroom/GetWarRoomAuditors", func() (RawJSON, error) { return lc.CommandCenterGetWarRoomAuditors(ctx) })
	// GetForgotPasswordTimeLimit omitted: HTTP 403 (permission-gated for the AppKey).
}

// No CRUD test: the CommandCenter (War Room) surface is incident-collaboration
// runtime data — incidents, chat messages, facts, decisions, assessments, tasks,
// severity scores — all of which require a live incident and are operational
// rather than cosmetic config. There is no safe throwaway config resource here,
// so this tag is reads only.
