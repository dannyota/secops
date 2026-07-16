package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// Retrohunts apply an existing detection rule to historical data over a chosen
// interval. They live under the owning rule and use the project ID (string)
// form in their resource path (numeric=false) — the wrapper builds every
// instance URL from the string project_id. See resource.go for why the form is
// explicit per endpoint.

// Interval is a [start, end) time window as the Chronicle API reports it, with
// RFC3339 timestamps.
type Interval struct {
	StartTime string `json:"startTime,omitempty"`
	EndTime   string `json:"endTime,omitempty"`
}

// Start parses StartTime as an RFC3339 time. The zero time is returned when the
// field is empty or unparseable.
func (i Interval) Start() time.Time { return parseRFC3339(i.StartTime) }

// End parses EndTime as an RFC3339 time. The zero time is returned when the
// field is empty or unparseable.
func (i Interval) End() time.Time { return parseRFC3339(i.EndTime) }

func parseRFC3339(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Retrohunt is a single retrohunt of a rule over historical data.
//
// Name is the resource name,
// projects/.../rules/ru_xxx/retrohunts/oh_xxxxxxxx; the trailing oh_xxx segment
// is the retrohunt (operation) ID the Get path expects (see RetrohuntID).
//
// State is one of the RETROHUNT_STATE_* enum strings (RUNNING / DONE /
// CANCELLED / FAILED). ProgressPercentage is a 0..100 completion figure the API
// reports as a float. ExecutionInterval is the historical window the rule is
// applied over.
type Retrohunt struct {
	Name               string   `json:"name,omitempty"`
	ExecutionInterval  Interval `json:"executionInterval,omitzero"`
	State              string   `json:"state,omitempty"`
	ProgressPercentage float64  `json:"progressPercentage,omitempty"`

	// Raw retains any fields not modeled above (e.g. an evolving operation
	// envelope). DEVIATION: the wrapper hands callers an untyped dict; we type
	// the stable fields and keep an escape hatch for the rest rather than
	// forcing map[string]any on every consumer.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and also stashes the full object in Raw
// so freeform/forward-compatible fields are not lost.
func (r *Retrohunt) UnmarshalJSON(b []byte) error {
	type alias Retrohunt // avoid recursing into this method
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*r = Retrohunt(a)
	r.Raw = append(r.Raw[:0:0], b...)
	return nil
}

// RetrohuntID returns the trailing oh_xxxxxxxx segment of the retrohunt's
// resource Name — the identifier GetRetrohunt's path expects.
func (r *Retrohunt) RetrohuntID() string {
	if r == nil || r.Name == "" {
		return ""
	}
	if i := strings.LastIndex(r.Name, "/retrohunts/"); i >= 0 {
		return r.Name[i+len("/retrohunts/"):]
	}
	return r.Name[strings.LastIndex(r.Name, "/")+1:]
}

// IsDone reports whether the retrohunt has reached a terminal state.
func (r *Retrohunt) IsDone() bool {
	switch strings.ToUpper(r.State) {
	case "RETROHUNT_STATE_DONE", "DONE",
		"RETROHUNT_STATE_CANCELLED", "CANCELLED",
		"RETROHUNT_STATE_FAILED", "FAILED":
		return true
	}
	return false
}

// CreateRetrohunt starts a retrohunt that applies rule ruleID to historical data
// in the [start, end) window. ruleID is the ru_xxxxxxxx segment (optionally with
// a version suffix). The returned Retrohunt reflects the newly created
// (typically still RUNNING) operation; poll it with GetRetrohunt.
func (c *Client) CreateRetrohunt(ctx context.Context, ruleID string, start, end time.Time) (*Retrohunt, error) {
	body := struct {
		ProcessInterval Interval `json:"processInterval"`
	}{
		ProcessInterval: Interval{
			StartTime: start.UTC().Format(time.RFC3339),
			EndTime:   end.UTC().Format(time.RFC3339),
		},
	}
	var rh Retrohunt
	path := c.resourcePath("rules/"+ruleID+"/retrohunts", false)
	if err := c.post(ctx, path, body, &rh); err != nil {
		return nil, err
	}
	return &rh, nil
}

// GetRetrohunt fetches a single retrohunt's status and progress. retrohuntID is
// the oh_xxxxxxxx segment; ruleID is the owning rule (optionally with a version
// suffix, "ru_xxx@v_<sec>_<nano>").
func (c *Client) GetRetrohunt(ctx context.Context, ruleID, retrohuntID string) (*Retrohunt, error) {
	var rh Retrohunt
	path := c.resourcePath("rules/"+ruleID+"/retrohunts/"+retrohuntID, false)
	if err := c.get(ctx, path, &rh); err != nil {
		return nil, err
	}
	return &rh, nil
}

// ListRetrohunts returns every retrohunt recorded for rule ruleID, following
// pagination to completion.
func (c *Client) ListRetrohunts(ctx context.Context, ruleID string) ([]Retrohunt, error) {
	var all []Retrohunt
	path := c.resourcePath("rules/"+ruleID+"/retrohunts", false)
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Retrohunts    []Retrohunt `json:"retrohunts"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if err := c.get(ctx, path, &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Retrohunts...)
		return resp.NextPageToken, nil
	})
	return all, err
}
