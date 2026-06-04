// Package legacy holds the TRANSITIONAL and LEGACY Google SecOps SOAR surfaces,
// quarantined so they can be deleted wholesale once the modern v1alpha API
// covers them. Two tiers live here:
//
//   - BRIDGE (playbooks.go): legacyPlaybooks:legacy* — v1alpha host, legacy
//     operation names. Remove when native v1alpha playbook CRUD ships.
//   - LEGACY (cases.go, playbooks_external.go): the Siemplify external API under
//     /api/external/v1. Remove when v1alpha bulk-case + playbook endpoints ship.
//
// Nothing in danny.vn/secops/soar imports this package — deleting this directory
// leaves the modern client untouched. Both packages build on the shared
// danny.vn/secops/soar/internal/transport, so legacy stays a clean cut.
//
// SOAR authenticates with an AppKey (auth.SOARAppKey), never ADC.
package legacy

import (
	"net/http"

	"danny.vn/secops/auth"
	"danny.vn/secops/soar/internal/transport"
)

// Settings identifies a tenant SOAR instance (host + v1alpha path components).
type Settings = transport.Settings

// Client speaks the legacy and bridge SOAR surfaces. It is safe for concurrent
// use. Construct it only when you actually need a transitional endpoint.
type Client struct {
	t *transport.Transport
}

// NewClient builds a legacy SOAR client. creds must be an AppKey credential
// (auth.SOARAppKey); auth resolves lazily on the first request. A nil httpClient
// uses a sensible default.
func NewClient(s Settings, creds auth.Credentials, httpClient *http.Client) *Client {
	return &Client{t: transport.New(s, creds, httpClient)}
}
