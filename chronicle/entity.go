package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// flexInt is an integer the API may render as a JSON number or, for proto3 int64
// fields, as a quoted string. It decodes either form (and null → 0); the zero
// value omits under omitempty and marshals back as a plain number.
type flexInt int

func (n *flexInt) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*n = 0
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if s == "" {
			*n = 0
			return nil
		}
		i, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("flexInt %q: %w", s, err)
		}
		*n = flexInt(i)
		return nil
	}
	var i int
	if err := json.Unmarshal(data, &i); err != nil {
		return err
	}
	*n = flexInt(i)
	return nil
}

// Entity summary and IoC listing.
//
// Both endpoints build their URL from the string project_id (numeric=false),
// matching the wrapper, whose entity/ioc modules issue every request from the
// instance built off project_id. See resource.go for why the form is explicit
// per endpoint.
//
// The wrapper uses millisecond-precision UTC timestamps
// (strftime("%Y-%m-%dT%H:%M:%S.%fZ")). We format with RFC3339Nano truncated to
// the same wire shape so the server sees an identical time range.

// summaryTimeFmt mirrors the wrapper's "%Y-%m-%dT%H:%M:%S.%fZ" microsecond UTC
// stamp used on every timeRange/timestampRange param.
func summaryTimeFmt(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000Z")
}

// --- entity types -----------------------------------------------------------

// TimeInterval is an entity's observation window.
type TimeInterval struct {
	StartTime time.Time `json:"startTime,omitzero"`
	EndTime   time.Time `json:"endTime,omitzero"`
}

// EntityMetadata carries the entity's type and observation interval.
type EntityMetadata struct {
	EntityType string        `json:"entityType,omitempty"`
	Interval   *TimeInterval `json:"interval,omitempty"`
}

// EntityMetrics carries first/last-seen timestamps for an entity.
type EntityMetrics struct {
	FirstSeen time.Time `json:"firstSeen,omitzero"`
	LastSeen  time.Time `json:"lastSeen,omitzero"`
}

// Entity is one resolved entity (asset, domain, file, user, IP, …).
//
// Entity holds the freeform UDM-shaped entity body as json.RawMessage: it is a
// large, type-dependent, evolving structure (asset vs file vs domain), so we
// keep it raw rather than modeling every variant. The stable framing
// (name/metadata/metric) is typed.
type Entity struct {
	Name     string          `json:"name,omitempty"`
	Metadata *EntityMetadata `json:"metadata,omitempty"`
	Metric   *EntityMetrics  `json:"metric,omitempty"`
	Entity   json.RawMessage `json:"entity,omitempty"`
}

// EntityID returns the trailing path segment of the entity's resource Name,
// the ID the :summarizeEntity endpoint expects.
func (e *Entity) EntityID() string {
	if e == nil || e.Name == "" {
		return ""
	}
	return e.Name[strings.LastIndex(e.Name, "/")+1:]
}

// EntityType returns the entity's metadata type, or "" if absent.
func (e *Entity) EntityType() string {
	if e == nil || e.Metadata == nil {
		return ""
	}
	return e.Metadata.EntityType
}

// AlertCount is the number of alerts a given rule raised for the entity. The API
// renders the count as a JSON string (proto3 int64 JSON), so it is a flexInt.
type AlertCount struct {
	Rule  string  `json:"rule,omitempty"`
	Count flexInt `json:"count,omitempty"`
}

// TimelineBucket is one fixed-width slot of the activity timeline.
type TimelineBucket struct {
	AlertCount flexInt `json:"alertCount,omitempty"`
	EventCount flexInt `json:"eventCount,omitempty"`
}

// Timeline is the bucketed alert/event activity over the query window.
type Timeline struct {
	Buckets    []TimelineBucket `json:"buckets,omitempty"`
	BucketSize string           `json:"bucketSize,omitempty"`
}

// WidgetMetadata is UI widget framing returned alongside the summary.
type WidgetMetadata struct {
	URI        string  `json:"uri,omitempty"`
	Detections flexInt `json:"detections,omitempty"`
	Total      flexInt `json:"total,omitempty"`
}

// PrevalenceData is one (time, count) prevalence sample for the entity.
type PrevalenceData struct {
	PrevalenceTime time.Time `json:"prevalenceTime,omitzero"`
	Count          flexInt   `json:"count,omitempty"`
}

// FileProperty is a single key/value file attribute.
type FileProperty struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// FilePropertyGroup is a titled group of file properties.
type FilePropertyGroup struct {
	Title      string         `json:"title,omitempty"`
	Properties []FileProperty `json:"properties,omitempty"`
}

