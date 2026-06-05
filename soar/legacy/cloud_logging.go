// LEGACY tier: the Siemplify external API (/api/external/v1) Cloud Logging
// surface. These endpoints expose execution logs: the structured logs of Python
// executions (integrations, connectors, actions, jobs) and the downloadable
// logs of remote agents.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"net/url"
	"strconv"
)

// CloudLoggingGetPythonLogs returns the logs of Python executions (integrations,
// connectors, actions, jobs). body is the freeform legacy filter/paging payload.
func (c *Client) CloudLoggingGetPythonLogs(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/logging/python", body)
}

// CloudLoggingDownloadAgentLogs downloads the logs of one remote agent by its
// identifier. hoursBack (when > 0) limits the results to the given timeframe.
func (c *Client) CloudLoggingDownloadAgentLogs(ctx context.Context, agentIdentifier string, hoursBack int) (RawJSON, error) {
	path := "/logging/agents/" + url.PathEscape(agentIdentifier)
	if hoursBack > 0 {
		q := url.Values{}
		q.Set("hoursBack", strconv.Itoa(hoursBack))
		return c.externalGetQuery(ctx, path, q)
	}
	return c.externalGet(ctx, path)
}
