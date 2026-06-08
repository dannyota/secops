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

// --- modern IoCs ------------------------------------------------------------
//
// The modern `iocs` resource is the per-indicator IoC surface (lookup an indicator
// value → its IoC record), distinct from the legacy enterprise-wide search in
// entity.go (ListIoCs). Read-only.

// IoCValueType is the indicator kind for a FindIoCs lookup. Only these are
// accepted by iocs:find (the FieldAndValue ValueType enum subset it supports).
type IoCValueType string

const (
	IoCValueMD5    IoCValueType = "HASH_MD5"
	IoCValueSHA1   IoCValueType = "HASH_SHA1"
	IoCValueSHA256 IoCValueType = "HASH_SHA256"
	IoCValueDomain IoCValueType = "DOMAIN_NAME"
	IoCValueIP     IoCValueType = "RESOLVED_IP_ADDRESS"
)

// RelatedIoCType is the IocType enum accepted by iocs:fetchRelated.
type RelatedIoCType string

const (
	RelatedIoCDomain     RelatedIoCType = "DOMAIN"
	RelatedIoCIP         RelatedIoCType = "IP"
	RelatedIoCURL        RelatedIoCType = "URL"
	RelatedIoCFileHash   RelatedIoCType = "FILE_HASH"
	RelatedIoCFileMD5    RelatedIoCType = "FILE_HASH_MD5"
	RelatedIoCFileSHA1   RelatedIoCType = "FILE_HASH_SHA1"
	RelatedIoCFileSHA256 RelatedIoCType = "FILE_HASH_SHA256"
)

// FieldAndValue is one indicator lookup: the value plus its type.
type FieldAndValue struct {
	Value     string       `json:"value"`
	ValueType IoCValueType `json:"valueType"`
}

// Ioc is a modern IoC record. The stable framing is typed; the indicator value is
// in DisplayName/ArtifactIndicator and the full record is in Raw.
type Ioc struct {
	Name              string          `json:"name"` // .../iocs/{ioc} — the authoritative id (NOT the indicator value)
	ID                string          `json:"-"`
	DisplayName       string          `json:"displayName"`
	IocType           string          `json:"iocType"`
	ArtifactIndicator json.RawMessage `json:"artifactIndicator"`
	Raw               json.RawMessage `json:"-"`
}

// RelatedIoCQuery are the fetchRelated options. Exactly one threat resource must
// be set. PageSize is capped by the API at 40; MaxPages bounds the local sweep.
type RelatedIoCQuery struct {
	IocType          RelatedIoCType
	ThreatCollection string
	IocAssociation   string
	OrderBy          string
	PageSize         int
	MaxPages         int
}

type relatedIoCList struct {
	Iocs          []Ioc  `json:"iocs"`
	NextPageToken string `json:"nextPageToken"`
	TotalSize     int64  `json:"totalSize"`
}

// UnmarshalJSON decodes typed fields, derives ID from the name, keeps Raw.
func (i *Ioc) UnmarshalJSON(data []byte) error {
	type alias Ioc
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*i = Ioc(a)
	i.ID = lastSegment(i.Name)
	i.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// FindIoCs resolves one or more indicator values to their IoC records
// (POST {instance}/iocs:find). It is a lookup, not a pageable list — pass the
// indicators to resolve. An unmatched indicator simply yields no record. Read-only.
func (c *Client) FindIoCs(ctx context.Context, lookups ...FieldAndValue) ([]Ioc, error) {
	if len(lookups) == 0 {
		return nil, nil
	}
	body := struct {
		FieldAndValue []FieldAndValue `json:"fieldAndValue"`
	}{FieldAndValue: lookups}
	var resp struct {
		Iocs []Ioc `json:"iocs"`
	}
	if err := c.post(ctx, c.resourcePath("iocs:find", false), body, &resp, withVersion(tiAPIVersion)); err != nil {
		return nil, err
	}
	return resp.Iocs, nil
}

// GetIoC fetches one IoC by id or full resource name (GET {instance}/iocs/{ioc}).
// The id is the Ioc.Name resource-id segment from FindIoCs/BatchGetIoCs — NOT the
// raw indicator value. Read-only.
func (c *Client) GetIoC(ctx context.Context, id string) (*Ioc, error) {
	var out Ioc
	if err := c.get(ctx, c.resourcePath("iocs/"+url.PathEscape(lastSegment(id)), false), &out, withVersion(tiAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}

// BatchGetIoCs fetches several IoCs by their full resource names in one call
// (GET {instance}/iocs:batchGet?names=...). Read-only.
func (c *Client) BatchGetIoCs(ctx context.Context, names ...string) ([]Ioc, error) {
	if len(names) == 0 {
		return nil, nil
	}
	q := url.Values{}
	for _, n := range names {
		q.Add("names", n)
	}
	var resp struct {
		Iocs []Ioc `json:"iocs"`
	}
	if err := c.get(ctx, c.resourcePath("iocs:batchGet", false), &resp, withQuery(q), withVersion(tiAPIVersion)); err != nil {
		return nil, err
	}
	return resp.Iocs, nil
}

// FetchRelatedIoCs lists IoCs of a requested type related to a threat collection
// or IoC association. Read-only.
func (c *Client) FetchRelatedIoCs(ctx context.Context, q RelatedIoCQuery) ([]Ioc, error) {
	if strings.TrimSpace(string(q.IocType)) == "" {
		return nil, fmt.Errorf("chronicle: related IoC type is required")
	}
	if countNonEmpty(q.ThreatCollection, q.IocAssociation) != 1 {
		return nil, fmt.Errorf("chronicle: set exactly one of ThreatCollection or IocAssociation")
	}
	maxPages := q.MaxPages
	if maxPages <= 0 {
		maxPages = 50
	}
	var all []Ioc
	err := paginate(maxPages, func(token string) (string, error) {
		vals := url.Values{}
		vals.Set("iocType", strings.TrimSpace(string(q.IocType)))
		if q.ThreatCollection != "" {
			vals.Set("threatCollection", c.threatCollectionResourceName(q.ThreatCollection))
		}
		if q.IocAssociation != "" {
			vals.Set("iocAssociation", c.iocAssociationResourceName(q.IocAssociation))
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
		var page relatedIoCList
		if err := c.get(ctx, c.resourcePath("iocs:fetchRelated", false), &page, withQuery(vals), withVersion(tiAPIVersion)); err != nil {
			return "", err
		}
		all = append(all, page.Iocs...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}
