// ti.go — Threat Intelligence (SIEM plane, read-only).
//
// threatCollections is the Google/Mandiant threat-intelligence catalog the tenant
// is matched against: campaigns, reports, threat actors, malware families and
// vulnerabilities. It is upstream-sourced, so there is no write path — this is an
// operational read surface (query → review). See docs/design/surfaces.md.

package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// tiAPIVersion (Threat-Intel + modern IoCs) is pinned in versions.go.

// Threat-collection type tokens used in the `collection_type:` filter. The set is
// open (the server may add more); these are the common ones.
const (
	CollectionCampaign      = "campaign"
	CollectionReport        = "report"
	CollectionActor         = "actor"
	CollectionMalware       = "malware"
	CollectionVulnerability = "vulnerability"
)

// ThreatCollection is one threat-intelligence collection. The stable framing is
// typed; the full server object (which carries the type-specific detail —
// associations, targetedIndustries/Regions, content, executiveSummary) is kept in
// Raw. Read-only.
type ThreatCollection struct {
	Name               string          `json:"name"`                 // full resource name (.../threatCollections/{id})
	ID                 string          `json:"-"`                    // short id (last name segment), e.g. "report--26-10031441"
	Type               string          `json:"threatCollectionType"` // campaign | report | actor | malware | vulnerability | …
	DisplayName        string          `json:"displayName"`
	Description        string          `json:"description"`
	CreateTime         string          `json:"createTime"`
	UpdateTime         string          `json:"updateTime"`
	AltNames           []string        `json:"altNames,omitempty"` // GTI/Mandiant ids, e.g. CAMP.22.147
	Associations       []string        `json:"associations,omitempty"`
	Iocs               []string        `json:"iocs,omitempty"`
	TargetedIndustries []string        `json:"targetedIndustries,omitempty"`
	TargetedRegions    []string        `json:"targetedRegions,omitempty"`
	Raw                json.RawMessage `json:"-"` // full server object, untrimmed
}

