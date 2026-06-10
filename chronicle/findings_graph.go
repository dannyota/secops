package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// The findings graph (`{instance}/findingsGraph`) is the API behind the UI's
// graph-pivot investigation view: seed a graph from a detection, then expand
// nodes into related nodes/edges over a time range. Read-only; node ids are
// tied to the initializing time range.

// InitializeFindingsGraph seeds a graph from a detection id over [start, end)
// (findingsGraph:initializeGraph). The response carries the root node, the
// first page of nodes/edges, and a nextPageToken.
func (c *Client) InitializeFindingsGraph(ctx context.Context, detectionID string, start, end time.Time) (json.RawMessage, error) {
	if strings.TrimSpace(detectionID) == "" {
		return nil, fmt.Errorf("chronicle: detectionID is required")
	}
	q := url.Values{
		"nodeSource.detectionId": {detectionID},
		"timeRange.startTime":    {start.UTC().Format(time.RFC3339)},
		"timeRange.endTime":      {end.UTC().Format(time.RFC3339)},
	}
	var out json.RawMessage
	path := c.resourcePath("findingsGraph:initializeGraph", false)
	if err := c.get(ctx, path, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return out, nil
}

// ExploreFindingsGraphNode expands one node (or unpacks a group node) of an
// initialized graph (findingsGraph:exploreNode). q carries the documented
// query parameters verbatim — node ids come from a prior InitializeFindingsGraph
// with the SAME time range.
func (c *Client) ExploreFindingsGraphNode(ctx context.Context, q url.Values) (json.RawMessage, error) {
	var out json.RawMessage
	path := c.resourcePath("findingsGraph:exploreNode", false)
	if err := c.get(ctx, path, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return out, nil
}
