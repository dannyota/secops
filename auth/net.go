package auth

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// ForceIPv4Env reports whether SECOPS_FORCE_IPV4 is set to a truthy value
// (1/true/yes). It lets the env override turn IPv4 forcing on without config.
func ForceIPv4Env() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SECOPS_FORCE_IPV4"))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// IPv4DialContext returns a DialContext that rewrites tcp/tcp6 to tcp4 when IPv4
// is forced — either by the force argument (from config) or by SECOPS_FORCE_IPV4
// — and nil otherwise (use the transport default). Some corporate VPNs /
// Context-Aware-Access setups have broken IPv6 routing to *.googleapis.com
// (intermittent hangs, spurious reauth); pinning to IPv4 stabilizes them. IPv6
// is correct and preferable on healthy networks, so this is opt-in.
func IPv4DialContext(force bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if !force && !ForceIPv4Env() {
		return nil
	}
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		switch network {
		case "tcp", "tcp6":
			network = "tcp4"
		}
		return d.DialContext(ctx, network, addr)
	}
}

// HTTPTransport returns the standard outbound *http.Transport for every
// secopsctl HTTP client: http.DefaultTransport-equivalent dialing, pooling,
// and timeouts, with the dialer pinned to IPv4 when forced — by the argument
// (from config) or by SECOPS_FORCE_IPV4 (see IPv4DialContext).
func HTTPTransport(forceIPv4 bool) *http.Transport {
	t := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if dc := IPv4DialContext(forceIPv4); dc != nil {
		t.DialContext = dc
	}
	return t
}
