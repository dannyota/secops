package chronicle

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// urlSafeEventID converts a standard-base64 event id (udm.metadata.id) to the
// URL-safe, unpadded base64 the {event} path segment requires.
func urlSafeEventID(id string) string {
	if b, err := base64.StdEncoding.DecodeString(id); err == nil {
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return strings.NewReplacer("+", "-", "/", "_").Replace(strings.TrimRight(id, "="))
}

// events.go is per-event / per-log drill-in: given an event id or search token
// (from a UDM search result), fetch the enriched UDM event, the unenriched UDM
// event(s), or — via the existing FindRawLogLines (legacy:legacyFindRawLogs) — the
// original raw log bytes. Both surfaces are SIEM-plane (chronicle host, ADC).

// EnrichedEvent is one enriched UDM event (geo / threat-intel / entity-graph
// fields layered onto the parsed event) plus per-field enrichment provenance.
type EnrichedEvent struct {
	// UDM is the enriched UDM event, kept raw (the SearchUDM []json.RawMessage
	// DEVIATION — UDM is large and evolving).
	UDM json.RawMessage `json:"udm"`
	// UDMEnrichedFields maps a UDM field path (e.g. "principal.ip") to the
	// source(s) the enrichment came from. Preferred over EnrichedFields.
	UDMEnrichedFields map[string]EnrichmentSources `json:"udmEnrichedFields,omitempty"`
	// EnrichedFields is the deprecated single-source map (server may still send
	// it); prefer UDMEnrichedFields.
	EnrichedFields map[string]EnrichingSource `json:"enrichedFields,omitempty"`
}

// EnrichmentSources is the set of sources for one enriched field.
type EnrichmentSources struct {
	Sources []EnrichingSource `json:"sources,omitempty"`
}

// EnrichingSource identifies where one enriched field's data originated. Exactly
// one of Event / Entity is set (the source union); both are resource names.
type EnrichingSource struct {
	DisplayName string `json:"displayName,omitempty"`
	Event       string `json:"event,omitempty"`
	Entity      string `json:"entity,omitempty"`
}

// FetchEnrichedEvent returns the enriched UDM event for a single event id. eventID
// is the unencoded base64 id from udm.metadata.id (or a search-result row id);
// detectionID is optional ("" to omit) — an event copied into a detection can
// carry different enrichment.
//
// Endpoint: GET {instance}/events/{urlencoded-id}:fetchEnrichedEvent (chronicle
// host, v1alpha; project ID form).
func (c *Client) FetchEnrichedEvent(ctx context.Context, eventID, detectionID string) (*EnrichedEvent, error) {
	if strings.TrimSpace(eventID) == "" {
		return nil, fmt.Errorf("chronicle: FetchEnrichedEvent requires an event id")
	}
	// The {event} path segment must be URL-safe, unpadded base64; the server 400s
	// on a standard-base64 id ("+"/"/"/"=") in the path. (Query-param ids elsewhere
	// — legacyFindUdmEvents/legacyFindRawLogs — take the standard form, so only this
	// path segment is converted.)
	path := c.instancePath(false) + "/events/" + urlSafeEventID(eventID) + ":fetchEnrichedEvent"
	var opts []requestOption
	if detectionID != "" {
		opts = append(opts, withQuery(url.Values{"detectionId": {detectionID}}))
	}
	var ev EnrichedEvent
	if err := c.get(ctx, path, &ev, opts...); err != nil {
		return nil, err
	}
	return &ev, nil
}

// FindUDMEventsResult holds the UDM event groups and entity groups from
// legacyFindUdmEvents. Each group aligns positionally with the ids/tokens in the
// request (group[i] ↔ ids[i] or tokens[i]); one id/token may map to several events.
type FindUDMEventsResult struct {
	UDMEventGroups []UDMEventGroup `json:"udmEventGroups,omitempty"`
	EntityGroups   []EntityGroup   `json:"entityGroups,omitempty"`
}

// UDMEventGroup is all UDM events for one token/id. Events kept raw (UDM DEVIATION).
type UDMEventGroup struct {
	Events []json.RawMessage `json:"events,omitempty"`
}

// EntityGroup is all UDM entity events for one token/id, kept raw.
type EntityGroup struct {
	Entities []json.RawMessage `json:"entities,omitempty"`
}

// UDMEvents flattens every UDMEventGroup into a single slice of raw UDM events
// (entity groups dropped) — the common "just give me the events" case.
func (r *FindUDMEventsResult) UDMEvents() []json.RawMessage {
	var out []json.RawMessage
	for _, g := range r.UDMEventGroups {
		out = append(out, g.Events...)
	}
	return out
}

// FindUDMEvents returns the UDM/entity events for a set of event ids OR search
// tokens (legacy:legacyFindUdmEvents). Pass EITHER ids OR tokens — when both are
// set the server uses ids and discards tokens. ids are unencoded base64 event ids
// (udm.metadata.id); tokens are eventLogToken values from a search-view result.
// returnUnenriched=true asks for the as-parsed UDM (what the console row drill-in
// shows); false returns the server default.
//
// Endpoint: GET {instance}/legacy:legacyFindUdmEvents (chronicle host, v1alpha;
// project NUMBER form, matching its sibling legacy:legacyFindRawLogs).
func (c *Client) FindUDMEvents(ctx context.Context, ids, tokens []string, returnUnenriched bool) (*FindUDMEventsResult, error) {
	q := url.Values{}
	for _, id := range ids {
		if id != "" {
			q.Add("ids", id)
		}
	}
	for _, t := range tokens {
		if t != "" {
			q.Add("tokens", t)
		}
	}
	if len(q["ids"]) == 0 && len(q["tokens"]) == 0 {
		return nil, fmt.Errorf("chronicle: FindUDMEvents requires at least one id or token")
	}
	if returnUnenriched {
		q.Set("returnUnenrichedData", "true")
	}

	var raw json.RawMessage
	if err := c.get(ctx, c.resourcePath("legacy:legacyFindUdmEvents", true), &raw, withQuery(q)); err != nil {
		return nil, err
	}
	// Defensive: a single object normally, but the legacy family can stream a
	// JSON array of chunks — merge the groups across chunks if so.
	chunks, err := decodeStreamChunks[FindUDMEventsResult](raw)
	if err != nil {
		return nil, fmt.Errorf("chronicle: decode UDM events: %w", err)
	}
	out := &FindUDMEventsResult{}
	for _, ch := range chunks {
		out.UDMEventGroups = append(out.UDMEventGroups, ch.UDMEventGroups...)
		out.EntityGroups = append(out.EntityGroups, ch.EntityGroups...)
	}
	return out, nil
}
