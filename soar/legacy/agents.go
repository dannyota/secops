// LEGACY tier: the Siemplify external API (/api/external/v1) Agents surface.
//
// Agents are the remote execution nodes (and their publishers) that run
// integrations, connectors, and jobs in a customer's own network. These
// endpoints enumerate, configure, and deploy agents and the publishers that
// distribute to them. This is the reliable external-API path for SOAR agents.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"net/url"
)

// AgentTestPublisherConnectivity checks connectivity to one publisher by id.
func (c *Client) AgentTestPublisherConnectivity(ctx context.Context, publisherID string) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/TestPublisherConnectivity/"+url.PathEscape(publisherID))
}

// AgentListPublishers returns all configured publishers.
func (c *Client) AgentListPublishers(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/GetPublishers")
}

// AgentGetPublisher returns one publisher by id.
func (c *Client) AgentGetPublisher(ctx context.Context, publisherID string) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/GetPublisherById/"+url.PathEscape(publisherID))
}

// AgentAddOrUpdatePublisher creates a publisher or updates an existing one. body
// is the freeform publisher payload. LIVE MUTATION.
func (c *Client) AgentAddOrUpdatePublisher(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/agents/AddOrUpdatePublisher", body)
}

// AgentDeletePublishers deletes the publishers identified by body. LIVE MUTATION;
// this cannot be undone.
func (c *Client) AgentDeletePublishers(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/agents/DeletePublishers", body)
}

// AgentList returns all agents.
func (c *Client) AgentList(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/GetAgents")
}

// AgentGetSecondaryByPrimaryIdentifiers returns secondary agents for the primary
// agent identifiers in body.
func (c *Client) AgentGetSecondaryByPrimaryIdentifiers(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/agents/GetSecondaryAgentsByPrimaryIdentifiers", body)
}

// AgentListEnabled returns only the enabled agents.
func (c *Client) AgentListEnabled(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/GetEnabledAgents")
}

// AgentListValidForConnector returns agents valid to run a connector in the given
// environment and integration.
func (c *Client) AgentListValidForConnector(ctx context.Context, environment, integration string) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/GetValidAgentsForConnector/"+url.PathEscape(environment)+"/"+url.PathEscape(integration))
}

// AgentListValidForJobs returns agents valid to run jobs for the given
// integration.
func (c *Client) AgentListValidForJobs(ctx context.Context, integration string) (RawJSON, error) {
	q := url.Values{}
	q.Set("integration", integration)
	return c.externalGetQuery(ctx, "/agents/valid-for-jobs", q)
}

// AgentListIntegrationUpgradeUnsupported returns agents that do not support the
// integration upgrade for the given required Python version, integration, and
// staging/production toggle. requiredPythonVersion is the enum value (e.g. "0"=
// None, "1"=V2_7, "2"=V3_7, "3"=V3_11).
func (c *Client) AgentListIntegrationUpgradeUnsupported(ctx context.Context, requiredPythonVersion, integrationIdentifier, toggleStagingProduction string) (RawJSON, error) {
	q := url.Values{}
	q.Set("requiredPythonVersion", requiredPythonVersion)
	q.Set("integrationIdentifier", integrationIdentifier)
	if toggleStagingProduction != "" {
		q.Set("toggleStagingProduction", toggleStagingProduction)
	}
	return c.externalGetQuery(ctx, "/agents/integration-upgrade-unsupported-agents", q)
}

// AgentListValidForIdeConnector returns agents valid to run an IDE connector in
// the given environment and integration.
func (c *Client) AgentListValidForIdeConnector(ctx context.Context, environment, integration string) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/GetValidAgentsForIdeConnector/"+url.PathEscape(environment)+"/"+url.PathEscape(integration))
}

// AgentListAvailableEnvironments returns the environments available for agents.
func (c *Client) AgentListAvailableEnvironments(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/GetAvailableEnvironmentsForAgents")
}

// AgentGet returns one agent by its identifier.
func (c *Client) AgentGet(ctx context.Context, agentIdentifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/GetAgentByIdentifier/"+url.PathEscape(agentIdentifier))
}

// AgentGetInformationByIntegrationInstance returns agent information for the given
// integration instance id.
func (c *Client) AgentGetInformationByIntegrationInstance(ctx context.Context, integrationInstanceID string) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/GetAgentInformationByIntegrationInstance/"+url.PathEscape(integrationInstanceID))
}

// AgentListByEnvironment returns agents for the environments specified in body.
func (c *Client) AgentListByEnvironment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/agents/GetAgentsByEnvironment", body)
}

// AgentGetInformationByIdentifier returns agent information for one agent by its
// identifier.
func (c *Client) AgentGetInformationByIdentifier(ctx context.Context, agentIdentifier string) (RawJSON, error) {
	return c.externalGet(ctx, "/agents/GetAgentInformationByIdentifier/"+url.PathEscape(agentIdentifier))
}

// AgentGetInformationByIdentifiers returns agent information for the identifiers
// in body.
func (c *Client) AgentGetInformationByIdentifiers(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/agents/GetAgentsInformationByIdentifiers", body)
}

// AgentAdd creates a new agent. body is the freeform agent payload. LIVE MUTATION.
func (c *Client) AgentAdd(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/agents/AddAgent", body)
}

// AgentUpdate updates an existing agent. body is the freeform agent payload.
// LIVE MUTATION.
func (c *Client) AgentUpdate(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/agents/UpdateAgent", body)
}

// AgentDelete deletes the agent identified by body. LIVE MUTATION; this cannot be
// undone.
func (c *Client) AgentDelete(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/agents/DeleteAgent", body)
}

// AgentRedeploy redeploys the agent identified by body. LIVE MUTATION.
func (c *Client) AgentRedeploy(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/agents/RedeployAgent", body)
}
