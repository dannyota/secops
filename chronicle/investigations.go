package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Investigations use the project ID (string) form in their resource path
// (numeric=false): the wrapper builds every investigations URL from the string
// project_id (see investigations.py / resource.go for why the form is explicit
// per endpoint).

// DetectionType identifies what kind of identifier an investigation query is
// keyed on. It mirrors the wrapper's DetectionType enum.
type DetectionType string

const (
	DetectionTypeUnspecified DetectionType = "DETECTION_TYPE_UNSPECIFIED"
	DetectionTypeAlert       DetectionType = "DETECTION_TYPE_ALERT"
	DetectionTypeCase        DetectionType = "DETECTION_TYPE_CASE"
)

// normalize coerces loose detection-type input (bare "ALERT", "alert",
// "DETECTION_TYPE_ALERT", or the wrapper's enum value) into the canonical
// DETECTION_TYPE_* string the API expects. Unknown values are returned
// upper-cased and untouched so the server can reject them with a clean
// *APIError rather than us guessing.
//
// DEVIATION: the wrapper raises a local ValueError on unrecognized input; we
// pass the (upper-cased) value through and let the API be the source of truth,
// which keeps the SDK forward-compatible with new detection types.
func (d DetectionType) normalize() DetectionType {
	s := strings.ToUpper(strings.TrimSpace(string(d)))
	switch s {
	case "", string(DetectionTypeUnspecified), "UNSPECIFIED":
		return DetectionTypeUnspecified
	case string(DetectionTypeAlert), "ALERT":
		return DetectionTypeAlert
	case string(DetectionTypeCase), "CASE":
		return DetectionTypeCase
	default:
		return DetectionType(s)
	}
}

// Investigation is a SecOps investigation resource.
//
// The Chronicle investigations API is in v1alpha and its response shape is only
// loosely documented; the well-known identifying fields are typed here and the
// complete server object is preserved in Raw for callers that need fields this
// struct does not (yet) name.
type Investigation struct {
	// Name is the full resource name,
	// projects/.../instances/.../investigations/<id>.
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Etag        string `json:"etag"`
	CreateTime  string `json:"createTime"`
	UpdateTime  string `json:"updateTime"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`

	// Lifecycle + triage fields of the per-alert AI (TIN) investigation flow.
	// Status is STATUS_IN_PROGRESS while the agent works; the verdict fields
	// below populate once it reaches STATUS_COMPLETED_*.
	Status      string `json:"status"`
	TriggerType string `json:"triggerType"`
	// Notebook is the resource name of the agent's working document,
	// {instance}/notebooks/<id> — fetch it with GetNotebook.
	Notebook string `json:"notebook"`

	// Verdict (e.g. FALSE_POSITIVE), Confidence (e.g. HIGH_CONFIDENCE), the
	// markdown Summary, and suggested NextSteps — the completed result.
	Verdict     string                  `json:"verdict"`
	Confidence  string                  `json:"confidence"`
	Summary     string                  `json:"summary"`
	NextSteps   []InvestigationNextStep `json:"nextSteps"`
	PublishTime string                  `json:"publishTime"`

	// Raw is the verbatim server object, so no field is lost to the typed view
	// (embedded investigationSteps, alert ids, time ranges, …).
	Raw json.RawMessage `json:"-"`
}

// InvestigationNextStep is one suggested follow-up on a completed
// investigation. Type is SEARCHABLE (backed by a UDM query) or MANUAL.
type InvestigationNextStep struct {
	Title string `json:"title"`
	Type  string `json:"type"`
}

// UnmarshalJSON captures the typed fields and also retains the full object in
// Raw without recursing.
func (i *Investigation) UnmarshalJSON(b []byte) error {
	type alias Investigation
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*i = Investigation(a)
	i.Raw = append(i.Raw[:0], b...)
	return nil
}

// InvestigationID returns the trailing identifier segment of the investigation's
// resource Name (the form GetInvestigation expects).
func (i *Investigation) InvestigationID() string {
	if i == nil || i.Name == "" {
		return ""
	}
	return i.Name[strings.LastIndex(i.Name, "/")+1:]
}

// Completed reports whether the investigation has finished — successfully or
// not (status STATUS_COMPLETED_*).
func (i *Investigation) Completed() bool {
	return i != nil && strings.HasPrefix(i.Status, "STATUS_COMPLETED")
}

// NotebookID returns the trailing id segment of the Notebook resource name
// (the form GetNotebook expects), or "" when the investigation has none.
func (i *Investigation) NotebookID() string {
	if i == nil || i.Notebook == "" {
		return ""
	}
	return i.Notebook[strings.LastIndex(i.Notebook, "/")+1:]
}

// investigationID extracts the bare ID from either a plain ID or a full
// resource name, mirroring the wrapper's format_resource_id.
func investigationID(idOrName string) string {
	if strings.HasPrefix(idOrName, "projects/") {
		return idOrName[strings.LastIndex(idOrName, "/")+1:]
	}
	return idOrName
}

// ListInvestigations returns investigations for the instance. pageSize bounds
// the per-page request size (the API default is 100, max 1000); pass 0 to use
// the server default. All pages are aggregated.
func (c *Client) ListInvestigations(ctx context.Context, pageSize int) ([]Investigation, error) {
	return c.listInvestigations(ctx, pageSize, "", "")
}

