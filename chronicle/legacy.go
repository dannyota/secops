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
