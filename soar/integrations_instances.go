package soar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"danny.vn/secops/soar/internal/transport"
)

// IntegrationInstance is a configured instance of an integration in a specific
// environment — the runtime card (credentials, parameters, enabled/disabled).
type IntegrationInstance struct {
	Name            string          `json:"name"`
	Identifier      string          `json:"identifier"`
	DisplayName     string          `json:"displayName"`
	IntegrationName string          `json:"integrationName"`
	Environment     string          `json:"environment"`
	IsEnabled       bool            `json:"isEnabled"`
	Raw             json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields while preserving the complete server
// object in Raw.
func (i *IntegrationInstance) UnmarshalJSON(data []byte) error {
	type alias IntegrationInstance
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*i = IntegrationInstance(a)
	i.Raw = append(json.RawMessage(nil), data...)
	return nil
}

type instancesList struct {
	Items         []IntegrationInstance `json:"integrationInstances"`
	NextPageToken string                `json:"nextPageToken"`
}

// ListAllIntegrationInstances returns every integration instance across ALL
// integrations and environments via the "-" wildcard parent. This is the
// fleet-wide instance inventory (the console's Settings → Integrations grid).
func (c *Client) ListAllIntegrationInstances(ctx context.Context) ([]IntegrationInstance, error) {
	return c.listIntegrationInstances(ctx, "-", "")
}

// ListIntegrationInstances returns the integration instances for one specific
// integration, optionally filtered to a single environment.
func (c *Client) ListIntegrationInstances(ctx context.Context, integration, environment string) ([]IntegrationInstance, error) {
	return c.listIntegrationInstances(ctx, integration, environment)
}

func (c *Client) listIntegrationInstances(ctx context.Context, integration, environment string) ([]IntegrationInstance, error) {
	var all []IntegrationInstance
	res := fmt.Sprintf("integrations/%s/integrationInstances", integration)
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		if environment != "" {
			q.Set("filter", fmt.Sprintf("environment = '%s'", environment))
		}
		var page instancesList
		if err := c.t.V1Alpha(ctx, "GET", res, nil, &page, transport.Query(q)); err != nil {
			return "", err
		}
		all = append(all, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// ListIntegrationsFiltered returns integrations matching a server-side filter
// expression (e.g. "(internal != true) and (type = 'RESPONSE')").
func (c *Client) ListIntegrationsFiltered(ctx context.Context, filter string) ([]Integration, error) {
	var all []Integration
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if filter != "" {
			q.Set("filter", filter)
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var page integrationsList
		if err := c.t.V1Alpha(ctx, "GET", "integrations", nil, &page, transport.Query(q)); err != nil {
			return "", err
		}
		all = append(all, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// ExecuteConnectorTest runs the test script for a connector instance. Returns
// the raw test result from the server.
func (c *Client) ExecuteConnectorTest(ctx context.Context, integration, connectorID, instanceID string) (json.RawMessage, error) {
	var out json.RawMessage
	res := fmt.Sprintf("integrations/%s/connectors/%s/connectorInstances/%s:executeTest", integration, connectorID, instanceID)
	if err := c.t.V1Alpha(ctx, "POST", res, struct{}{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ExecuteJobTest runs the test script for a job instance. Returns the raw test
// result from the server.
func (c *Client) ExecuteJobTest(ctx context.Context, integration, jobID, instanceID string) (json.RawMessage, error) {
	var out json.RawMessage
	res := fmt.Sprintf("integrations/%s/jobs/%s/jobInstances/%s:executeTest", integration, jobID, instanceID)
	if err := c.t.V1Alpha(ctx, "POST", res, struct{}{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
