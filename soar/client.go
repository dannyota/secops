// Package soar is an unofficial Go SDK for the Google SecOps SOAR (Siemplify)
// API.
//
// This package is the MODERN, durable v1alpha surface: integrations, connectors,
// jobs, alert-grouping rules, module settings, and cases. Transitional and
// legacy surfaces live in the danny.vn/secops/soar/legacy subpackage, quarantined
// so they can be deleted wholesale when their v1alpha equivalents ship — nothing
// here imports that subpackage.
//
// SOAR authenticates with an AppKey (auth.SOARAppKey) on the tenant SOAR host,
// never ADC. See docs/design/soar.md for the full design and the three tiers.
package soar

import (
	"net/http"

	"danny.vn/secops/auth"
	"danny.vn/secops/soar/internal/transport"
)

// Settings identifies a tenant SOAR instance (host + v1alpha path components).
type Settings = transport.Settings

// Client is a modern (v1alpha) SOAR API client. It is safe for concurrent use.
type Client struct {
	t *transport.Transport
}

type clientConfig struct {
	httpClient *http.Client
}

// Option customizes a Client.
type Option func(*clientConfig)

// WithHTTPClient overrides the underlying *http.Client (e.g. for tests).
func WithHTTPClient(h *http.Client) Option {
	return func(c *clientConfig) { c.httpClient = h }
}

// NewClient builds a modern SOAR client. creds must be an AppKey credential
// (auth.SOARAppKey); auth resolves lazily on the first request, so constructing
// a client never touches the network.
func NewClient(s Settings, creds auth.Credentials, opts ...Option) (*Client, error) {
	cfg := &clientConfig{}
	for _, o := range opts {
		o(cfg)
	}
	return &Client{t: transport.New(s, creds, cfg.httpClient)}, nil
}
