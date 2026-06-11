// authoring.go — MODERN (v1alpha) Python action/job authoring on the SOAR
// host: the IDE's create flow as an API loop. fetchTemplate returns a new
// definition skeleton; filling it and POSTing it to the integration's
// actions/jobs collection creates the definition; DELETE by numeric id
// removes it. The same numeric ids appear in the wildcard catalogs
// (integrations/-/actions), which is how callers find what they created.
package soar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"danny.vn/secops/soar/internal/transport"
)

// templateEnvelope wraps the fetchTemplate response ({"template": {...}}).
type templateEnvelope struct {
	Template json.RawMessage `json:"template"`
}

// FetchActionTemplate returns the new-action definition skeleton for one
// integration (actions:fetchTemplate). async selects the asynchronous action
// variant (polling timeouts and an async script skeleton). Read-only.
func (c *Client) FetchActionTemplate(ctx context.Context, integration string, async bool) (json.RawMessage, error) {
	if strings.TrimSpace(integration) == "" {
		return nil, fmt.Errorf("soar: integration is required")
	}
	q := url.Values{"async": {strconv.FormatBool(async)}}
	var env templateEnvelope
	res := fmt.Sprintf("integrations/%s/actions:fetchTemplate", integration)
	if err := c.t.V1Alpha(ctx, "GET", res, nil, &env, transport.Query(q)); err != nil {
		return nil, err
	}
	return env.Template, nil
}

// FetchJobTemplate returns the new-job definition skeleton for one
// integration (jobs:fetchTemplate) — a SiemplifyJob Python scaffold.
// Read-only.
func (c *Client) FetchJobTemplate(ctx context.Context, integration string) (json.RawMessage, error) {
	if strings.TrimSpace(integration) == "" {
		return nil, fmt.Errorf("soar: integration is required")
	}
	var env templateEnvelope
	res := fmt.Sprintf("integrations/%s/jobs:fetchTemplate", integration)
	if err := c.t.V1Alpha(ctx, "GET", res, nil, &env, transport.Query(url.Values{})); err != nil {
		return nil, err
	}
	return env.Template, nil
}

// CreateActionDef creates a custom action definition inside an integration:
// POST integrations/{key}/actions with a filled template body (name "" =
// create; displayName, script, custom:true, …). body is sent verbatim so
// numeric fields survive untouched; the response is the stored definition.
// LIVE MUTATION.
func (c *Client) CreateActionDef(ctx context.Context, integration string, body json.RawMessage) (json.RawMessage, error) {
	return c.createDef(ctx, integration, "actions", body)
}

// UpdateActionDef patches fields of an existing custom action definition by its
// numeric id: PATCH integrations/{key}/actions/{id}?updateMask=<fields>. The
// body carries only the changed fields and updateMask names them (a v1alpha
// sparse update). Create is POST (CreateActionDef); update is PATCH by id — a
// POST always creates (it collides on displayName), so updates must go through
// here. LIVE MUTATION.
func (c *Client) UpdateActionDef(ctx context.Context, integration, actionID string, body json.RawMessage, fields ...string) (json.RawMessage, error) {
	return c.patchDef(ctx, integration, "actions", actionID, body, fields)
}

// DeleteActionDef deletes one custom action definition by its numeric id
// (DELETE integrations/{key}/actions/{id}; the id comes from the wildcard
// catalog or the create response). LIVE MUTATION.
func (c *Client) DeleteActionDef(ctx context.Context, integration, actionID string) error {
	return c.deleteDef(ctx, integration, "actions", actionID)
}

// CreateJobDef creates a custom job definition inside an integration (POST
// integrations/{key}/jobs, the filled jobs:fetchTemplate body). LIVE
// MUTATION.
func (c *Client) CreateJobDef(ctx context.Context, integration string, body json.RawMessage) (json.RawMessage, error) {
	return c.createDef(ctx, integration, "jobs", body)
}

// UpdateJobDef patches fields of an existing custom job definition by its
// numeric id — the jobs twin of UpdateActionDef (PATCH
// integrations/{key}/jobs/{id}?updateMask=<fields>). LIVE MUTATION.
func (c *Client) UpdateJobDef(ctx context.Context, integration, jobID string, body json.RawMessage, fields ...string) (json.RawMessage, error) {
	return c.patchDef(ctx, integration, "jobs", jobID, body, fields)
}

// DeleteJobDef deletes one custom job definition by its numeric id. LIVE
// MUTATION.
func (c *Client) DeleteJobDef(ctx context.Context, integration, jobID string) error {
	return c.deleteDef(ctx, integration, "jobs", jobID)
}

func (c *Client) createDef(ctx context.Context, integration, collection string, body json.RawMessage) (json.RawMessage, error) {
	if strings.TrimSpace(integration) == "" {
		return nil, fmt.Errorf("soar: integration is required")
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("soar: a definition body is required")
	}
	var out json.RawMessage
	res := fmt.Sprintf("integrations/%s/%s", integration, collection)
	if err := c.t.V1Alpha(ctx, "POST", res, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// patchDef applies a sparse v1alpha PATCH to one definition by numeric id.
func (c *Client) patchDef(ctx context.Context, integration, collection, id string, body json.RawMessage, fields []string) (json.RawMessage, error) {
	if strings.TrimSpace(integration) == "" || strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("soar: integration and id are required")
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("soar: a definition body is required")
	}
	res := fmt.Sprintf("integrations/%s/%s/%s", integration, collection, id)
	opts := []transport.Option{}
	if len(fields) > 0 {
		opts = append(opts, transport.UpdateMask(fields...))
	}
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "PATCH", res, body, &out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) deleteDef(ctx context.Context, integration, collection, id string) error {
	if strings.TrimSpace(integration) == "" || strings.TrimSpace(id) == "" {
		return fmt.Errorf("soar: integration and id are required")
	}
	res := fmt.Sprintf("integrations/%s/%s/%s", integration, collection, id)
	return c.t.V1Alpha(ctx, "DELETE", res, nil, nil)
}
