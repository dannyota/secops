// BRIDGE tier: legacyPlaybooks AI-generation RPCs (v1alpha host, legacy op
// names) — Gemini playbook drafting. Every generate/update call CREATES OR
// EDITS playbook content on the live tenant, so callers gate them.
package legacy

import (
	"context"
	"encoding/json"
)

// AiGeneratePlaybook generates a playbook draft from a description
// (legacyPlaybooks:legacyAiGenerate). body is the freeform request payload.
// LIVE MUTATION (creates a draft).
func (c *Client) AiGeneratePlaybook(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyAiGenerate", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AiGeneratePlaybookByAlert generates a playbook draft FROM a specific alert
// (legacyPlaybooks:legacyAiGenerateByAlert): body carries caseId (int64 as
// string), alertId, and the optional hashedUserId / forceRefreshData /
// isFirstRequest polling fields. LIVE MUTATION (creates a draft).
func (c *Client) AiGeneratePlaybookByAlert(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyAiGenerateByAlert", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AiUpdatePlaybook revises an existing playbook with AI
// (legacyPlaybooks:legacyAiUpdate). LIVE MUTATION.
func (c *Client) AiUpdatePlaybook(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyAiUpdate", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AiGenerationStatusByAlert polls the by-alert generation status
// (legacyPlaybooks:legacyGetAiGenerationStatusByAlert). Read-only.
func (c *Client) AiGenerationStatusByAlert(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "legacyPlaybooks:legacyGetAiGenerationStatusByAlert", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
