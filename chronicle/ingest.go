package chronicle

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// Ingestion endpoints write events/logs/entities into the SIEM. All three hit
// the instance import RPCs and use the project ID (string) form in their
// resource path (numeric=false), matching the wrapper, whose SDK builds every
// instance URL from the string project_id. See resource.go.

// --- raw log ingestion ------------------------------------------------------

// ingestLogEntry is one entry in a logs:import request. The JSON keys are
// snake_case because the SecOps logs:import RPC expects the proto field names in
// that form (see the wrapper's ingest_log).
type ingestLogEntry struct {
	Data                 string              `json:"data"` // base64-encoded raw log
	LogEntryTime         string              `json:"log_entry_time,omitempty"`
	CollectionTime       string              `json:"collection_time,omitempty"`
	EnvironmentNamespace string              `json:"environment_namespace,omitempty"`
	Labels               map[string]logLabel `json:"labels,omitempty"`
}

// logLabel wraps a custom-metadata value, matching the LogLabel proto shape the
// API requires ({"value": "..."}) rather than a bare string.
type logLabel struct {
	Value string `json:"value"`
}

// Forwarder is a Chronicle log forwarder. Only the fields this SDK needs are
// modeled; Config is preserved as a freeform blob.
type Forwarder struct {
	Name        string          `json:"name,omitempty"`
	DisplayName string          `json:"displayName,omitempty"`
	Config      json.RawMessage `json:"config,omitempty"`
}

// defaultForwarderDisplayName is the forwarder this SDK gets-or-creates when a
// caller ingests a raw log without naming one, mirroring the wrapper's default.
const defaultForwarderDisplayName = "Wrapper-SDK-Forwarder"

// IngestLog ingests a single raw log of the given log type (e.g. "OKTA",
// "WINDOWS"). message is the raw log text; it is base64-encoded for transport.
// labels are attached as custom metadata. ts is the log-entry time (and is also
// used as the collection time); a zero ts defaults to now.
//
// A raw-log import requires a forwarder resource, so this resolves (or creates)
// a default forwarder once per call.
//
// DEVIATION: the wrapper auto-splits a multi-log string by log-type-specific
// heuristics (JSON/Windows/XML splitters). This method ingests exactly the one
// message it is given; callers that have multiple logs should call per log (or
// use a future batch helper). That keeps the SDK pure and predictable.
func (c *Client) IngestLog(ctx context.Context, logType, message string, labels map[string]string, ts time.Time) error {
	if logType == "" {
		return fmt.Errorf("chronicle: IngestLog: logType is required")
	}
	if ts.IsZero() {
		ts = time.Now()
	}
	// API timestamp form: RFC3339 with fractional seconds + Z (UTC).
	tstr := ts.UTC().Format("2006-01-02T15:04:05.000000Z")

	fwd, err := c.getOrCreateForwarder(ctx, defaultForwarderDisplayName)
	if err != nil {
		return err
	}

	entry := ingestLogEntry{
		Data:           base64.StdEncoding.EncodeToString([]byte(message)),
		LogEntryTime:   tstr,
		CollectionTime: tstr,
	}
	if len(labels) > 0 {
		entry.Labels = make(map[string]logLabel, len(labels))
		for k, v := range labels {
			entry.Labels[k] = logLabel{Value: v}
		}
	}

	body := struct {
		InlineSource struct {
			Logs      []ingestLogEntry `json:"logs"`
			Forwarder string           `json:"forwarder"`
		} `json:"inline_source"`
	}{}
	body.InlineSource.Logs = []ingestLogEntry{entry}
	body.InlineSource.Forwarder = fwd.Name

	path := c.resourcePath("logTypes/"+logType+"/logs:import", false)
	return c.post(ctx, path, body, nil)
}

