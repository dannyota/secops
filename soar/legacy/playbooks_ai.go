// BRIDGE tier: legacyPlaybooks AI-generation RPCs (v1alpha host, legacy op
// names) — Gemini playbook drafting. Every generate/update call CREATES OR
// EDITS playbook content on the live tenant, so callers gate them.
package legacy

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
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

// AiGenerateRequest is the legacyAiGenerate envelope: the free-text prompt
// plus the playbook the assistant works on — for create-from-description an
// EMPTY definition. The call is synchronous and returns the proposed draft
// WITHOUT persisting anything (persisting is the normal playbook save). The
// endpoint rejects a creationSource field, so the DTO omits it; servers can
// also reject API-key auth for the whole Playbook Assistant family
// ("restricted for API keys") — surface that error as-is.
type AiGenerateRequest struct {
	Prompt       string             `json:"prompt"`
	Playbook     AiGenerateDraftDTO `json:"playbook"`
	HashedUserID string             `json:"hashedUserId"`
}

// AiGenerateDraftDTO is the minimal playbook definition the assistant
// envelope carries (the designer's pre-save draft shape).
type AiGenerateDraftDTO struct {
	ID                         string   `json:"id"`
	Identifier                 string   `json:"identifier"`
	Version                    string   `json:"version"`
	IsEnabled                  bool     `json:"isEnabled"`
	IsDebugMode                bool     `json:"isDebugMode"`
	Name                       string   `json:"name"`
	Description                string   `json:"description"`
	CategoryID                 int      `json:"categoryId"`
	Creator                    string   `json:"creator"`
	Priority                   string   `json:"priority"`
	Steps                      []any    `json:"steps"`
	StepsRelations             []any    `json:"stepsRelations"`
	Environments               []string `json:"environments"`
	OriginalPlaybookIdentifier string   `json:"originalPlaybookIdentifier"`
	CreationTimeMs             string   `json:"creationTimeUnixTimeInMs"`
	ModificationTimeMs         string   `json:"modificationTimeUnixTimeInMs"`
	ModifiedBy                 string   `json:"modifiedBy"`
	TemplateName               string   `json:"templateName"`
	PlaybookType               string   `json:"playbookType"`
	CategoryName               string   `json:"categoryName"`
	DefaultAccessLevel         string   `json:"defaultAccessLevel"`
	EntityAccessLevel          string   `json:"entityAccessLevel"`
	Permissions                []any    `json:"permissions"`
	HasRestrictedEnvironments  bool     `json:"hasRestrictedEnvironments"`
	OverviewTemplates          []any    `json:"overviewTemplates"`
}

// NewAiGenerateRequest builds a create-from-description envelope: an empty
// REGULAR draft named name (id "0" = unsaved) carrying the prompt. Adjust the
// returned struct's fields (environments, category, …) before sending if the
// defaults don't fit.
func NewAiGenerateRequest(prompt, name string) (*AiGenerateRequest, error) {
	if prompt == "" {
		return nil, fmt.Errorf("legacy: a prompt is required")
	}
	if name == "" {
		name = "ai-draft"
	}
	id, err := randomUUID()
	if err != nil {
		return nil, err
	}
	now := strconv.FormatInt(time.Now().UnixMilli(), 10)
	return &AiGenerateRequest{
		Prompt: prompt,
		Playbook: AiGenerateDraftDTO{
			ID:                         "0",
			Identifier:                 id,
			Version:                    "1",
			IsEnabled:                  true,
			Name:                       name,
			CategoryID:                 1,
			Priority:                   "2",
			Steps:                      []any{},
			StepsRelations:             []any{},
			Environments:               []string{"Default Environment"},
			OriginalPlaybookIdentifier: id,
			CreationTimeMs:             now,
			ModificationTimeMs:         now,
			PlaybookType:               "REGULAR",
			CategoryName:               "Default",
			DefaultAccessLevel:         "EDIT",
			EntityAccessLevel:          "EDIT",
			Permissions:                []any{},
			OverviewTemplates:          []any{},
		},
	}, nil
}

// randomUUID mints a random (version 4) UUID from crypto/rand.
func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("legacy: mint identifier: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