// FileMetadataAndProperties holds file metadata and grouped properties; only
// populated for FILE entities (hash lookups).
type FileMetadataAndProperties struct {
	Metadata   []FileProperty      `json:"metadata,omitempty"`
	Properties []FilePropertyGroup `json:"properties,omitempty"`
	QueryState string              `json:"queryState,omitempty"`
}

// EntitySummary is the merged result of the query + by-ID + prevalence calls
// that SummarizeEntity issues, mirroring the wrapper's combined summary object.
type EntitySummary struct {
	PrimaryEntity             *Entity                    `json:"primaryEntity,omitempty"`
	RelatedEntities           []Entity                   `json:"relatedEntities,omitempty"`
	AlertCounts               []AlertCount               `json:"alertCounts,omitempty"`
	Timeline                  *Timeline                  `json:"timeline,omitempty"`
	WidgetMetadata            *WidgetMetadata            `json:"widgetMetadata,omitempty"`
	Prevalence                []PrevalenceData           `json:"prevalence,omitempty"`
	TPDPrevalence             []PrevalenceData           `json:"tpdPrevalence,omitempty"`
	FileMetadataAndProperties *FileMetadataAndProperties `json:"fileMetadataAndProperties,omitempty"`
	HasMoreAlerts             bool                       `json:"hasMoreAlerts,omitempty"`
	NextPageToken             string                     `json:"nextPageToken,omitempty"`
}

// --- value-type detection ---------------------------------------------------

var (
	reHash     = regexp.MustCompile(`^[a-fA-F0-9]{32}$|^[a-fA-F0-9]{40}$|^[a-fA-F0-9]{64}$`)
	reDomain   = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)+$`)
	reEmail    = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	reMAC      = regexp.MustCompile(`^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$`)
	reHostname = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`)
	reUsername = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
)

// detectEntityQuery maps a raw value to a UDM query fragment and the entity
// type that fragment naturally resolves to. Mirrors the wrapper's
// _detect_value_type_for_query (IP → ASSET, hash → FILE, domain → DOMAIN_NAME,
// email/username → USER, mac/hostname → ASSET, fallback → ASSET).
func detectEntityQuery(value string) (fragment, autoType string) {
	if net.ParseIP(value) != nil {
		return fmt.Sprintf("ip = %q", value), "ASSET"
	}
	if reHash.MatchString(value) {
		return fmt.Sprintf("hash = %q", value), "FILE"
	}
	if reDomain.MatchString(value) {
		return fmt.Sprintf("domain = %q", value), "DOMAIN_NAME"
	}
	if reEmail.MatchString(value) {
		return fmt.Sprintf("email = %q", value), "USER"
	}
	if reMAC.MatchString(value) {
		return fmt.Sprintf("mac = %q", value), "ASSET"
	}
	if reHostname.MatchString(value) {
		return fmt.Sprintf("hostname = %q", value), "ASSET"
	}
	if reUsername.MatchString(value) {
		return fmt.Sprintf("user.userid = %q", value), "USER"
	}
	return fmt.Sprintf("string_value = %q", value), "ASSET"
}

// --- SummarizeEntity --------------------------------------------------------

