// LEGACY tier: the Siemplify external API (/api/external/v1) CommandCenter surface.
//
// CommandCenter is the SOAR "War Room": the live-incident collaboration space
// where responders coordinate around an incident — chat channels, facts,
// decisions, assessments, tasks, severity scoring, and the generated incident
// report. These endpoints all live under /warroom/ and predate the modern
// case-collaboration model; they are kept here until it covers them.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). Incident/user ids are int64 path parameters. All
// methods speak the AppKey-authenticated external API via c.t.External.
//
// This file holds the read (GET) surface; the write surface lives in
// command_center_write.go.
package legacy

import (
	"context"
	"strconv"
)

// CommandCenterGetDepartments lists the War Room departments.
func (c *Client) CommandCenterGetDepartments(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/warroom/GetDepartments")
}

// CommandCenterGetFilterDepartments lists the departments available to filter by
// for one incident.
func (c *Client) CommandCenterGetFilterDepartments(ctx context.Context, incidentID int) (RawJSON, error) {
	return c.externalGet(ctx, "/warroom/GetFilterDepartments/"+strconv.Itoa(incidentID))
}

// CommandCenterGetForgotPasswordTimeLimit returns the War Room forgot-password
// time limit setting.
func (c *Client) CommandCenterGetForgotPasswordTimeLimit(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/warroom/GetForgotPasswordTimeLimit")
}

// CommandCenterGetIncident returns the full details of one War Room incident.
func (c *Client) CommandCenterGetIncident(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/warroom/GetIncident/"+strconv.Itoa(id))
}

// CommandCenterGetIncidentUserByID returns one War Room incident user by id.
func (c *Client) CommandCenterGetIncidentUserByID(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/warroom/GetIncidentUserById/"+strconv.Itoa(id))
}

// CommandCenterGetLastSeverityScore returns the most recent severity score for an
// incident.
func (c *Client) CommandCenterGetLastSeverityScore(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/warroom/GetLastSeverityScore/"+strconv.Itoa(id))
}

// CommandCenterGetWarRoomAuditors lists the War Room auditors.
func (c *Client) CommandCenterGetWarRoomAuditors(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/warroom/GetWarRoomAuditors")
}

// CommandCenterGetWarRoomUserForIncident returns the War Room user record for one
// incident.
func (c *Client) CommandCenterGetWarRoomUserForIncident(ctx context.Context, incidentID int) (RawJSON, error) {
	return c.externalGet(ctx, "/warroom/GetWarRoomUserForIncident/"+strconv.Itoa(incidentID))
}

// CommandCenterGetChatChannelCards lists the chat-channel cards for one incident.
func (c *Client) CommandCenterGetChatChannelCards(ctx context.Context, incidentID int) (RawJSON, error) {
	return c.externalGet(ctx, "/warroom/GetChatChannelCards/"+strconv.Itoa(incidentID))
}

// CommandCenterCreateWarRoomReport generates the War Room report for one incident.
func (c *Client) CommandCenterCreateWarRoomReport(ctx context.Context, incidentID int) (RawJSON, error) {
	return c.externalGet(ctx, "/warroom/CreateWarRoomReport/"+strconv.Itoa(incidentID))
}

// CommandCenterGetChatChannelConversation returns the conversation for a chat
// channel. body is the freeform legacy request payload.
func (c *Client) CommandCenterGetChatChannelConversation(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/GetChatChannelConversation", body)
}

// CommandCenterGetWarRoomWallItems returns the War Room wall items. body is the
// freeform legacy request payload.
func (c *Client) CommandCenterGetWarRoomWallItems(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/GetWarRoomWallItems", body)
}
