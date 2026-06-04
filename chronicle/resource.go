package chronicle

import "fmt"

// instancePath returns the Chronicle instance resource name:
//
//	projects/<id-or-number>/locations/<region>/instances/<customer_id>
//
// Pass numeric=true ONLY for the endpoints that genuinely require the project
// NUMBER — curated rule-set categories, curated rule sets, and parsers
// (logTypes/*/parsers). Everything else uses the project ID (numeric=false):
// rules, reference lists, data tables, native dashboards, curated rule-set
// deployments, featured content rules, feeds, and UDM search. This matches the
// legacy tool, which only forced the number on those three raw endpoints.
//
// DEVIATION: the official Python wrapper often discovers the right form by
// issuing a call, catching a 404, and retrying the other form. We make the
// required form explicit per endpoint instead (callers pass the correct bool),
// and expose IsNotFound for the rare case a fallback is genuinely wanted.
func (c *Client) instancePath(numeric bool) string {
	proj := c.settings.ProjectID
	if numeric {
		proj = c.settings.ProjectNumber
	}
	return fmt.Sprintf("projects/%s/locations/%s/instances/%s",
		proj, c.settings.Region, c.settings.CustomerID)
}

// resourcePath joins the instance path with sub (no leading slash on sub).
func (c *Client) resourcePath(sub string, numeric bool) string {
	return c.instancePath(numeric) + "/" + sub
}
