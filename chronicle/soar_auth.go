package chronicle

import (
	"context"
	"fmt"
)

// SoarAuthJwt is the response from GenerateSoarAuthJwt — a signed Siemplify JWT
// that authenticates to the SOAR host via Bearer auth. The JWT carries the
// user's identity, permissions, and SOC role derived from the ADC principal.
type SoarAuthJwt struct {
	SignedJwt string `json:"signedJwt"`
}

// GenerateSoarAuthJwt mints a Siemplify JWT from the current ADC credentials.
// The chronicle host exchanges the caller's Google OAuth token for a SOAR-plane
// JWT that can authenticate to the siemplify-soar host as a Bearer token. This
// bridges ADC auth to the SOAR plane, eliminating the need for a SOAR AppKey.
//
// Requires the chronicle.instances.generateSoarAuthJwt IAM permission and one
// of the standard Chronicle OAuth scopes (cloud-platform, chronicle, or
// chronicle.readonly). The project ID form is used (matching the documented
// path parameter name).
//
// NOTE: as of 2026-06, this endpoint requires IAM permissions from Google's
// Stage 2 SOAR→IAM migration (deadline September 30, 2026). Until that
// migration completes on a given tenant, the endpoint returns 404. The CLI
// uses AppKey auth directly; this SDK method is for forward compatibility.
func (c *Client) GenerateSoarAuthJwt(ctx context.Context) (*SoarAuthJwt, error) {
	body := map[string]string{"soarJwtType": "USER_CLAIMS_JWT"}
	path := c.instancePath(false) + ":generateSoarAuthJwt"
	var result SoarAuthJwt
	if err := c.post(ctx, path, body, &result); err != nil {
		return nil, err
	}
	if result.SignedJwt == "" {
		return nil, fmt.Errorf("generateSoarAuthJwt: empty signedJwt in response")
	}
	return &result, nil
}
