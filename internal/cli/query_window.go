package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"danny.vn/secops/chronicle"
)

// query_window.go splits wide search windows into API-sized chunks and runs the
// per-chunk fetch loops behind `search udm` (events, --all, --count-only, --raw),
// plus the --meta evidence sidecar describing a saved result set.

// searchWindowCap is the widest time range one UDM search request accepts.
// A wider --from/--to window is searched in sequential half-open chunks of at
// most this span and the results merged (per-chunk counts on stderr).
const searchWindowCap = 90 * 24 * time.Hour

// searchWindow is one half-open [start, end) search window.
type searchWindow struct{ start, end time.Time }

// chunkWindow splits [start, end) into sequential half-open windows of at most
// span each; a range within span yields the single original window.
func chunkWindow(start, end time.Time, span time.Duration) []searchWindow {
	var out []searchWindow
	for s := start; s.Before(end); s = s.Add(span) {
		e := s.Add(span)
		if e.After(end) {
			e = end
		}
		out = append(out, searchWindow{s, e})
	}
	return out
}

// announceChunks reports a multi-chunk search plan on stderr.
func announceChunks(chunks []searchWindow) {
	if len(chunks) < 2 {
		return
	}
	fmt.Fprintf(os.Stderr, "note: window exceeds the %d-day search cap — searching %d chunks sequentially.\n",
		int(searchWindowCap.Hours()/24), len(chunks))
}

// chunkLabel renders one chunk's window for progress lines.
func chunkLabel(w searchWindow) string {
	return w.start.Format(time.RFC3339) + " → " + w.end.Format(time.RFC3339)
}

// chunkCount records one chunk's outcome, for per-chunk reporting and the
// --meta sidecar.
type chunkCount struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Returned int    `json:"returned"`
	Total    int    `json:"total,omitempty"` // baseline matches (complete-results engine only)
}

// wrapChunkErr labels a failed chunk with its position and window — but only
// when the search actually ran chunked, so single-window errors stay verbatim.
func wrapChunkErr(err error, i int, chunks []searchWindow) error {
	if len(chunks) < 2 {
		return err
	}
	return fmt.Errorf("chunk %d/%d (%s): %w", i+1, len(chunks), chunkLabel(chunks[i]), err)
}

// fetchEventsPaged runs the plain :udmSearch page per chunk, accumulating up to
// limit events. truncated reports whether any chunk had more matches than fetched
// (server moreDataAvailable, or the overall limit cut the chunk loop short).
func fetchEventsPaged(ctx context.Context, c *chronicle.Client, filter string, chunks []searchWindow, limit int) (events []json.RawMessage, counts []chunkCount, truncated bool, err error) {
	counts = make([]chunkCount, 0, len(chunks))
	for i, w := range chunks {
		remaining := limit - len(events)
		if remaining <= 0 {
			truncated = true
			break
		}
		if len(chunks) == 1 {
			printProgress("events", 0, 0)
		}
		evs, more, err := c.SearchUDMPage(ctx, filter, w.start, w.end, remaining)
		if err != nil {
			clearProgress()
			return nil, nil, false, wrapChunkErr(err, i, chunks)
		}
		if len(chunks) == 1 {
			clearProgress()
		}
		truncated = truncated || more
		events = append(events, evs...)
		counts = append(counts, chunkCount{
			From: w.start.Format(time.RFC3339), To: w.end.Format(time.RFC3339), Returned: len(evs),
		})
		if len(chunks) > 1 {
			fmt.Fprintf(os.Stderr, "chunk %d/%d (%s): %d event(s)\n", i+1, len(chunks), chunkLabel(w), len(evs))
		}
	}
	if len(chunks) > 1 {
		events = dedupeEventsByID(events)
	}
	return events, counts, truncated, nil
}

// fetchEventsComplete runs the complete-results engine per chunk, accumulating
// events up to maxEvents total and summing the baseline match counts. With
// maxEvents 0 it degrades to a pure count probe (one event requested per chunk,
// none kept) — the --count-only path.
func fetchEventsComplete(ctx context.Context, c *chronicle.Client, filter string, chunks []searchWindow, maxEvents int) (events []json.RawMessage, counts []chunkCount, total int, err error) {
	counts = make([]chunkCount, 0, len(chunks))
	for i, w := range chunks {
		remaining := maxEvents - len(events)
		// Single-chunk searches block in one long HTTP request; show elapsed
		// time so it doesn't read as a hang.
		var stopTicker func()
		if len(chunks) == 1 {
			stopTicker = progressTicker("events")
		}
		view, err := c.FetchUDMSearchView(ctx, filter, w.start, w.end, chronicle.UDMSearchViewOptions{
			// The baseline count is computed server-side regardless of how many
			// events are returned, so a spent limit still counts the chunk.
			MaxEvents:       max(remaining, 1),
			CaseInsensitive: true,
		})
		if stopTicker != nil {
			stopTicker()
		}
		if err != nil {
			return nil, nil, 0, wrapChunkErr(err, i, chunks)
		}
		returned := 0
		if remaining > 0 {
			events = append(events, view.Events...)
			returned = len(view.Events)
		}
		total += view.BaselineEventsCount
		counts = append(counts, chunkCount{
			From: w.start.Format(time.RFC3339), To: w.end.Format(time.RFC3339),
			Returned: returned, Total: view.BaselineEventsCount,
		})
		if len(chunks) > 1 {
			fmt.Fprintf(os.Stderr, "chunk %d/%d (%s): %d match(es)\n", i+1, len(chunks), chunkLabel(w), view.BaselineEventsCount)
		}
	}
	if len(chunks) > 1 {
		events = dedupeEventsByID(events)
	}
	return events, counts, total, nil
}

