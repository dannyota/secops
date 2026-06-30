package chronicle

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Parser write/lifecycle operations.
//
// These mirror the official wrapper's parser.py. CBN (the parser source, the
// "Config-Based Normalizer") is base64-encoded on the wire; helpers here accept
// plain source text and encode it for the caller, matching the wrapper's
// base64.b64encode(...).decode() dance on every send.
//
// DEVIATION: logTypes/*/parsers use the project NUMBER form (numeric=true),
// consistent with the sibling ListParsers in parsers.go and the resource.go doc
// table (parsers are one of the three numeric-project exceptions). All paths in
// this file therefore pass numeric=true.

// parserLogTypePath returns logTypes/<logType>.
func parserLogTypePath(logType string) string {
	return "logTypes/" + url.PathEscape(logType)
}

// parserPath returns logTypes/<logType>/parsers/<parserID>.
func parserPath(logType, parserID string) string {
	return parserLogTypePath(logType) + "/parsers/" + url.PathEscape(parserID)
}

// CreateParser creates a new custom parser for logType. code is the plain CBN
// parser source (NOT base64) — it is base64-encoded here before sending.
//
// validatedOnEmptyLogs controls whether the parser must validate against empty
// logs; the wrapper defaults this to true.
func (c *Client) CreateParser(ctx context.Context, logType, code string, validatedOnEmptyLogs bool) (*Parser, error) {
	body := struct {
		CBN                  string `json:"cbn"`
		ValidatedOnEmptyLogs bool   `json:"validated_on_empty_logs"`
	}{
		CBN:                  base64.StdEncoding.EncodeToString([]byte(code)),
		ValidatedOnEmptyLogs: validatedOnEmptyLogs,
	}
	var p Parser
	sub := parserLogTypePath(logType) + "/parsers"
	if err := c.post(ctx, c.resourcePath(sub, true), body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetParser fetches a single parser by ID for the given logType. The returned
// Parser's CBN is base64-encoded, as the API returns it.
func (c *Client) GetParser(ctx context.Context, logType, parserID string) (*Parser, error) {
	var p Parser
	if err := c.get(ctx, c.resourcePath(parserPath(logType, parserID), true), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ActivateParser makes a custom parser live for the instance. The endpoint
// returns an empty object on success.
func (c *Client) ActivateParser(ctx context.Context, logType, parserID string) error {
	sub := parserPath(logType, parserID) + ":activate"
	return c.post(ctx, c.resourcePath(sub, true), struct{}{}, nil)
}

// ActivateReleaseCandidateParser activates the release-candidate parser, making
// it live for the instance.
func (c *Client) ActivateReleaseCandidateParser(ctx context.Context, logType, parserID string) error {
	sub := parserPath(logType, parserID) + ":activateReleaseCandidateParser"
	return c.post(ctx, c.resourcePath(sub, true), struct{}{}, nil)
}

// DeactivateParser deactivates a custom parser. Returns an empty object on
// success.
func (c *Client) DeactivateParser(ctx context.Context, logType, parserID string) error {
	sub := parserPath(logType, parserID) + ":deactivate"
	return c.post(ctx, c.resourcePath(sub, true), struct{}{}, nil)
}

// DeleteParser deletes a parser. Pass force=true to forcibly delete an ACTIVE
// parser; otherwise the API rejects deleting an active one.
func (c *Client) DeleteParser(ctx context.Context, logType, parserID string, force bool) error {
	q := url.Values{}
	if force {
		q.Set("force", "true")
	}
	path := c.resourcePath(parserPath(logType, parserID), true)
	return c.do(ctx, "DELETE", path, nil, nil, withQuery(q))
}

// CopyParser makes a copy of a (typically prebuilt) parser identified by
// sourceID, returning the newly created Parser.
func (c *Client) CopyParser(ctx context.Context, logType, sourceID string) (*Parser, error) {
	var p Parser
	sub := parserPath(logType, sourceID) + ":copy"
	if err := c.post(ctx, c.resourcePath(sub, true), struct{}{}, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// RunParserResult is one per-log result from RunParser. Log echoes back the
// (base64-encoded) input log line; the result is a union of ParsedEvents (success)
// or Error (a gRPC Status with code+message). ParsedFields and
// FailedFieldsAndErrors carry intermediate parse state for debugging.
type RunParserResult struct {
	Log                   string            `json:"log,omitempty"`
	ParsedEvents          *ParsedEvents     `json:"parsedEvents,omitempty"`
	Error                 *RunParserError   `json:"error,omitempty"`
	ParsedFields          json.RawMessage   `json:"parsedFields,omitempty"`
	FailedFieldsAndErrors json.RawMessage   `json:"failedFieldsAndErrors,omitempty"`
	StatedumpResults      []json.RawMessage `json:"statedumpResults,omitempty"`
}

// RunParserError is the gRPC Status returned when a log fails to parse.
type RunParserError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

// ParsedEvents is the server's wrapper around the UDM events a parser emitted for
// one input log. Each Events element is a freeform {"event": {...}} UDM object.
type ParsedEvents struct {
	Events []json.RawMessage `json:"events,omitempty"`
}

// RunParserResponse is the result of evaluating a parser against sample logs.
type RunParserResponse struct {
	RunParserResults []RunParserResult `json:"runParserResults,omitempty"`
}

// RunParser evaluates code (plain CBN source) against the given sampleLogs
// without creating or activating a parser, returning the emitted UDM output.
//
// code and each sample log are base64-encoded on send (the API expects base64
// for both the parser cbn and every log entry), mirroring the wrapper. code is
// required — the API returns 400 without a parser block.
//
// A statedump filter is injected automatically (before the closing brace of
// the outermost filter block) when the CBN does not already contain one, so
// diagnostics (@onErrorCount, @output, intermediate variables) are always
// available in the response.
func (c *Client) RunParser(ctx context.Context, logType, code string, sampleLogs []string) (*RunParserResponse, error) {
	if logType == "" {
		return nil, fmt.Errorf("chronicle: RunParser: logType is required")
	}
	if code == "" {
		return nil, fmt.Errorf("chronicle: RunParser: code (CBN source) is required")
	}
	if len(sampleLogs) == 0 {
		return nil, fmt.Errorf("chronicle: RunParser: at least one sample log is required")
	}

	code = injectStatedump(code)

	encodedLogs := make([]string, len(sampleLogs))
	for i, log := range sampleLogs {
		encodedLogs[i] = base64.StdEncoding.EncodeToString([]byte(log))
	}

	body := struct {
		Parser struct {
			CBN string `json:"cbn"`
		} `json:"parser"`
		Log              []string `json:"log"`
		StatedumpAllowed bool     `json:"statedump_allowed"`
	}{
		Log:              encodedLogs,
		StatedumpAllowed: true,
	}
	body.Parser.CBN = base64.StdEncoding.EncodeToString([]byte(code))

	var resp RunParserResponse
	sub := parserLogTypePath(logType) + ":runParser"
	if err := c.post(ctx, c.resourcePath(sub, true), body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// injectStatedump appends a statedump filter before the final closing brace
// of the outermost filter block when the CBN does not already contain one.
func injectStatedump(code string) string {
	if strings.Contains(code, "statedump") {
		return code
	}
	i := strings.LastIndex(code, "}")
	if i < 0 {
		return code
	}
	return code[:i] + "\n  statedump { label => \"secopsctl\" }\n}\n"
}
