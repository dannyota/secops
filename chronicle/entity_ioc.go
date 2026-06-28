package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// --- ListIoCs ---------------------------------------------------------------

// IoCAssociation is a threat-intel association (campaign, actor, malware, …)
// attached to an IoC match.
type IoCAssociation struct {
	Name            string `json:"name,omitempty"`
	AssociationID   string `json:"associationId,omitempty"`
	AssociationType string `json:"associationType,omitempty"`
	RegionCode      string `json:"regionCode,omitempty"` // lifted region/country code
}

// UnmarshalJSON lifts regionCode from either shape: the live API sends an object
// {"countryOrRegion":"…"}, while the older shape sent a bare string. Either way
// RegionCode ends up as the code string.
func (a *IoCAssociation) UnmarshalJSON(b []byte) error {
	var raw struct {
		Name            string          `json:"name"`
		AssociationID   string          `json:"associationId"`
		AssociationType string          `json:"associationType"`
		RegionCode      json.RawMessage `json:"regionCode"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.Name, a.AssociationID, a.AssociationType, a.RegionCode = raw.Name, raw.AssociationID, raw.AssociationType, ""
	if len(raw.RegionCode) > 0 {
		var s string
		var obj struct {
			CountryOrRegion string `json:"countryOrRegion"`
		}
		if json.Unmarshal(raw.RegionCode, &s) == nil {
			a.RegionCode = s
		} else if json.Unmarshal(raw.RegionCode, &obj) == nil {
			a.RegionCode = obj.CountryOrRegion
		}
	}
	return nil
}

// IoCMatch is one enterprise-wide indicator-of-compromise match.
//
// FilterProperties is the freeform per-IoC property bag (string properties
// keyed by name, each with raw values) and varies by indicator type, so it is
// kept as json.RawMessage. The stable framing (artifact, timestamps,
// associations) is typed.
type IoCMatch struct {
	ArtifactIndicator     json.RawMessage  `json:"artifactIndicator,omitempty"`
	IoCIngestTimestamp    string           `json:"iocIngestTimestamp,omitempty"`
	FirstSeenTimestamp    string           `json:"firstSeenTimestamp,omitempty"`
	LastSeenTimestamp     string           `json:"lastSeenTimestamp,omitempty"`
	FilterProperties      json.RawMessage  `json:"filterProperties,omitempty"`
	AssociationIdentifier []IoCAssociation `json:"associationIdentifier,omitempty"`
	// Sources lists the feeds that reported the IoC (e.g. ["Mandiant"]) — a
	// top-level string array in the wrapper's canonical response.
	//
	// DEVIATION: dropped the speculative flat Confidence/RawSeverity/Categorization
	// fields the initial port guessed at; the only severity/category data the
	// upstream confirms lives nested under FilterProperties (kept raw above).
	Sources []string `json:"sources,omitempty"`
	// Raw is the complete match object as returned, for fields not modeled above.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps the typed fields and retains the full match in Raw.
func (m *IoCMatch) UnmarshalJSON(b []byte) error {
	type alias IoCMatch // avoid recursing into this method
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*m = IoCMatch(a)
	m.Raw = append(m.Raw[:0], b...)
	return nil
}

// ListIoCs returns enterprise-wide IoC matches observed in [start, end].
//
// prioritized restricts results to prioritized (curated/Mandiant-prioritized)
// IoCs only. maxMatches caps the number of matches returned (<=0 → 1000, the
// wrapper's default). Mandiant attributes are always requested (the wrapper's
// add_mandiant_attributes default of true), enriching each match.
//
// Endpoint: GET {instance}/legacy:legacySearchEnterpriseWideIoCs with params
// timestampRange.startTime/endTime (microsecond UTC), maxMatchesToReturn,
// addMandiantAttributes, fetchPrioritizedIocsOnly.
//
// DEVIATION: the wrapper post-processes the response (strips trailing "Z" from
// timestamps, flattens stringProperties into a "properties" map, dedupes
// associations). We return the matches as the server sends them, keeping the
// freeform property bag raw, and dedupe associations on (name, associationType)
// — the one transform that loses no information.
func (c *Client) ListIoCs(ctx context.Context, start, end time.Time, prioritized bool, maxMatches int) ([]IoCMatch, error) {
	if !start.Before(end) {
		return nil, fmt.Errorf("chronicle: ListIoCs start (%s) must be before end (%s)",
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}
	if maxMatches <= 0 {
		maxMatches = 1000
	}

	q := url.Values{
		"timestampRange.startTime": {summaryTimeFmt(start)},
		"timestampRange.endTime":   {summaryTimeFmt(end)},
		"maxMatchesToReturn":       {fmt.Sprintf("%d", maxMatches)},
		"addMandiantAttributes":    {"true"},
		"fetchPrioritizedIocsOnly": {fmt.Sprintf("%t", prioritized)},
	}

	var resp struct {
		Matches []IoCMatch `json:"matches"`
	}
	// legacy: paths attach after the instance with a slash (not RPC-style).
	if err := c.get(ctx, c.resourcePath("legacy:legacySearchEnterpriseWideIoCs", false), &resp, withQuery(q)); err != nil {
		return nil, err
	}

	for i := range resp.Matches {
		resp.Matches[i].AssociationIdentifier = dedupeAssociations(resp.Matches[i].AssociationIdentifier)
	}
	return resp.Matches, nil
}

// dedupeAssociations drops associations sharing a (name, associationType) key,
// which the API can repeat across region codes. Order is preserved.
func dedupeAssociations(in []IoCAssociation) []IoCAssociation {
	if len(in) < 2 {
		return in
	}
	seen := make(map[[2]string]struct{}, len(in))
	out := in[:0]
	for _, a := range in {
		k := [2]string{a.Name, a.AssociationType}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, a)
	}
	return out
}
