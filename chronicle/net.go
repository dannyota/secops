package chronicle

import (
	"context"
	"net"
	"os"
	"strings"
	"time"
)

// forceIPv4Enabled reports whether SECOPS_FORCE_IPV4 is set to a truthy value.
//
// Some corporate VPNs / Context-Aware-Access setups have broken IPv6 routing to
// *.googleapis.com (intermittent hangs, spurious ADC reauth). Pinning the
// dialer to IPv4 stabilizes those networks. Off by default — IPv6 is correct
// and preferable on healthy networks.
func forceIPv4Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SECOPS_FORCE_IPV4"))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// ipv4DialContext returns a DialContext that rewrites tcp/tcp6 to tcp4 when
// SECOPS_FORCE_IPV4 is enabled, or nil to use the transport default.
func ipv4DialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	if !forceIPv4Enabled() {
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
