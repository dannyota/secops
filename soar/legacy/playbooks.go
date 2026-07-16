// BRIDGE tier: SOAR playbooks on the SOAR-host v1alpha API (New-generation), via
// legacy-named operations.
//
// These endpoints live under the v1alpha tenant path (the siemplify domain, AppKey
// — NOT the Siemplify external /api/external/v1 API) but carry the
// legacyPlaybooks:legacy* operation names: a thin bridge over the not-yet-native
// playbook surface, used until native v1alpha playbook CRUD ships. It lives in this
// package only for proximity to the other playbook code.
//
// Playbook payloads are large, deeply nested, and freeform, so a Playbook is
// just json.RawMessage. The only place we reach into the body is to coerce the
// field types SOAR insists on (see coercePlaybookTypes) and to validate the
// display name on save (see validatePlaybookName).
package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"danny.vn/secops/soar/internal/transport"
)

// Bridge RPC resources. POST unless noted; appended to the v1alpha instance path.
const (
	playbookListRPC   = "legacyPlaybooks:legacyGetWorkflowMenuCardsWithEnvFilter"
	playbookReadRPC   = "legacyPlaybooks:legacyGetWorkflowFullInfoWithEnvFilterByIdentifier" // GET
	playbookSaveRPC   = "legacyPlaybooks:legacySaveWorkflowDefinitions"
	playbookAttachRPC = "legacyPlaybooks:legacyAttachWorkflowToCase"
	playbookStatsRPC  = "legacyPlaybooks:legacyGetPlaybookStatsMap"
)

// defaultPlaybookTypes is the menu-card filter SOAR expects when none is given.
var defaultPlaybookTypes = []string{"NESTED", "REGULAR"}

// Playbook is a full playbook definition body. It is intentionally opaque: the
// payload is large and freeform, so callers round-trip the raw JSON.
type Playbook = json.RawMessage

// PlaybookCard is a single entry from the playbook menu listing. Raw holds the
// untouched card object for callers that need fields beyond the typed ones.
type PlaybookCard struct {
	// ID is the card's numeric menu id. DEVIATION: the SOAR menu RPC returns this
	// as a JSON number, so it is typed json.Number to decode it without error; the
	// uuid used for workflow operations is Identifier.
	ID           json.Number     `json:"id"`
	Identifier   string          `json:"identifier"`
	Name         string          `json:"name"`
	CategoryName string          `json:"categoryName"`
	IsEnabled    bool            `json:"isEnabled"`
	Raw          json.RawMessage `json:"-"`
}

// playbookListRequest is the body of the menu-cards RPC. SOAR keys the type
// filter under "legacyPayload".
type playbookListRequest struct {
	LegacyPayload []string `json:"legacyPayload"`
}

// playbookListResponse covers the several keys SOAR has used for the card list.
// The primary key is "payload"; "items" and "legacyPayload" are fallbacks.
type playbookListResponse struct {
	Payload       json.RawMessage `json:"payload"`
	Items         json.RawMessage `json:"items"`
	LegacyPayload json.RawMessage `json:"legacyPayload"`
}

// ListPlaybooks returns the playbook menu cards. types filters the card kinds;
// it defaults to ["NESTED","REGULAR"] when empty.
func (c *Client) ListPlaybooks(ctx context.Context, types []string) ([]PlaybookCard, error) {
	if len(types) == 0 {
		types = defaultPlaybookTypes
	}
	var resp playbookListResponse
	if err := c.t.V1Alpha(ctx, "POST", playbookListRPC, playbookListRequest{LegacyPayload: types}, &resp); err != nil {
		return nil, err
	}

	raw := resp.Payload
	if len(raw) == 0 {
		raw = resp.Items
	}
	if len(raw) == 0 {
		raw = resp.LegacyPayload
	}
	if len(raw) == 0 {
		return nil, nil
	}

	// Decode twice: once into typed cards, once into raw objects to populate Raw.
	var cards []PlaybookCard
	if err := json.Unmarshal(raw, &cards); err != nil {
		return nil, fmt.Errorf("legacy: decode playbook cards: %w", err)
	}
	var rawObjs []json.RawMessage
	if err := json.Unmarshal(raw, &rawObjs); err == nil && len(rawObjs) == len(cards) {
		for i := range cards {
			cards[i].Raw = rawObjs[i]
		}
	}
	return cards, nil
}