// SummarizeEntity returns a comprehensive summary for an entity value (IP,
// domain, file hash, user, etc.): related entities, alert counts, an activity
// timeline, and prevalence samples.
//
// It mirrors the wrapper's two-phase flow:
//  1. GET {instance}:summarizeEntitiesFromQuery to resolve the primary and
//     related entities for the value over [start, end].
//  2. GET {instance}:summarizeEntity?entityId=... with returnAlerts=true to
//     pull alert counts, the timeline, widget metadata, and (for hashes) file
//     metadata; then a second by-ID call with returnPrevalence=true for the
//     prevalence series.
//
// entityType is the preferred entity type to pick as primary among the
// resolved entities (e.g. "ASSET", "FILE", "DOMAIN_NAME", "USER"); pass "" to
// auto-detect from value. The primary falls back to the first resolved entity
// when no type matches.
//
// DEVIATION: the wrapper prints a warning and swallows prevalence failures. We
// instead treat a prevalence-call error as soft: the (alerts/timeline) summary
// is returned with empty prevalence rather than failing the whole call, and
// never print. Only the first (entity-resolution) call is hard-required.
func (c *Client) SummarizeEntity(ctx context.Context, entityType, value string, start, end time.Time) (*EntitySummary, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("chronicle: SummarizeEntity requires a non-empty value")
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("chronicle: SummarizeEntity start (%s) must be before end (%s)",
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}

	fragment, autoType := detectEntityQuery(value)
	preferred := entityType
	if preferred == "" {
		preferred = autoType
	}

	// Phase 1: resolve entities from the query.
	q := url.Values{
		"query":               {fragment},
		"timeRange.startTime": {summaryTimeFmt(start)},
		"timeRange.endTime":   {summaryTimeFmt(end)},
	}
	var queryResp struct {
		EntitySummaries []struct {
			Entity []Entity `json:"entity"`
		} `json:"entitySummaries"`
	}
	if err := c.get(ctx, c.instancePath(false)+":summarizeEntitiesFromQuery", &queryResp, withQuery(q)); err != nil {
		return nil, err
	}

	var all []Entity
	for _, s := range queryResp.EntitySummaries {
		all = append(all, s.Entity...)
	}

	// Pick the primary entity: first one matching the preferred type, else the
	// first entity overall.
	var primary *Entity
	for i := range all {
		if all[i].EntityType() == preferred {
			primary = &all[i]
			break
		}
	}
	if primary == nil && len(all) > 0 {
		primary = &all[0]
	}

	summary := &EntitySummary{PrimaryEntity: primary}
	if primary != nil {
		for i := range all {
			if &all[i] != primary {
				summary.RelatedEntities = append(summary.RelatedEntities, all[i])
			}
		}
	}
	if primary == nil {
		return summary, nil
	}

	primaryID := primary.EntityID()

	// Phase 2a: by-ID details with alerts (no prevalence).
	details, err := c.summarizeEntityByID(ctx, primaryID, start, end, true, false)
	if err != nil {
		return nil, err
	}
	summary.AlertCounts = details.AlertCounts
	summary.HasMoreAlerts = details.HasMoreAlerts
	summary.NextPageToken = details.NextPageToken
	if details.Timeline != nil && len(details.Timeline.Buckets) > 0 {
		summary.Timeline = details.Timeline
	}
	summary.WidgetMetadata = details.WidgetMetadata
	summary.FileMetadataAndProperties = details.FileMetadataAndProperties
	// If the by-ID call returned an updated copy of the primary entity, prefer it.
	if len(details.Entities) > 0 && details.Entities[0].Name == primary.Name {
		summary.PrimaryEntity = &details.Entities[0]
	}

	// Phase 2b: prevalence. For an IP value, prefer the IP_ADDRESS entity's ID,
	// matching the wrapper.
	prevalenceID := primaryID
	if net.ParseIP(value) != nil {
		for i := range all {
			if all[i].EntityType() == "IP_ADDRESS" {
				if id := all[i].EntityID(); id != "" {
					prevalenceID = id
				}
				break
			}
		}
	}
	if prev, err := c.summarizeEntityByID(ctx, prevalenceID, start, end, false, true); err == nil {
		summary.Prevalence = prev.PrevalenceResult
		summary.TPDPrevalence = prev.TPDPrevalenceResult
	}
	// DEVIATION: a prevalence error is non-fatal; the summary stands without it.

	return summary, nil
}

// entitySummaryByID is the decoded :summarizeEntity response. Fields are a
// superset across the alerts call and the prevalence call.
type entitySummaryByID struct {
	AlertCounts               []AlertCount               `json:"alertCounts"`
	HasMoreAlerts             bool                       `json:"hasMoreAlerts"`
	NextPageToken             string                     `json:"nextPageToken"`
	Timeline                  *Timeline                  `json:"timeline"`
	WidgetMetadata            *WidgetMetadata            `json:"widgetMetadata"`
	FileMetadataAndProperties *FileMetadataAndProperties `json:"fileMetadataAndProperties"`
	Entities                  []Entity                   `json:"entities"`
	PrevalenceResult          []PrevalenceData           `json:"prevalenceResult"`
	TPDPrevalenceResult       []PrevalenceData           `json:"tpdPrevalenceResult"`
}

// summarizeEntityByID fetches the by-ID :summarizeEntity payload, toggling the
// alert vs prevalence views. includeAllUdmEventTypesForFirstLastSeen is always
// true (matching the wrapper's default), and a large page size is requested.
func (c *Client) summarizeEntityByID(ctx context.Context, entityID string, start, end time.Time, returnAlerts, returnPrevalence bool) (*entitySummaryByID, error) {
	q := url.Values{
		"entityId":            {entityID},
		"timeRange.startTime": {summaryTimeFmt(start)},
		"timeRange.endTime":   {summaryTimeFmt(end)},
		"returnAlerts":        {fmt.Sprintf("%t", returnAlerts)},
		"returnPrevalence":    {fmt.Sprintf("%t", returnPrevalence)},
		"includeAllUdmEventTypesForFirstLastSeen": {"true"},
		"pageSize": {"1000"},
	}
	var resp entitySummaryByID
	if err := c.get(ctx, c.instancePath(false)+":summarizeEntity", &resp, withQuery(q)); err != nil {
		return nil, err
	}
	return &resp, nil
}
