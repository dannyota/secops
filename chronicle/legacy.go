// Chronicle "legacy:" RPC verbs. Despite the name, these are MODERN chronicle
// v1alpha endpoints (New-generation, chronicle.googleapis.com / ADC) that merely
// carry a "legacy" path segment — NOT the Siemplify external /api/external/v1
// API (that capital-L Legacy generation lives in soar/legacy/). FindRawLogs and
// BatchGetCases (the SOAR-int ⇄ SIEM-uuid case bridge) are the live verbs here.
package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// FindRawLogs runs the legacy raw-log lookup against the instance.
//
// body is the request payload (search window, filters, log-type selectors, …);
// it is sent as-is to allow the full, evolving legacy schema without pinning a
// struct here. The response is the original raw log JSON, returned untouched so
// callers can persist the exact bytes the platform stored.
//
// Endpoint: POST .../instances/<id>/legacy:legacyFindRawLogs (an RPC-style verb on
// the "legacy" collection — note the /legacy segment). Matching the legacy tool,
// it uses the project NUMBER form of the resource name (numeric=true).
// FindRawLogsByIDs downloads the FULL, untruncated raw (unparsed) log bytes for
// the given raw-log ids — the ids carried as snippet.id by SearchRawLogs matches.
// SearchRawLogs only returns an 80-char preview snippet; this is how you get the
// complete log line a parser needs.
//
// Endpoint: GET .../instances/<id>/legacy:legacyFindRawLogs?ids=<id>… with an
// empty body (project NUMBER form). Each id is a base64-encoded raw-log id. The
// response carries rawLogs[] (logs grouped per requested id), returned untouched
// so the caller decodes the exact bytes the platform stored.
func (c *Client) FindRawLogsByIDs(ctx context.Context, ids []string) (json.RawMessage, error) {
	q := url.Values{}
	// Match the console request: a large response cap so big logs aren't truncated,
	// and the literal read-only flags it sends.
	q.Set("maxResponseByteSize", "300000000")
	q.Set("regexSearch", "false")
	q.Set("caseSensitive", "false")
	q.Set("query", "")
	for _, id := range ids {
		if id != "" {
			q.Add("ids", id)
		}
	}
	var out json.RawMessage
	if err := c.get(ctx, c.resourcePath("legacy:legacyFindRawLogs", true), &out, withQuery(q)); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) FindRawLogs(ctx context.Context, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.post(ctx, c.resourcePath("legacy:legacyFindRawLogs", true), body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// LegacyBatchGetCasesResponse is the envelope returned by legacyBatchGetCases.
//
// The payload is intentionally not fully modeled: cases carry deeply nested,
// fast-moving fields, so each case is kept as raw JSON. The one field this SDK
// surfaces explicitly is soarPlatformInfo.caseId, which bridges the SOAR
// integer case id to the SIEM case uuid.
type LegacyBatchGetCasesResponse struct {
	Cases []LegacyCase `json:"cases"`
}

// LegacyCase is a single case from legacyBatchGetCases. Raw carries the full,
// unmodeled case JSON; SoarPlatformInfo exposes the SOAR⇄SIEM id bridge.
type LegacyCase struct {
	// Raw is the complete case object exactly as returned.
	Raw json.RawMessage `json:"-"`
	// SoarPlatformInfo carries the SOAR platform linkage (notably caseId).
	SoarPlatformInfo *LegacySoarPlatformInfo `json:"soarPlatformInfo,omitempty"`
}

// LegacySoarPlatformInfo links a SIEM case to its SOAR counterpart.
//
// CaseID is the SOAR integer case id (returned as a string here, like the rest
// of the SOAR surface). The SIEM-side uuid is the case resource name supplied
// in the request.
type LegacySoarPlatformInfo struct {
	CaseID string `json:"caseId"`
}

// UnmarshalJSON captures the full case object in Raw while still decoding the
// soarPlatformInfo bridge into a typed field.
//
// DEVIATION: the official wrapper hands back the whole map untyped. We keep the
// raw bytes (so nothing is lost) yet pull out the one cross-platform id callers
// actually need — the SOAR caseId — without modeling the rest of the schema.
func (c *LegacyCase) UnmarshalJSON(data []byte) error {
	c.Raw = append(c.Raw[:0], data...)
	var probe struct {
		SoarPlatformInfo *LegacySoarPlatformInfo `json:"soarPlatformInfo"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	c.SoarPlatformInfo = probe.SoarPlatformInfo
	return nil
}

// BatchGetCases fetches multiple SIEM cases by uuid in one call.
//
// names are the case uuids (each becomes a repeated ?names=<uuid> query param).
// The response carries soarPlatformInfo.caseId per case — the integer-id ⇄ uuid
// bridge between SOAR and the SIEM. Each returned LegacyCase keeps its full raw
// bytes (LegacyCase.Raw) alongside the typed SoarPlatformInfo.
//
// Endpoint: GET .../instances/<id>/legacy:legacyBatchGetCases (project ID form).
func (c *Client) BatchGetCases(ctx context.Context, names []string) (*LegacyBatchGetCasesResponse, error) {
	q := url.Values{}
	for _, n := range names {
		q.Add("names", n)
	}
	var out LegacyBatchGetCasesResponse
	if err := c.get(ctx, c.resourcePath("legacy:legacyBatchGetCases", false), &out, withQuery(q)); err != nil {
		return nil, err
	}
	return &out, nil
}

// LegacyBatchGetCollectionsResponse is the envelope returned by
// legacyBatchGetCollections — the full detection-alert collection(s) the web UI
// renders when an analyst opens an alert (the rule detection, every mapped UDM
// event, the involved entities, and the alert's triage feedback).
type LegacyBatchGetCollectionsResponse struct {
	Collections []LegacyCollection `json:"collections"`
}

// LegacyCollection is one detection-alert collection. The schema is large and
// fast-moving, so the whole object is kept in Raw; the fields surfaced here are
// the stable ones alert triage reads.
type LegacyCollection struct {
	// Raw is the complete collection object exactly as returned.
	Raw json.RawMessage `json:"-"`
	// ID is the detection-alert id (e.g. de_…), CaseName the SIEM case uuid.
	ID       string `json:"id"`
	CaseName string `json:"caseName"`
	// Detection carries the rule detection(s) — rule name, severity, outcomes.
	Detection []LegacyDetection `json:"detection"`
	// CollectionElements holds the mapped UDM events (Raw, unmodeled).
	CollectionElements []json.RawMessage `json:"collectionElements"`
	// Tags are the detection's labels (e.g. MITRE tactic/technique ids).
	Tags []string `json:"tags"`
	// FeedbackSummary is the alert's current triage disposition, including the
	// AI triage-agent investigation id when an agent has run.
	FeedbackSummary *LegacyFeedbackSummary `json:"feedbackSummary"`
}

// LegacyDetection is one rule detection within a collection.
type LegacyDetection struct {
	RuleName            string `json:"ruleName"`
	Description         string `json:"description"`
	Severity            string `json:"severity"`
	AlertState          string `json:"alertState"`
	RuleSetDisplayName  string `json:"ruleSetDisplayName"`
	RulesetCategoryName string `json:"rulesetCategoryDisplayName"`
}

// LegacyFeedbackSummary is the alert's triage disposition as stored on the
// detection collection (a superset of the snapshot view's feedback).
type LegacyFeedbackSummary struct {
	Verdict                    string `json:"verdict"`
	Status                     string `json:"status"`
	PriorityDisplay            string `json:"priorityDisplay"`
	SeverityDisplay            string `json:"severityDisplay"`
	RiskScore                  int    `json:"riskScore"`
	UserType                   string `json:"userType"`
	TriageAgentInvestigationID string `json:"triageAgentInvestigationId"`
}

// UnmarshalJSON keeps the full collection bytes in Raw while decoding the typed
// fields above (mirroring LegacyCase).
func (c *LegacyCollection) UnmarshalJSON(data []byte) error {
	type alias LegacyCollection
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = LegacyCollection(a)
	c.Raw = append(c.Raw[:0], data...)
	return nil
}

// BatchGetCollections fetches detection-alert collections by id in one call —
// the rich per-alert view the console renders (rule detection, mapped UDM
// events, entities, and triage feedback). This is the surface the web UI reads
// when an alert is opened; it supersedes the unused enrichmentAgent path.
//
// ids are detection-alert ids (each becomes a repeated ?collectionIds=<id>
// query param). Each returned collection keeps its full raw bytes alongside the
// typed summary fields.
//
// Endpoint: GET .../instances/<id>/legacy:legacyBatchGetCollections (project ID
// form, ADC — same host/auth as its legacy: siblings).
func (c *Client) BatchGetCollections(ctx context.Context, ids []string) (*LegacyBatchGetCollectionsResponse, error) {
	q := url.Values{}
	for _, id := range ids {
		if id != "" {
			q.Add("collectionIds", id)
		}
	}
	var out LegacyBatchGetCollectionsResponse
	if err := c.get(ctx, c.resourcePath("legacy:legacyBatchGetCollections", false), &out, withQuery(q)); err != nil {
		return nil, err
	}
	return &out, nil
}