// getOrCreateForwarder returns the forwarder with the given display name,
// creating it if absent. Forwarders are required to ingest raw logs.
func (c *Client) getOrCreateForwarder(ctx context.Context, displayName string) (*Forwarder, error) {
	var found *Forwarder
	err := paginate(50, func(token string) (string, error) {
		q := map[string][]string{"pageSize": {"1000"}}
		if token != "" {
			q["pageToken"] = []string{token}
		}
		var resp struct {
			Forwarders    []Forwarder `json:"forwarders"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("forwarders", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		for i := range resp.Forwarders {
			if resp.Forwarders[i].DisplayName == displayName {
				f := resp.Forwarders[i]
				found = &f
				return "", nil // stop paginating
			}
		}
		return resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	if found != nil {
		return found, nil
	}

	// Not found: create a minimal forwarder with the requested display name.
	create := struct {
		DisplayName string `json:"displayName"`
		Config      struct {
			UploadCompression bool           `json:"uploadCompression"`
			Metadata          map[string]any `json:"metadata"`
			ServerSettings    map[string]any `json:"serverSettings"`
		} `json:"config"`
	}{DisplayName: displayName}
	create.Config.Metadata = map[string]any{}
	create.Config.ServerSettings = map[string]any{"enabled": false}

	var created Forwarder
	if err := c.post(ctx, c.resourcePath("forwarders", false), create, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// --- UDM event ingestion ----------------------------------------------------

// IngestUDM ingests one or more UDM events directly (no parsing). Each event is
// a freeform UDM JSON object. Events missing metadata.event_timestamp or
// metadata.id are filled in (timestamp = now, id = a random UUID) so the import
// is accepted, matching the wrapper's add_missing_ids behavior.
func (c *Client) IngestUDM(ctx context.Context, events ...json.RawMessage) error {
	if len(events) == 0 {
		return fmt.Errorf("chronicle: IngestUDM: no events provided")
	}

	type wrapped struct {
		UDM json.RawMessage `json:"udm"`
	}
	out := make([]wrapped, 0, len(events))
	for i, ev := range events {
		filled, err := ensureUDMMetadata(ev)
		if err != nil {
			return fmt.Errorf("chronicle: IngestUDM: event %d: %w", i, err)
		}
		out = append(out, wrapped{UDM: filled})
	}

	body := struct {
		InlineSource struct {
			Events []wrapped `json:"events"`
		} `json:"inline_source"`
	}{}
	body.InlineSource.Events = out

	return c.post(ctx, c.resourcePath("events:import", false), body, nil)
}

// ensureUDMMetadata returns ev with metadata.event_timestamp and metadata.id
// populated when missing. It round-trips through a generic map only to inject
// the two defaults, preserving every other field verbatim.
func ensureUDMMetadata(ev json.RawMessage) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(ev, &m); err != nil {
		return nil, fmt.Errorf("event must be a JSON object: %w", err)
	}
	var meta map[string]json.RawMessage
	if raw, ok := m["metadata"]; ok {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("metadata must be a JSON object: %w", err)
		}
	} else {
		meta = map[string]json.RawMessage{}
	}

	changed := false
	if _, ok := meta["event_timestamp"]; !ok {
		ts, _ := json.Marshal(time.Now().UTC().Format(time.RFC3339))
		meta["event_timestamp"] = ts
		changed = true
	}
	if _, ok := meta["id"]; !ok {
		id, _ := json.Marshal(newUUID())
		meta["id"] = id
		changed = true
	}
	if !changed {
		return ev, nil
	}

	metaRaw, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	m["metadata"] = metaRaw
	return json.Marshal(m)
}

// newUUID returns a random RFC 4122 version-4 UUID string. We generate one
// locally rather than pull a dependency for the single ID-filling use here.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// --- entity import ----------------------------------------------------------

// ImportEntities imports entity graph data derived from the given log type.
// Each entity is a freeform entity JSON object. logType identifies the log the
// entities were derived from (required by the API).
func (c *Client) ImportEntities(ctx context.Context, logType string, entities []json.RawMessage) error {
	if logType == "" {
		return fmt.Errorf("chronicle: ImportEntities: logType is required")
	}
	if len(entities) == 0 {
		return fmt.Errorf("chronicle: ImportEntities: no entities provided")
	}

	body := struct {
		InlineSource struct {
			Entities []json.RawMessage `json:"entities"`
			LogType  string            `json:"log_type"`
		} `json:"inline_source"`
	}{}
	body.InlineSource.Entities = entities
	body.InlineSource.LogType = logType

	return c.post(ctx, c.resourcePath("entities:import", false), body, nil)
}
