package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

// Parser is a log-type parser configuration on the Chronicle instance.
//
// CBN holds the parser source (the "Config-Based Normalizer") as the API
// returns it: base64-encoded. The mirror layer decodes it to a .conf file; the
// SDK keeps it raw so callers decide when to decode.
//
// Creator and VersionInfo are intentionally freeform: they are small,
// loosely-specified metadata objects (creator.source, versionInfo.version,
// versionInfo.rollbackAvailable, ...) whose shapes the API does not firmly
// commit to. Everything load-bearing for pull/push is a typed scalar field.
type Parser struct {
	Name         string         `json:"name"`
	State        string         `json:"state"`
	Type         string         `json:"type"`
	ReleaseStage string         `json:"releaseStage,omitempty"`
	CreateTime   string         `json:"createTime,omitempty"`
	CBN          string         `json:"cbn,omitempty"` // base64-encoded parser source
	Creator      map[string]any `json:"creator,omitempty"`
	VersionInfo  map[string]any `json:"versionInfo,omitempty"`

	// ValidationReport is the resource name of the validation report generated for
	// this parser version (present on submitted parsers). Append "/parsingErrors"
	// to list the parse failures (see ListParsingErrors). ValidationStage is the
	// stage/state of that validation (e.g. the FAILED state behind a submit's
	// FAILED_PRECONDITION).
	ValidationReport string `json:"validationReport,omitempty"`
	ValidationStage  string `json:"validationStage,omitempty"`
}

// ParsingError is one log that failed to parse, from a parser's validation report.
// Error is the structured error (shape varies); use Message for a human string.
type ParsingError struct {
	Name    string          `json:"name"`    // resource name of this parsing error
	Error   json.RawMessage `json:"error"`   // structured parse error (object or string)
	LogData string          `json:"logData"` // base64-encoded raw log that failed to parse
}

// Message extracts a human-readable error string from the structured Error field,
// accepting either a bare string or an object carrying a message-like key.
func (e ParsingError) Message() string {
	var s string
	if json.Unmarshal(e.Error, &s) == nil {
		return s
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(e.Error, &m) == nil {
		for _, k := range []string{"message", "errorMessage", "error", "description", "detail"} {
			if v, ok := m[k]; ok {
				if json.Unmarshal(v, &s) == nil && s != "" {
					return s
				}
			}
		}
	}
	return string(e.Error)
}

// ListParsingErrors lists the parsing errors of a parser validation report
// (GET {validationReport}/parsingErrors). validationReportName is the full
// resource name from Parser.ValidationReport. This is how to see WHY a parser
// submit/activate failed validation — the per-log error message + the failing log.
func (c *Client) ListParsingErrors(ctx context.Context, validationReportName string, pageSize int) ([]ParsingError, error) {
	var all []ParsingError
	err := paginate(20, func(token string) (string, error) {
		q := url.Values{}
		if pageSize > 0 {
			q.Set("pageSize", strconv.Itoa(pageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			ParsingErrors []ParsingError `json:"parsingErrors"`
			NextPageToken string         `json:"nextPageToken"`
		}
		if err := c.get(ctx, validationReportName+"/parsingErrors", &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.ParsingErrors...)
		if pageSize > 0 && len(all) >= pageSize {
			return "", nil
		}
		return resp.NextPageToken, nil
	})
	if pageSize > 0 && len(all) > pageSize {
		all = all[:pageSize]
	}
	return all, err
}

// ListParsers returns every parser configured for logType (e.g. "OKTA",
// "WINDOWS_AD"). Both ACTIVE and inactive parsers are returned; callers filter
// for state == "ACTIVE" when they want the live one.
//
// DEVIATION: parsers use the project NUMBER form (numeric=true), matching the
// legacy tool's raw_get(..., numeric_project=True). This diverges from the
// resource.go doc table, which lists parsers under the project-ID form; the
// live endpoint accepts (and the legacy tool relies on) the numeric form, so
// that is what is encoded here.
//
// DEVIATION: page size is capped at 100 to mirror the legacy puller and the
// endpoint's documented per-page limit, rather than the 1000 used for rules.
func (c *Client) ListParsers(ctx context.Context, logType string) ([]Parser, error) {
	sub := "logTypes/" + url.PathEscape(logType) + "/parsers"
	var all []Parser
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Parsers       []Parser `json:"parsers"`
			NextPageToken string   `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath(sub, true), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Parsers...)
		return resp.NextPageToken, nil
	})
	return all, err
}
