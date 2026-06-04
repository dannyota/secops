package auth

import "testing"

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
