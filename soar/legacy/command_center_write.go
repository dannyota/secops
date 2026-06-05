// LEGACY tier: the Siemplify external API (/api/external/v1) CommandCenter
// surface — write half (see command_center.go for the package overview and the
// read surface).
//
// These are the War Room mutations: creating and closing incidents and recording
// the artifacts that document a live response (assessments, comments, decisions,
// facts, tasks, severity scores, chat messages). Each takes a freeform legacy
// body and returns json.RawMessage. All methods speak the AppKey-authenticated
// external API via c.t.External.
package legacy

import "context"

// CommandCenterCreateIncident creates a War Room incident. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterCreateIncident(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/CreateIncident", body)
}

// CommandCenterCloseIncident closes a War Room incident. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterCloseIncident(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/CloseIncident", body)
}

// CommandCenterCreateWarRoomAssessment creates a War Room assessment. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterCreateWarRoomAssessment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/CreateWarRoomAssessment", body)
}

// CommandCenterUpdateWarRoomAssessment updates a War Room assessment. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterUpdateWarRoomAssessment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/UpdateWarRoomAssessment", body)
}

// CommandCenterCreateWarRoomComment adds a comment to the War Room. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterCreateWarRoomComment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/CreateWarRoomComment", body)
}

// CommandCenterUpdateWarRoomComment updates a War Room comment. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterUpdateWarRoomComment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/UpdateWarRoomComment", body)
}

// CommandCenterCreateWarRoomDecision records a War Room decision. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterCreateWarRoomDecision(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/CreateWarRoomDecision", body)
}

// CommandCenterUpdateWarRoomDecision updates a War Room decision. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterUpdateWarRoomDecision(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/UpdateWarRoomDecision", body)
}

// CommandCenterCreateWarRoomFact records a War Room fact. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterCreateWarRoomFact(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/CreateWarRoomFact", body)
}

// CommandCenterUpdateWarRoomFact updates a War Room fact. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterUpdateWarRoomFact(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/UpdateWarRoomFact", body)
}

// CommandCenterCreateWarRoomTask creates a War Room task. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterCreateWarRoomTask(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/CreateWarRoomTask", body)
}

// CommandCenterUpdateWarRoomTask updates a War Room task. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterUpdateWarRoomTask(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/UpdateWarRoomTask", body)
}

// CommandCenterSaveSeverityScore saves an incident severity score. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterSaveSeverityScore(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/SaveSeverityScore", body)
}

// CommandCenterSendChatMessage sends a War Room chat message. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) CommandCenterSendChatMessage(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/warroom/SendChatMessage", body)
}
