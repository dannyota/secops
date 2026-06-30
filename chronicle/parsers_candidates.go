package chronicle

import (
	"context"
	"net/url"
)

// ParserAction is a parser lifecycle action for fetchParserCandidates.
type ParserAction string

const (
	ParserActionUpgrade         ParserAction = "PARSER_ACTION_UPGRADE"
	ParserActionRollback        ParserAction = "PARSER_ACTION_ROLLBACK"
	ParserActionOptInToPreview  ParserAction = "PARSER_ACTION_OPT_IN_TO_PREVIEW"
	ParserActionOptOutOfPreview ParserAction = "PARSER_ACTION_OPT_OUT_OF_PREVIEW"
)

// FetchParserCandidates returns the parser candidates eligible for the given
// action on a log type. For example, PARSER_ACTION_UPGRADE returns parsers
// that can be upgraded; PARSER_ACTION_OPT_IN_TO_PREVIEW returns those eligible
// for preview opt-in.
//
// Endpoint: GET {instance}/logTypes/{logType}/parsers:fetchParserCandidates
// Query: parserAction={action}
//
// DEVIATION: parsers use the project NUMBER form (numeric=true), consistent
// with ListParsers and all other parser endpoints.
func (c *Client) FetchParserCandidates(ctx context.Context, logType string, action ParserAction) ([]Parser, error) {
	sub := parserLogTypePath(logType) + "/parsers:fetchParserCandidates"
	q := url.Values{"parserAction": {string(action)}}
	var resp struct {
		Candidates []Parser `json:"candidates"`
	}
	if err := c.get(ctx, c.resourcePath(sub, true), &resp, withQuery(q)); err != nil {
		return nil, err
	}
	return resp.Candidates, nil
}
