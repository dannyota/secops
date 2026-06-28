// ti_iocs.go — Modern IoC resource surface (SIEM plane, read-only).
//
// The modern `iocs` resource is the per-indicator IoC surface (lookup an indicator
// value -> its IoC record), distinct from the legacy enterprise-wide search in
// entity.go (ListIoCs). Read-only.

package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

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
