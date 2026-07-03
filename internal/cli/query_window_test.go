package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustTS(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func TestChunkWindow(t *testing.T) {
	day := 24 * time.Hour
	cases := []struct {
		name       string
		start, end string
		span       time.Duration
		want       int
	}{
		{"within span: single chunk", "2025-01-01T00:00:00Z", "2025-02-01T00:00:00Z", 90 * day, 1},
		{"exactly span: single chunk", "2025-01-01T00:00:00Z", "2025-04-01T00:00:00Z", 90 * day, 1},
		{"one second over: two chunks", "2025-01-01T00:00:00Z", "2025-04-01T00:00:01Z", 90 * day, 2},
		{"a year: five 90d chunks", "2025-01-01T00:00:00Z", "2026-01-01T00:00:00Z", 90 * day, 5},
		{"exact multiple: no empty tail", "2025-01-01T00:00:00Z", "2025-01-03T00:00:00Z", day, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := mustTS(t, tc.start), mustTS(t, tc.end)
			chunks := chunkWindow(start, end, tc.span)
			if len(chunks) != tc.want {
				t.Fatalf("got %d chunks, want %d: %+v", len(chunks), tc.want, chunks)
			}
			// Chunks tile [start, end) exactly: contiguous, ordered, within span.
			if !chunks[0].start.Equal(start) {
				t.Errorf("first chunk starts %v, want %v", chunks[0].start, start)
			}
			if !chunks[len(chunks)-1].end.Equal(end) {
				t.Errorf("last chunk ends %v, want %v", chunks[len(chunks)-1].end, end)
			}
			for i, c := range chunks {
				if !c.start.Before(c.end) {
					t.Errorf("chunk %d empty or inverted: %+v", i, c)
				}
				if c.end.Sub(c.start) > tc.span {
					t.Errorf("chunk %d wider than span: %v", i, c.end.Sub(c.start))
				}
				if i > 0 && !chunks[i-1].end.Equal(c.start) {
					t.Errorf("gap/overlap between chunk %d and %d", i-1, i)
				}
			}
		})
	}
}

func TestDedupeEventsByID(t *testing.T) {
	ev := func(shape, id string) json.RawMessage {
		return json.RawMessage(`{"` + shape + `":{"metadata":{"id":"` + id + `"}}}`)
	}
	events := []json.RawMessage{
		ev("udm", "a"),
		ev("udm", "b"),
		ev("udm", "a"),   // duplicate id, :udmSearch shape
		ev("event", "b"), // duplicate id, search-view shape
		ev("event", "c"),
		json.RawMessage(`{"udm":{}}`), // no id: kept
		json.RawMessage(`{"udm":{}}`), // no id: kept too
	}
	got := dedupeEventsByID(events)
	if len(got) != 5 {
		t.Fatalf("got %d events, want 5: %s", len(got), got)
	}
	wantIDs := []string{"a", "b", "c", "", ""}
	for i, e := range got {
		if id := eventID(e); id != wantIDs[i] {
			t.Errorf("event %d: id %q, want %q", i, id, wantIDs[i])
		}
	}
}

func TestMetaSidecarPath(t *testing.T) {
	cases := map[string]string{
		"evidence.jsonl":         "evidence.meta.json",
		"dir/login-events.jsonl": filepath.Join("dir", "login-events.meta.json"),
		"noext":                  "noext.meta.json",
		"weird.tar.gz":           "weird.tar.meta.json",
	}
	for in, want := range cases {
		if got := metaSidecarPath(in); got != want {
			t.Errorf("metaSidecarPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildEvidenceMeta(t *testing.T) {
	start := mustTS(t, "2025-01-01T00:00:00Z")
	end := mustTS(t, "2026-01-01T00:00:00Z")
	counts := []chunkCount{
		{From: "2025-01-01T00:00:00Z", To: "2025-04-01T00:00:00Z", Returned: 2, Total: 10},
		{From: "2025-04-01T00:00:00Z", To: "2025-06-30T00:00:00Z", Returned: 1, Total: 5},
	}
	total := 15
	m := buildEvidenceMeta(`metadata.event_type = "USER_LOGIN"`, start, end, 3, counts, &total)

	if m.From != "2025-01-01T00:00:00Z" || m.To != "2026-01-01T00:00:00Z" {
		t.Errorf("window: %s → %s", m.From, m.To)
	}
	if m.TotalCount == nil || *m.TotalCount != 15 || m.ReturnedCount != 3 {
		t.Errorf("counts: total=%v returned=%d", m.TotalCount, m.ReturnedCount)
	}
	if len(m.Chunks) != 2 {
		t.Errorf("chunks: %d, want 2", len(m.Chunks))
	}
	if m.SecopsctlVersion == "" {
		t.Error("version must never be empty")
	}
	if _, err := time.Parse(time.RFC3339, m.SavedAt); err != nil {
		t.Errorf("saved_at not RFC3339: %q", m.SavedAt)
	}

	// Single-chunk runs omit the chunks list; page path omits total_count.
	single := buildEvidenceMeta("q", start, end, 3, counts[:1], nil)
	if single.Chunks != nil {
		t.Error("single-chunk meta must omit chunks")
	}
	b, err := json.Marshal(single)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "total_count") || strings.Contains(string(b), "chunks") {
		t.Errorf("omitempty fields leaked: %s", b)
	}
}

func TestExtractJSONPath(t *testing.T) {
	var doc any
	raw := `{
		"protoPayload": {
			"methodName": "SetIamPolicy",
			"metadata": {
				"event": [
					{"parameter": [
						{"name": "client_id", "value": "abc123"},
						{"name": "scope", "multiStrValue": ["a", "b"]}
					]}
				]
			},
			"numeric": 42,
			"flag": true
		},
		"snake_case_key": {"innerValue": "x"}
	}`
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"protoPayload.methodName":                                 "SetIamPolicy",
		"protoPayload.metadata.event.0.parameter.0.name":          "client_id",
		"protoPayload.metadata.event.0.parameter.1.multiStrValue": "a,b",
		// Non-scalar leaves render as compact JSON (map keys re-sorted by Marshal).
		"protoPayload.metadata.event.0.parameter": `[{"name":"client_id","value":"abc123"},{"multiStrValue":["a","b"],"name":"scope"}]`,
		"protoPayload.numeric":                    "42",
		"protoPayload.flag":                       "true",
		"snake_case_key.inner_value":              "x", // snake→camel tolerance
		"protoPayload.missing":                    "",
		"protoPayload.metadata.event.5.parameter": "", // index out of range
		"protoPayload.metadata.event.x":           "", // non-numeric array segment
		"protoPayload.methodName.deeper":          "", // descend past a leaf
	}
	for path, want := range cases {
		if got := extractJSONPath(doc, path); got != want {
			t.Errorf("extractJSONPath(%q) = %q, want %q", path, got, want)
		}
	}
}
