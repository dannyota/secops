// Package legacy is the Google SecOps SOAR client for the Siemplify external API
// (/api/external/v1, AppKey). It is the broad, reliable SOAR lane: the reconcile
// engine and the case verbs run on it, and it is the fallback when a modern
// v1alpha surface 500s. It stays importable and is part of the long-term design —
// not a temporary shim. Two tiers live here:
//
//   - EXTERNAL (cases.go, playbooks_external.go, and the family files): the
//     Siemplify external API under /api/external/v1 — the reliable AppKey surface.
//   - BRIDGE (playbooks.go): legacyPlaybooks:legacy* — SOAR-host v1alpha endpoints
//     with legacy operation names, used until native v1alpha playbook CRUD ships.
//
// Both build on the shared danny.vn/secops/soar/internal/transport. SOAR
// authenticates with an AppKey (auth.SOARAppKey), never ADC.
package legacy

import (
	"net/http"

	"danny.vn/secops/auth"
	"danny.vn/secops/soar/internal/transport"
)

// Settings identifies a tenant SOAR instance (host + v1alpha path components).
type Settings = transport.Settings

// Client speaks the Siemplify external-API (and bridge) SOAR surfaces. It is safe
// for concurrent use.
type Client struct {
	t *transport.Transport
}

// NewClient builds a legacy SOAR client. creds must be an AppKey credential
// (auth.SOARAppKey); auth resolves lazily on the first request. A nil httpClient
// uses a sensible default.
func NewClient(s Settings, creds auth.Credentials, httpClient *http.Client) *Client {
	return &Client{t: transport.New(s, creds, httpClient)}
}
