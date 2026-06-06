// LEGACY tier: the Siemplify external API (/api/external/v1) Settings surface —
// the form-dynamic-parameters CRUD set.
//
// Method names are prefixed "SettingX" to stay globally unique across the shared
// *Client; reads return json.RawMessage and writes take a freeform body.
package legacy

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// SettingXListFormDynamicParameters returns all form dynamic parameters.
func (c *Client) SettingXListFormDynamicParameters(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/form-dynamic-parameters")
}

// SettingXListFormDynamicParametersByType returns the form dynamic parameters for
// one form type. The collection GET requires a formType filter (the unfiltered
// read is rejected), so config-as-code lists per type; FormType has a single
// usable value, "CloseCase".
func (c *Client) SettingXListFormDynamicParametersByType(ctx context.Context, formType string) (RawJSON, error) {
	return c.externalGetQuery(ctx, "/settings/form-dynamic-parameters", url.Values{"formType": {formType}})
}

// SettingXGetFormDynamicParameter returns one form dynamic parameter by its
// numeric id.
func (c *Client) SettingXGetFormDynamicParameter(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/form-dynamic-parameters/"+strconv.Itoa(id))
}

// SettingXCreateFormDynamicParameter creates a new form dynamic parameter. body
// is the freeform legacy payload. LIVE MUTATION.
func (c *Client) SettingXCreateFormDynamicParameter(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/form-dynamic-parameters", body)
}

// SettingXUpdateFormDynamicParameter updates a form dynamic parameter by its
// numeric id. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) SettingXUpdateFormDynamicParameter(ctx context.Context, id int, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/settings/form-dynamic-parameters/"+strconv.Itoa(id), body)
}

// SettingXDeleteFormDynamicParameter deletes a form dynamic parameter by its
// numeric id. LIVE MUTATION; this cannot be undone.
func (c *Client) SettingXDeleteFormDynamicParameter(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/settings/form-dynamic-parameters/"+strconv.Itoa(id), nil)
}