// GetPlaybook fetches a full playbook definition by its workflow identifier.
func (c *Client) GetPlaybook(ctx context.Context, identifier string) (Playbook, error) {
	if identifier == "" {
		return nil, fmt.Errorf("legacy: GetPlaybook: empty identifier")
	}
	q := url.Values{}
	q.Set("workflowIdentifier", identifier)
	var out Playbook
	if err := c.t.V1Alpha(ctx, "GET", playbookReadRPC, nil, &out, transport.Query(q)); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPlaybookByName lists the menu cards, matches on Name, and reads the full
// definition. When enabledOnly is set, disabled cards are skipped.
//
// DEVIATION: the listing carries no full body, so this is a two-call helper
// (list then read) rather than a single round-trip.
func (c *Client) GetPlaybookByName(ctx context.Context, name string, enabledOnly bool) (Playbook, error) {
	cards, err := c.ListPlaybooks(ctx, nil)
	if err != nil {
		return nil, err
	}
	for _, card := range cards {
		if card.Name != name {
			continue
		}
		if enabledOnly && !card.IsEnabled {
			continue
		}
		return c.GetPlaybook(ctx, card.Identifier)
	}
	return nil, fmt.Errorf("legacy: playbook %q not found", name)
}

// SavePlaybook saves a whole playbook definition. The body's field types are
// coerced to what SOAR expects (see coercePlaybookTypes) and its display name is
// validated (see validatePlaybookName) before the call. The server echo is
// returned.
//
// WARNING: a save is a whole-body REPLACE, not a patch — omitted fields are
// dropped. SOAR also mints a NEW UUID for the saved workflow, so the identifier
// you sent goes stale immediately; re-resolve the playbook by name (see
// GetPlaybookByName) after saving rather than reusing the old identifier.
func (c *Client) SavePlaybook(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	coerced, err := preparePlaybookForSave(body)
	if err != nil {
		return nil, err
	}
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", playbookSaveRPC, coerced, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ValidatePlaybookForSave performs the same local body checks as SavePlaybook
// without making an API call. Use it for dry-runs and local reconcile planning.
func ValidatePlaybookForSave(body json.RawMessage) error {
	_, err := preparePlaybookForSave(body)
	return err
}

func preparePlaybookForSave(body json.RawMessage) (json.RawMessage, error) {
	coerced, err := coercePlaybookTypes(body)
	if err != nil {
		return nil, err
	}
	if name, ok := playbookName(coerced); ok {
		if err := validatePlaybookName(name); err != nil {
			return nil, err
		}
	}
	return coerced, nil
}

// AttachPlaybookToCase attaches a workflow to a case. body is a freeform request
// payload; the server echo is returned.
func (c *Client) AttachPlaybookToCase(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", playbookAttachRPC, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPlaybookStats returns the playbook stats map. body is a freeform request
// payload; the server echo is returned.
func (c *Client) GetPlaybookStats(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", playbookStatsRPC, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// coerceIntFields are numeric fields SOAR returns as strings and rejects as
// numbers on save. They are stringified at the top level, inside "trigger", and
// in every element of "steps".
var coerceIntFields = []string{
	"id",
	"priority",
	"version",
	"modificationTimeUnixTimeInMs",
	"creationTimeUnixTimeInMs",
}

// coercePlaybookTypes normalizes a playbook body for save: it stringifies the
// numeric fields SOAR insists on receiving as strings (at the top level, inside
// "trigger", and in each "steps" element) and replaces a null "templateName"
// with "". It returns the re-marshaled body.
func coercePlaybookTypes(body json.RawMessage) (json.RawMessage, error) {
	// UseNumber: a playbook body carries int64 ids/timestamps that a float64
	// round-trip would corrupt above 2^53.
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("legacy: parse playbook body: %w", err)
	}

	stringifyInts(m)

	if trig, ok := m["trigger"].(map[string]any); ok {
		stringifyInts(trig)
	}
	if steps, ok := m["steps"].([]any); ok {
		for _, s := range steps {
			if step, ok := s.(map[string]any); ok {
				stringifyInts(step)
				coerceStepType(step)
				backfillStepParams(step)
			}
		}
	}

	// templateName: null -> "".
	if v, present := m["templateName"]; present && v == nil {
		m["templateName"] = ""
	}

	out, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("legacy: re-marshal playbook body: %w", err)
	}
	return out, nil
}

// stringifyInts converts each coerceIntFields entry in m to its string form.
// Numbers decoded as float64 are rendered without a trailing ".0".
func stringifyInts(m map[string]any) {
	for _, f := range coerceIntFields {
		v, ok := m[f]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64:
			m[f] = strconv.FormatInt(int64(n), 10)
		case json.Number:
			m[f] = n.String()
		case string:
			// already a string; leave as-is.
		}
	}
}

// stepTypeNames maps the integer step-type code to the string enum the SOAR
// save API expects. Exported playbooks carry the integer; the save endpoint
// rejects it with "Expected string TokenType for enum [StepType]".
var stepTypeNames = map[int64]string{
	0: "ACTION",
	1: "CONDITION",
	2: "PLACEHOLDER",
	5: "BLOCK",
}

// coerceStepType converts a numeric "type" field in a step to the string enum
// the SOAR save API requires. Already-string values are left as-is.
func coerceStepType(step map[string]any) {
	v, ok := step["type"]
	if !ok {
		return
	}
	if _, ok := v.(string); ok {
		return // already a string — leave for the server to validate.
	}
	code, ok := intFieldValue(v)
	if !ok {
		return
	}
	if name, known := stepTypeNames[code]; known {
		step["type"] = name
	}
}

// requiredStepParams are infrastructure parameters the SOAR save endpoint
// expects on every ACTION step. Older playbooks pre-date these fields; the
// server NPEs (HTTP 500, errorCode 2000) when they are absent. Backfilling
// them with their platform defaults lets legacy playbooks save again.
var requiredStepParams = []struct {
	name  string
	value any // nil → JSON null; string → JSON string.
}{
	{"AssignedUsers", nil},
	{"AsyncActionTimeout", "86400"},
	{"AsyncPollingInterval", "3600"},
	{"DynamicInjectionInstancePlaceholder", ""},
	{"FallbackIntegrationInstance", nil},
	{"FetchInstanceByName", "false"},
	{"HasApprovalLink", nil},
	{"MessageToAssignee", nil},
	{"PendingActionTimeout", nil},
}

// backfillStepParams ensures every ACTION step carries the infrastructure
// parameters the server requires. Parameters already present are never
// overwritten.
func backfillStepParams(step map[string]any) {
	// Only backfill ACTION steps — other step kinds use a different param set
	// and save fine without these. isActionStepType handles both the string
	// enum and the numeric export form, so numeric non-action types are never
	// backfilled by mistake.
	if !isActionStepType(step["type"]) {
		return
	}

	params, ok := step["parameters"].([]any)
	if !ok {
		return
	}

	existing := make(map[string]bool, len(params))
	for _, p := range params {
		if pm, ok := p.(map[string]any); ok {
			if name, ok := pm["name"].(string); ok {
				existing[name] = true
			}
		}
	}

	for _, rp := range requiredStepParams {
		if existing[rp.name] {
			continue
		}
		p := map[string]any{
			"name":                         rp.name,
			"value":                        rp.value,
			"isMandatory":                  false,
			"id":                           json.Number("0"),
			"creationTimeUnixTimeInMs":     json.Number("0"),
			"modificationTimeUnixTimeInMs": json.Number("0"),
			"additionalProperties":         map[string]any{},
		}
		params = append(params, p)
	}
	step["parameters"] = params
}

// playbookName extracts the "name" field from a marshaled playbook body.
func playbookName(body json.RawMessage) (string, bool) {
	var m struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return "", false
	}
	if m.Name == "" {
		return "", false
	}
	return m.Name, true
}

// validatePlaybookName allows only letters, digits, space, hyphen, and underscore
// — the set SOAR accepts. Everything else (including . ( ) [ ] : / which break the
// save) is rejected by the allowlist.
func validatePlaybookName(name string) error {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ' ' || r == '-' || r == '_':
		default:
			return fmt.Errorf("legacy: invalid playbook name %q: only letters, digits, space, hyphen, underscore allowed", name)
		}
	}
	return nil
}