// eventID lifts an event's udm.metadata.id, probing both element shapes the two
// search engines emit ({"udm":{…}} from :udmSearch, {"event":{…}} from the
// search view). Empty when the event carries no id.
func eventID(ev json.RawMessage) string {
	var d struct {
		UDM struct {
			Metadata struct {
				ID string `json:"id"`
			} `json:"metadata"`
		} `json:"udm"`
		Event struct {
			Metadata struct {
				ID string `json:"id"`
			} `json:"metadata"`
		} `json:"event"`
	}
	if json.Unmarshal(ev, &d) != nil {
		return ""
	}
	return firstNonEmpty(d.UDM.Metadata.ID, d.Event.Metadata.ID)
}

// dedupeEventsByID drops duplicate events across chunk boundaries (an event
// falling exactly on a boundary can match two adjacent windows). Events without
// an id are kept as-is.
func dedupeEventsByID(events []json.RawMessage) []json.RawMessage {
	seen := make(map[string]struct{}, len(events))
	out := events[:0]
	for _, e := range events {
		if id := eventID(e); id != "" {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
		}
		out = append(out, e)
	}
	return out
}

// fetchRawLinesProgress hydrates raw logs in slices, reporting progress on
// stderr for large sets so a multi-second fetch doesn't read as a hang.
func fetchRawLinesProgress(ctx context.Context, c *chronicle.Client, ids []string) ([]chronicle.RawLogLine, error) {
	const slice = 100
	if len(ids) <= slice {
		return c.FindRawLogLines(ctx, ids)
	}
	out := make([]chronicle.RawLogLine, 0, len(ids))
	for start := 0; start < len(ids); start += slice {
		end := min(start+slice, len(ids))
		lines, err := c.FindRawLogLines(ctx, ids[start:end])
		if err != nil {
			clearProgress()
			return nil, err
		}
		out = append(out, lines...)
		printProgress("raw log", end, len(ids))
	}
	clearProgress()
	return out, nil
}

// printCountOnly emits the total match count: the bare number on stdout (per-
// chunk counts already went to stderr), or a structured envelope under --json.
func printCountOnly(total int, counts []chunkCount) error {
	if jsonOut {
		env := struct {
			Total  int          `json:"total"`
			Chunks []chunkCount `json:"chunks,omitempty"`
		}{Total: total}
		if len(counts) > 1 {
			env.Chunks = counts
		}
		return emitJSON(env)
	}
	fmt.Println(total)
	return nil
}

// evidenceMeta is the --meta sidecar: the provenance of a saved result set, so
// an evidence file records what produced it without a hand-written note.
type evidenceMeta struct {
	Query            string       `json:"query"`
	From             string       `json:"from"`
	To               string       `json:"to"`
	TotalCount       *int         `json:"total_count,omitempty"` // baseline matches (--all only)
	ReturnedCount    int          `json:"returned_count"`
	Chunks           []chunkCount `json:"chunks,omitempty"` // present when the window was chunked
	SavedAt          string       `json:"saved_at"`
	SecopsctlVersion string       `json:"secopsctl_version"`
}

// buildEvidenceMeta assembles the sidecar. total is nil on the plain page path
// (only the complete-results engine reports a baseline total).
func buildEvidenceMeta(query string, start, end time.Time, returned int, counts []chunkCount, total *int) evidenceMeta {
	m := evidenceMeta{
		Query:            query,
		From:             start.Format(time.RFC3339),
		To:               end.Format(time.RFC3339),
		TotalCount:       total,
		ReturnedCount:    returned,
		SavedAt:          time.Now().UTC().Format(time.RFC3339),
		SecopsctlVersion: resolveBuildInfo().Version,
	}
	if len(counts) > 1 {
		m.Chunks = counts
	}
	return m
}

// metaSidecarPath derives the sidecar path for an --out file: the extension is
// replaced by .meta.json (evidence.jsonl → evidence.meta.json).
func metaSidecarPath(out string) string {
	return strings.TrimSuffix(out, filepath.Ext(out)) + ".meta.json"
}

// writeMetaSidecar writes the sidecar next to the --out file.
func writeMetaSidecar(out string, meta evidenceMeta) error {
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	path := metaSidecarPath(out)
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("write --meta sidecar: %w", err)
	}
	fmt.Fprintf(os.Stderr, "meta sidecar written to %s\n", path)
	return nil
}
