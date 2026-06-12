package auth

import (
	"context"
	"net"
	"testing"
)

func TestIPv4DialContext(t *testing.T) {
	t.Run("force on", func(t *testing.T) {
		t.Setenv("SECOPS_FORCE_IPV4", "")
		if IPv4DialContext(true) == nil {
			t.Error("force=true should yield a dialer")
		}
	})
	t.Run("off by default", func(t *testing.T) {
		t.Setenv("SECOPS_FORCE_IPV4", "")
		if IPv4DialContext(false) != nil {
			t.Error("force=false with no env should yield nil")
		}
	})
	t.Run("env enables", func(t *testing.T) {
		t.Setenv("SECOPS_FORCE_IPV4", "1")
		if IPv4DialContext(false) == nil {
			t.Error("SECOPS_FORCE_IPV4=1 should yield a dialer even when force=false")
		}
	})
}

func TestHTTPTransport(t *testing.T) {
	t.Run("default dialer when not forced", func(t *testing.T) {
		t.Setenv("SECOPS_FORCE_IPV4", "")
		tr := HTTPTransport(false)
		if tr.DialContext == nil {
			t.Error("want the default 30s dialer, got nil DialContext")
		}
		if tr.Proxy == nil || !tr.ForceAttemptHTTP2 {
			t.Error("want ProxyFromEnvironment and HTTP/2 enabled")
		}
		if tr.MaxIdleConns != 100 || tr.IdleConnTimeout == 0 {
			t.Error("want DefaultTransport-equivalent pooling (MaxIdleConns 100, nonzero IdleConnTimeout)")
		}
	})
	// The pinned dialer rewrites tcp/tcp6 to tcp4, so it cannot reach an
	// IPv6-only loopback listener while the default dialer can.
	t.Run("pinned dialer when forced", func(t *testing.T) {
		t.Setenv("SECOPS_FORCE_IPV4", "")
		l, err := net.Listen("tcp6", "[::1]:0")
		if err != nil {
			t.Skip("IPv6 loopback unavailable")
		}
		defer func() { _ = l.Close() }()
		ctx := context.Background()
		if conn, err := HTTPTransport(false).DialContext(ctx, "tcp", l.Addr().String()); err != nil {
			t.Errorf("default dialer should reach [::1]: %v", err)
		} else {
			_ = conn.Close()
		}
		if conn, err := HTTPTransport(true).DialContext(ctx, "tcp", l.Addr().String()); err == nil {
			_ = conn.Close()
			t.Error("forced dialer should be pinned to IPv4 and fail to reach [::1]")
		}
	})
	t.Run("env forces too", func(t *testing.T) {
		t.Setenv("SECOPS_FORCE_IPV4", "1")
		l, err := net.Listen("tcp6", "[::1]:0")
		if err != nil {
			t.Skip("IPv6 loopback unavailable")
		}
		defer func() { _ = l.Close() }()
		if conn, err := HTTPTransport(false).DialContext(context.Background(), "tcp", l.Addr().String()); err == nil {
			_ = conn.Close()
			t.Error("SECOPS_FORCE_IPV4=1 should pin the dialer to IPv4 even when force=false")
		}
	})
}
