package chronicle

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Parser extensions augment a built-in log-type parser with extra extraction
// logic (a CBN snippet, structured field extractors, or a dynamic-parsing
// block). They live under logTypes/<logType>/parserExtensions — the same
// instance base as parsers (parsers.go), which the legacy tool addressed with
// the project NUMBER. To stay consistent with that proven form, every path here
// uses numeric=true.

// ParserExtension is a single parser extension as returned by the API.
//
// State is typically "NEW", "VALIDATING", "LIVE", etc.; Validation surfaces the
// server-side validation report. The State/Validation/CbnSnippet/etc. fields
// are loosely specified by the API, so the bulk of the freeform payload is
// preserved verbatim — only the load-bearing identifiers are typed.
type ParserExtension struct {
	Name       string                     `json:"name,omitempty"`
	State      string                     `json:"state,omitempty"`
	CreateTime string                     `json:"createTime,omitempty"`
	UpdateTime string                     `json:"updateTime,omitempty"`
	Validation *ParserExtensionValidation `json:"validationReport,omitempty"`
	CbnSnippet string                     `json:"cbnSnippet,omitempty"` // base64-encoded snippet
	Log        string                     `json:"log,omitempty"`        // base64-encoded sample log
}

// ParserExtensionValidation is the server-side validation report for an
// extension. Its shape is not firmly committed to by the API, so the verbatim
// fields are preserved while the common scalars are typed.
type ParserExtensionValidation struct {
	VerdictType string `json:"verdictType,omitempty"`
}

// ID returns the trailing parserExtensions/<id> segment of the extension's
// resource Name — the identifier the get/activate/delete paths expect.
func (e *ParserExtension) ID() string {
	if e == nil || e.Name == "" {
		return ""
	}
	const sep = "/parserExtensions/"
	if i := strings.LastIndex(e.Name, sep); i >= 0 {
		return e.Name[i+len(sep):]
	}
	return e.Name[strings.LastIndex(e.Name, "/")+1:]
}

// ParserExtensionConfig is the request body for CreateParserExtension. Exactly
// one of CbnSnippet, FieldExtractors, or DynamicParsing must be set (the API
// rejects zero or multiple); Validate enforces this before the call.
//
// CbnSnippet and Log are sent base64-encoded; the constructor helpers below
// encode raw text for you. FieldExtractors and DynamicParsing are genuinely
// freeform structured blobs, so they are passed through as decoded JSON.
//
// DEVIATION: the wrapper's ParserExtensionConfig silently re-uses an
// already-base64 input by attempting a round-trip decode. We make encoding
// explicit: NewCBNSnippetConfig encodes raw text; callers with pre-encoded
// bytes set the field directly. This avoids the ambiguous "is this already
// base64?" guess on inputs that happen to be valid base64 by coincidence.
type ParserExtensionConfig struct {
	// Log is an optional base64-encoded sample log the snippet is tested against.
	Log string `json:"log,omitempty"`
	// CbnSnippet is a base64-encoded CBN (Config-Based Normalizer) snippet.
	CbnSnippet string `json:"cbn_snippet,omitempty"`
	// FieldExtractors is a structured field-extractor definition.
	FieldExtractors map[string]any `json:"field_extractors,omitempty"`
	// DynamicParsing is a structured dynamic-parsing definition.
	DynamicParsing map[string]any `json:"dynamic_parsing,omitempty"`
}

// NewCBNSnippetConfig builds a config from raw (un-encoded) CBN snippet text
// and an optional raw sample log, base64-encoding both for the wire.
func NewCBNSnippetConfig(snippet, sampleLog string) *ParserExtensionConfig {
	cfg := &ParserExtensionConfig{CbnSnippet: encodeB64(snippet)}
	if sampleLog != "" {
		cfg.Log = encodeB64(sampleLog)
	}
	return cfg
}

// Validate enforces the API's "exactly one config field" rule.
func (cfg *ParserExtensionConfig) Validate() error {
	n := 0
	if cfg.CbnSnippet != "" {
		n++
	}
	if cfg.FieldExtractors != nil {
		n++
	}
	if cfg.DynamicParsing != nil {
		n++
	}
	if n != 1 {
		return fmt.Errorf("chronicle: parser extension config must set exactly one of CbnSnippet, FieldExtractors, or DynamicParsing (got %d)", n)
	}
	return nil
}

// encodeB64 returns s base64-encoded, passing through input that already decodes
// to valid UTF-8 (matching the wrapper's idempotent-encode behavior).
func encodeB64(s string) string {
	if s == "" {
		return ""
	}
	if dec, err := base64.StdEncoding.DecodeString(s); err == nil && utf8.Valid(dec) {
		return s // already base64
	}
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func (c *Client) parserExtensionsPath(logType string) string {
	return c.resourcePath("logTypes/"+url.PathEscape(logType)+"/parserExtensions", true)
}

// CreateParserExtension creates a parser extension for logType. The variadic
// body is optional only ergonomically — a single config is required, and it
// must pass Validate (exactly one config field set).
//
// DEVIATION: the wrapper takes a single positional config. We accept it
// variadically so a future "empty extension" form can be added without an API
// break, but reject zero or multiple configs here.
func (c *Client) CreateParserExtension(ctx context.Context, logType string, body ...*ParserExtensionConfig) (*ParserExtension, error) {
	if len(body) != 1 || body[0] == nil {
		return nil, fmt.Errorf("chronicle: CreateParserExtension requires exactly one config")
	}
	cfg := body[0]
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var ext ParserExtension
	if err := c.post(ctx, c.parserExtensionsPath(logType), cfg, &ext); err != nil {
		return nil, err
	}
	return &ext, nil
}

// GetParserExtension fetches a single parser extension by ID.
func (c *Client) GetParserExtension(ctx context.Context, logType, extID string) (*ParserExtension, error) {
	sub := "logTypes/" + url.PathEscape(logType) + "/parserExtensions/" + url.PathEscape(extID)
	var ext ParserExtension
	if err := c.get(ctx, c.resourcePath(sub, true), &ext); err != nil {
		return nil, err
	}
	return &ext, nil
}

// ListParserExtensions returns every parser extension for logType. pageSize
// caps items per request (<=0 lets the server pick its default); results are
// aggregated across pages.
func (c *Client) ListParserExtensions(ctx context.Context, logType string, pageSize int) ([]ParserExtension, error) {
	base := c.parserExtensionsPath(logType)
	var all []ParserExtension
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{}
		if pageSize > 0 {
			q.Set("pageSize", fmt.Sprintf("%d", pageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			ParserExtensions []ParserExtension `json:"parserExtensions"`
			NextPageToken    string            `json:"nextPageToken"`
		}
		if err := c.get(ctx, base, &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.ParserExtensions...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// ActivateParserExtension promotes a validated extension to LIVE via the
// :activate RPC suffix. Returns *APIError on failure.
func (c *Client) ActivateParserExtension(ctx context.Context, logType, extID string) error {
	sub := "logTypes/" + url.PathEscape(logType) + "/parserExtensions/" + url.PathEscape(extID) + ":activate"
	return c.post(ctx, c.resourcePath(sub, true), nil, nil)
}

// DeleteParserExtension permanently removes a parser extension.
func (c *Client) DeleteParserExtension(ctx context.Context, logType, extID string) error {
	sub := "logTypes/" + url.PathEscape(logType) + "/parserExtensions/" + url.PathEscape(extID)
	return c.do(ctx, "DELETE", c.resourcePath(sub, true), nil, nil)
}