// UnmarshalJSON decodes the typed fields, derives the short ID from the resource
// name, and preserves the complete server object in Raw (the threatCollection
// payload carries far more than we type, and has no top-level id field).
func (t *ThreatCollection) UnmarshalJSON(data []byte) error {
	type alias ThreatCollection // avoid recursion
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*t = ThreatCollection(a)
	t.ID = lastSegment(t.Name)
	t.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ThreatCollectionQuery are the list options.
//
// Types are OR-ed into a `(collection_type:a OR collection_type:b)` filter (empty
// = all types). OrderBy is a server order key, e.g. "last_modification_date-" (a
// trailing "-" sorts descending). PageSize bounds each page; MaxPages caps how
// many pages are fetched (a guard against an unbounded sweep).
type ThreatCollectionQuery struct {
	Types    []string
	OrderBy  string
	PageSize int
	MaxPages int
}

// threatCollectionsList is the v1alpha LIST envelope.
type threatCollectionsList struct {
	Items         []ThreatCollection `json:"threatCollections"`
	NextPageToken string             `json:"nextPageToken"`
}

// RelatedThreatCollectionType is the campaign/report type selector accepted by
// threatCollections:fetchRelated.
type RelatedThreatCollectionType string

const (
	RelatedThreatCollectionCampaign RelatedThreatCollectionType = "CAMPAIGN"
	RelatedThreatCollectionReport   RelatedThreatCollectionType = "REPORT"
)

// RelatedThreatCollectionQuery are the fetchRelated options. Exactly one threat
// resource must be set. PageSize is capped by the API at 40; MaxPages bounds the
// local sweep.
type RelatedThreatCollectionQuery struct {
	Type             RelatedThreatCollectionType
	Ioc              string
	IocAssociation   string
	ThreatCollection string
	OrderBy          string
	PageSize         int
	MaxPages         int
}

// IocMatchMetadata is the match-count record returned for a GTI/Mandiant
// collection alt name (for example CAMP.22.147).
type IocMatchMetadata struct {
	ThreatCollection string `json:"threatCollection"`
	IocMatchesCount  int64  `json:"iocMatchesCount"`
}

type threatCollectionRelatedList struct {
	Items         []ThreatCollection `json:"threatCollections"`
	NextPageToken string             `json:"nextPageToken"`
	TotalSize     int64              `json:"totalSize"`
}

// tcFilter builds the `(collection_type:a OR collection_type:b)` filter for the
// requested types, or "" for all types.
func tcFilter(types []string) string {
	if len(types) == 0 {
		return ""
	}
	parts := make([]string, 0, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t != "" {
			parts = append(parts, "collection_type:"+t)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// ListThreatCollections returns threat-intelligence collections matching the
// query. Read-only.
//
// Endpoint: GET {instance}/threatCollections with optional filter / orderBy /
// pageSize. Uses the project NUMBER in the resource name (the form the live
// Threat-Intel surface serves).
func (c *Client) ListThreatCollections(ctx context.Context, q ThreatCollectionQuery) ([]ThreatCollection, error) {
	maxPages := q.MaxPages
	if maxPages <= 0 {
		maxPages = 50
	}
	var all []ThreatCollection
	err := paginate(maxPages, func(token string) (string, error) {
		vals := url.Values{}
		if f := tcFilter(q.Types); f != "" {
			vals.Set("filter", f)
		}
		if q.OrderBy != "" {
			vals.Set("orderBy", q.OrderBy)
		}
		if q.PageSize > 0 {
			vals.Set("pageSize", fmt.Sprintf("%d", q.PageSize))
		}
		if token != "" {
			vals.Set("pageToken", token)
		}
		var page threatCollectionsList
		if err := c.get(ctx, c.resourcePath("threatCollections", true), &page, withQuery(vals), withVersion(tiAPIVersion)); err != nil {
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

// GetThreatCollection fetches one collection by its short id (e.g.
// "report--26-10031441") or full resource name. Read-only.
func (c *Client) GetThreatCollection(ctx context.Context, id string) (*ThreatCollection, error) {
	sub := "threatCollections/" + url.PathEscape(lastSegment(id))
	var out ThreatCollection
	if err := c.get(ctx, c.resourcePath(sub, true), &out, withVersion(tiAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}

// lastSegment returns the final "/"-segment of a resource name, or s unchanged if
// it has none — so callers may pass either the short id or the full resource name.
func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// FetchRelatedThreatCollections lists campaigns or reports related to one threat
// artifact (an IoC, IoC association, or another threat collection). Read-only.
func (c *Client) FetchRelatedThreatCollections(ctx context.Context, q RelatedThreatCollectionQuery) ([]ThreatCollection, error) {
	if strings.TrimSpace(string(q.Type)) == "" {
		return nil, fmt.Errorf("chronicle: related threat collection type is required")
	}
	if countNonEmpty(q.Ioc, q.IocAssociation, q.ThreatCollection) != 1 {
		return nil, fmt.Errorf("chronicle: set exactly one of Ioc, IocAssociation, or ThreatCollection")
	}
	maxPages := q.MaxPages
	if maxPages <= 0 {
		maxPages = 50
	}
	var all []ThreatCollection
	err := paginate(maxPages, func(token string) (string, error) {
		vals := url.Values{}
		vals.Set("threatCollectionType", strings.TrimSpace(string(q.Type)))
		if q.Ioc != "" {
			vals.Set("ioc", c.iocResourceName(q.Ioc))
		}
		if q.IocAssociation != "" {
			vals.Set("iocAssociation", c.iocAssociationResourceName(q.IocAssociation))
		}
		if q.ThreatCollection != "" {
			vals.Set("threatCollection", c.threatCollectionResourceName(q.ThreatCollection))
		}
		if q.OrderBy != "" {
			vals.Set("orderBy", q.OrderBy)
		}
		if q.PageSize > 0 {
			vals.Set("pageSize", fmt.Sprintf("%d", q.PageSize))
		}
		if token != "" {
			vals.Set("pageToken", token)
		}
		var page threatCollectionRelatedList
		if err := c.get(ctx, c.resourcePath("threatCollections:fetchRelated", true), &page, withQuery(vals), withVersion(tiAPIVersion)); err != nil {
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

// FetchIocMatchMetadata returns IoC match counts for GTI/Mandiant collection alt
// names such as CAMP.22.147. Read-only.
func (c *Client) FetchIocMatchMetadata(ctx context.Context, altNames ...string) ([]IocMatchMetadata, error) {
	q := url.Values{}
	for _, name := range altNames {
		name = strings.TrimSpace(name)
		if name != "" {
			q.Add("threatCollections", name)
		}
	}
	if len(q) == 0 {
		return nil, nil
	}
	var resp struct {
		Items []IocMatchMetadata `json:"iocMatchMetadata"`
	}
	if err := c.get(ctx, c.resourcePath("threatCollections:fetchIocMatchMetadata", true), &resp, withQuery(q), withVersion(tiAPIVersion)); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func countNonEmpty(vals ...string) int {
	n := 0
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}

func (c *Client) threatCollectionResourceName(id string) string {
	return c.tiResourceName("threatCollections", id, true)
}

func (c *Client) iocResourceName(id string) string {
	return c.tiResourceName("iocs", id, false)
}

func (c *Client) iocAssociationResourceName(id string) string {
	return c.tiResourceName("iocAssociations", id, false)
}

func (c *Client) tiResourceName(kind, id string, numeric bool) string {
	id = strings.TrimSpace(id)
	if strings.Contains(id, "/") {
		return id
	}
	return c.resourcePath(kind+"/"+url.PathEscape(lastSegment(id)), numeric)
}

// ---------------------------------------------------------------------------
// IoC associations — malware families and threat actors linked to IoCs.
// ---------------------------------------------------------------------------

// AssociationType is the IoC-association type selector. The set is open; these
// are the server's tokens. DEVIATION: the REST reference documents
// MALWARE_FAMILY, but the server rejects it and returns/accepts MALWARE.
type AssociationType string

const (
	AssociationMalware         AssociationType = "MALWARE"
	AssociationThreatActor     AssociationType = "THREAT_ACTOR"
	AssociationSoftwareToolkit AssociationType = "SOFTWARE_TOOLKIT"
)

// IocAssociation is one malware / threat-actor / software-toolkit IoC
// association record.
type IocAssociation struct {
	Name               string          `json:"name"`
	ID                 string          `json:"id"` // short id, e.g. "malware--<uuid>"
	AssociationType    string          `json:"type"`
	ThreatDisplayName  string          `json:"threatDisplayName"`
	Description        string          `json:"description"`
	FirstReferenceTime string          `json:"firstReferenceTime"`
	LastReferenceTime  string          `json:"lastReferenceTime"`
	Roles              []string        `json:"roles,omitempty"`
	OperatingSystems   []string        `json:"operatingSystems,omitempty"`
	IndustriesAffected []string        `json:"industriesAffected,omitempty"`
	Iocs               []string        `json:"iocs,omitempty"`
	Raw                json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields, derives the short ID, and preserves
// the full server payload in Raw.
func (a *IocAssociation) UnmarshalJSON(data []byte) error {
	type alias IocAssociation
	var v alias
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*a = IocAssociation(v)
	if a.ID == "" {
		a.ID = lastSegment(a.Name)
	}
	a.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// iocAssociationsBatchGetResponse is the batchGet envelope.
type iocAssociationsBatchGetResponse struct {
	Items []IocAssociation `json:"associations"`
}

// batchGetIocAssociationsChunkSize is the maximum number of resource names per
// batchGet request. The console sends up to ~100 in a POST with
// x-http-method-override; we use plain GET and stay within URL-length limits.
const batchGetIocAssociationsChunkSize = 80

// BatchGetIocAssociations fetches IoC associations by their short ids or full
// resource names. The input is automatically chunked to avoid exceeding
// URL-length limits. Read-only.
func (c *Client) BatchGetIocAssociations(ctx context.Context, ids ...string) ([]IocAssociation, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// Deduplicate and build full resource names.
	seen := make(map[string]bool, len(ids))
	var names []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		full := c.iocAssociationResourceName(id)
		if seen[full] {
			continue
		}
		seen[full] = true
		names = append(names, full)
	}
	if len(names) == 0 {
		return nil, nil
	}

	var all []IocAssociation
	for lo := 0; lo < len(names); lo += batchGetIocAssociationsChunkSize {
		hi := min(lo+batchGetIocAssociationsChunkSize, len(names))
		chunk := names[lo:hi]
		q := url.Values{}
		for _, n := range chunk {
			q.Add("names", n)
		}
		var resp iocAssociationsBatchGetResponse
		if err := c.get(ctx, c.resourcePath("iocAssociations:batchGet", true), &resp, withQuery(q), withVersion(tiAPIVersion)); err != nil {
			return nil, err
		}
		all = append(all, resp.Items...)
	}
	return all, nil
}

// GetIocAssociation fetches one IoC association by short id or full resource
// name. Read-only.
func (c *Client) GetIocAssociation(ctx context.Context, id string) (*IocAssociation, error) {
	sub := "iocAssociations/" + url.PathEscape(lastSegment(id))
	var out IocAssociation
	if err := c.get(ctx, c.resourcePath(sub, true), &out, withVersion(tiAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}

// RelatedAssociationQuery are the options for fetching IoC associations related
// to a given threat resource. Exactly one of Ioc, IocAssociation, or
// ThreatCollection must be set. OrderBy is a server-side sort key; PageSize
// bounds each page; MaxPages caps local sweeping.
type RelatedAssociationQuery struct {
	Type             AssociationType
	Ioc              string
	IocAssociation   string
	ThreatCollection string
	OrderBy          string
	PageSize         int
	MaxPages         int
}

type iocAssociationsRelatedList struct {
	Items         []IocAssociation `json:"associations"`
	NextPageToken string           `json:"nextPageToken"`
	TotalSize     int64            `json:"totalSize"`
}

// FetchRelatedAssociations lists IoC associations related to one threat
// resource (an IoC, another IoC association, or a threat collection). Read-only.
func (c *Client) FetchRelatedAssociations(ctx context.Context, q RelatedAssociationQuery) ([]IocAssociation, error) {
	if countNonEmpty(q.Ioc, q.IocAssociation, q.ThreatCollection) != 1 {
		return nil, fmt.Errorf("chronicle: set exactly one of Ioc, IocAssociation, or ThreatCollection")
	}
	maxPages := q.MaxPages
	if maxPages <= 0 {
		maxPages = 50
	}
	var all []IocAssociation
	err := paginate(maxPages, func(token string) (string, error) {
		vals := url.Values{}
		if q.Type != "" {
			vals.Set("associationType", string(q.Type))
		}
		if q.Ioc != "" {
			vals.Set("ioc", c.iocResourceName(q.Ioc))
		}
		if q.IocAssociation != "" {
			vals.Set("iocAssociation", c.iocAssociationResourceName(q.IocAssociation))
		}
		if q.ThreatCollection != "" {
			vals.Set("threatCollection", c.threatCollectionResourceName(q.ThreatCollection))
		}
		if q.OrderBy != "" {
			vals.Set("orderBy", q.OrderBy)
		}
		if q.PageSize > 0 {
			vals.Set("pageSize", fmt.Sprintf("%d", q.PageSize))
		}
		if token != "" {
			vals.Set("pageToken", token)
		}
		var page iocAssociationsRelatedList
		if err := c.get(ctx, c.resourcePath("iocAssociations:fetchRelated", true), &page, withQuery(vals), withVersion(tiAPIVersion)); err != nil {
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

// ---------------------------------------------------------------------------
// Coverage details — rule × threat-collection coverage with filter support.
// ---------------------------------------------------------------------------

// CoverageDetail is one rule × threat-collection mapping from the
// coverageDetails surface.
type CoverageDetail struct {
	Name             string          `json:"name"`
	ThreatCollection string          `json:"threatCollection"`
	Rule             string          `json:"rule"`
	Raw              json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and preserves the full server object.
func (d *CoverageDetail) UnmarshalJSON(data []byte) error {
	type alias CoverageDetail
	var v alias
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*d = CoverageDetail(v)
	d.Raw = append(json.RawMessage(nil), data...)
	return nil
}

type coverageDetailsList struct {
	Items         []CoverageDetail `json:"coverageDetails"`
	NextPageToken string           `json:"nextPageToken"`
	TotalSize     int64            `json:"totalSize"`
}

// coverageFilterChunkSize is the maximum number of threat-collection ids per
// coverageDetails filter clause. The console sends ~40 per POST; plain GET must
// stay within URL-length limits.
const coverageFilterChunkSize = 40

// CoverageFilter builds the filter string for ListCoverageDetailsFiltered from
// a set of threat-collection ids (short form like "campaign--<uuid>" or full
// resource names). Returns "" for an empty set.
func CoverageFilter(collectionIDs []string) string {
	if len(collectionIDs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(collectionIDs))
	for _, id := range collectionIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		parts = append(parts, `threat_collection:"`+lastSegment(id)+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " OR ")
}

// ListCoverageDetailsFiltered returns coverage details for the given
// threat-collection ids. IDs are chunked to coverageFilterChunkSize per call.
// Uses project NUMBER (numeric=true) in the resource path. Read-only.
func (c *Client) ListCoverageDetailsFiltered(ctx context.Context, collectionIDs []string, pageSize int) ([]CoverageDetail, error) {
	if len(collectionIDs) == 0 {
		return nil, nil
	}
	var all []CoverageDetail
	for lo := 0; lo < len(collectionIDs); lo += coverageFilterChunkSize {
		hi := min(lo+coverageFilterChunkSize, len(collectionIDs))
		filter := CoverageFilter(collectionIDs[lo:hi])
		if filter == "" {
			continue
		}
		err := paginate(50, func(token string) (string, error) {
			q := url.Values{}
			q.Set("filter", filter)
			if pageSize > 0 {
				q.Set("pageSize", fmt.Sprintf("%d", pageSize))
			}
			if token != "" {
				q.Set("pageToken", token)
			}
			var page coverageDetailsList
			if err := c.get(ctx, c.resourcePath("coverageDetails", true), &page, withQuery(q), withVersion(coverageAPIVersion)); err != nil {
				return "", err
			}
			all = append(all, page.Items...)
			return page.NextPageToken, nil
		})
		if err != nil {
			return nil, err
		}
	}
	return all, nil
}

// ---------------------------------------------------------------------------
// Threat-collection filter set — the filter/facet metadata for the Emerging
// Threats UI.
// ---------------------------------------------------------------------------

// GetThreatCollectionFilterSet returns the threat-collection filter-set metadata
// (the set of available facets the Emerging Threats console uses to populate its
// filter sidebar). The response shape is not fully documented; it is returned as
// raw JSON.
//
// DEVIATION: the official docs describe this as an instance-level custom method
// `:getThreatCollectionFilterSet`; the console fetches the plain subresource
// `threatCollectionFilterSet`. Both forms are probed by the live version test;
// the SDK defaults to the subresource form (console-observed).
func (c *Client) GetThreatCollectionFilterSet(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.get(ctx, c.resourcePath("threatCollectionFilterSet", true), &raw, withVersion(tiAPIVersion)); err != nil {
		return nil, err
	}
	return raw, nil
}