// ListInvestigationsFiltered is ListInvestigations with the optional filter
// expression and orderBy. The filtered surface speaks snake_case (e.g. filter
// "alert_id='de_…' AND latest_in_alert=true", orderBy "start_time desc").
// Either may be empty.
func (c *Client) ListInvestigationsFiltered(ctx context.Context, pageSize int, filter, orderBy string) ([]Investigation, error) {
	return c.listInvestigations(ctx, pageSize, filter, orderBy)
}

func (c *Client) listInvestigations(ctx context.Context, pageSize int, filter, orderBy string) ([]Investigation, error) {
	var all []Investigation
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{}
		if pageSize > 0 {
			q.Set("pageSize", strconv.Itoa(pageSize))
		}
		if filter != "" {
			q.Set("filter", filter)
		}
		if orderBy != "" {
			q.Set("orderBy", orderBy)
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Investigations []Investigation `json:"investigations"`
			NextPageToken  string          `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("investigations", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Investigations...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetInvestigation fetches a single investigation. id may be a bare ID or a full
// resource name (the latter is reduced to its final segment).
func (c *Client) GetInvestigation(ctx context.Context, id string) (*Investigation, error) {
	var inv Investigation
	if err := c.get(ctx, c.resourcePath("investigations/"+investigationID(id), false), &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// TriggerInvestigation kicks off an investigation for a single alert and returns
// the created investigation. RPC-style method investigations:trigger on the
// instance.
func (c *Client) TriggerInvestigation(ctx context.Context, alertID string) (*Investigation, error) {
	body := struct {
		AlertID string `json:"alertId"`
	}{AlertID: alertID}
	var inv Investigation
	if err := c.post(ctx, c.resourcePath("investigations:trigger", false), body, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// AssociatedInvestigations is the result of FetchAssociatedInvestigations: a map
// from each queried alert/case ID to its associated investigations, plus a map
// flagging which inputs are experimental. Both fields are freeform JSON because
// the v1alpha schema for the per-key value objects is unstable.
type AssociatedInvestigations struct {
	// AssociationsList maps alert/case ID -> list of associated investigations.
	AssociationsList map[string]json.RawMessage `json:"associationsList,omitempty"`
	// ExperimentalAlert maps alert/case ID -> experimental flag.
	ExperimentalAlert map[string]json.RawMessage `json:"experimentalAlert,omitempty"`
}

// FetchAssociatedInvestigations returns the investigations associated with the
// given alert and/or case IDs. detectionType declares which identifiers are
// supplied (ALERT or CASE; loose forms are normalized). Up to 100 alertIDs and
// 100 caseIDs are accepted. limit caps associations returned per detection
// (server default 1, max 5); pass 0 to use the server default.
//
// DEVIATION: limit maps to the wrapper's association_limit_per_detection; the
// orderBy knob is exposed via FetchAssociatedInvestigationsOrdered to keep this
// signature aligned with the task contract.
func (c *Client) FetchAssociatedInvestigations(ctx context.Context, detectionType string, alertIDs, caseIDs []string, limit int) (*AssociatedInvestigations, error) {
	return c.fetchAssociatedInvestigations(ctx, DetectionType(detectionType), alertIDs, caseIDs, limit, "")
}

// FetchAssociatedInvestigationsOrdered is FetchAssociatedInvestigations with the
// wrapper's optional orderBy (e.g. "createTime", "createTime desc",
// "updateTime", "updateTime desc").
func (c *Client) FetchAssociatedInvestigationsOrdered(ctx context.Context, detectionType string, alertIDs, caseIDs []string, limit int, orderBy string) (*AssociatedInvestigations, error) {
	return c.fetchAssociatedInvestigations(ctx, DetectionType(detectionType), alertIDs, caseIDs, limit, orderBy)
}

func (c *Client) fetchAssociatedInvestigations(ctx context.Context, detectionType DetectionType, alertIDs, caseIDs []string, limit int, orderBy string) (*AssociatedInvestigations, error) {
	q := url.Values{}
	q.Set("detectionType", string(detectionType.normalize()))
	// Repeated query params: the API expects alertIds/caseIds as repeated keys.
	for _, a := range alertIDs {
		q.Add("alertIds", a)
	}
	for _, cs := range caseIDs {
		q.Add("caseIds", cs)
	}
	if limit > 0 {
		q.Set("associationLimitPerDetection", strconv.Itoa(limit))
	}
	if orderBy != "" {
		q.Set("orderBy", orderBy)
	}

	var out AssociatedInvestigations
	if err := c.get(ctx, c.resourcePath("investigations:fetchAssociated", false), &out, withQuery(q)); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetNotebook fetches an investigation's notebook — the TIN agent's working
// document for the investigation (`{instance}/notebooks/{id}`; confirmed
// against the web UI's request, the resource is absent from the public REST
// index). The notebook id is referenced from the investigation record.
func (c *Client) GetNotebook(ctx context.Context, id string) (json.RawMessage, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("chronicle: notebook id is required")
	}
	var out json.RawMessage
	if err := c.get(ctx, c.resourcePath("notebooks/"+id, false), &out); err != nil {
		return nil, err
	}
	return out, nil
}
