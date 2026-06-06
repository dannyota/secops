package legacy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"danny.vn/secops/auth"
)

// captureRT records the request body and returns a canned response.
type captureRT struct {
	body string
	resp string
}

func (r *captureRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		r.body = string(b)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(r.resp)),
		Header:     make(http.Header),
	}, nil
}

func newCaptureClient(rt http.RoundTripper) *Client {
	return NewClient(
		Settings{BaseURL: "https://t.example.com", ProjectNumber: "0", Region: "us", CustomerID: "c"},
		auth.SOARAppKey("k"), &http.Client{Transport: rt})
}

// TestCreateManualCaseForcesEmptyCollections locks the fix: nil Entities/Playbooks/
// Tags must serialize as [] (the legacy server NPEs on null), and the integer case
// id in the response is parsed and returned.
func TestCreateManualCaseForcesEmptyCollections(t *testing.T) {
	rt := &captureRT{resp: "4361"}
	c := newCaptureClient(rt)

	id, err := c.CreateManualCase(context.Background(), ManualCaseRequest{
		Title: "x", AssignedUser: "@Tier1", Priority: 40, Environment: "Default Environment",
		AlertName: "x", OccurenceTime: "2026-01-02T03:04:05Z",
		// Entities / Playbooks / Tags intentionally left nil.
	})
	if err != nil {
		t.Fatalf("CreateManualCase: %v", err)
	}
	if id != 4361 {
		t.Errorf("id = %d, want 4361", id)
	}

	var sent map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rt.body), &sent); err != nil {
		t.Fatalf("request body not JSON: %v\n%s", err, rt.body)
	}
	for _, k := range []string{"entities", "playbooks", "tags"} {
		v, ok := sent[k]
		if !ok {
			t.Errorf("%s missing from body (must be sent as [])", k)
			continue
		}
		if string(v) != "[]" {
			t.Errorf("%s = %s, want [] (server NPEs on null)", k, v)
		}
	}
	if string(sent["assignedUser"]) != `"@Tier1"` {
		t.Errorf("assignedUser = %s, want \"@Tier1\"", sent["assignedUser"])
	}
}
