package cli

import (
	"testing"
	"time"

	"danny.vn/secops/auth"
)

// TestTimedHTTPClientAppliesTimeout verifies --timeout is wired as a PER-REQUEST
// http.Client.Timeout (the correct altitude) rather than a context deadline that
// would span a confirm prompt or cap a multi-call command in aggregate.
func TestTimedHTTPClientAppliesTimeout(t *testing.T) {
	saved := requestTimeout
	defer func() { requestTimeout = saved }()

	creds := auth.SOARAppKey("k")

	requestTimeout = 45 * time.Second
	if got := timedHTTPClient(creds, false).Timeout; got != 45*time.Second {
		t.Errorf("client.Timeout = %v, want 45s", got)
	}

	// 0 disables the per-request bound (http.Client.Timeout == 0 means no limit).
	requestTimeout = 0
	if got := timedHTTPClient(creds, false).Timeout; got != 0 {
		t.Errorf("--timeout 0 should leave client.Timeout unbounded, got %v", got)
	}
}

// TestBaseContextHasNoDeadline guards the altitude fix: baseContext must NOT carry
// a deadline (timeouts live on the HTTP client), so a confirm prompt or a long
// multi-call command is never on a context clock.
func TestBaseContextHasNoDeadline(t *testing.T) {
	saved := requestTimeout
	defer func() { requestTimeout = saved }()
	requestTimeout = 30 * time.Second
	if _, ok := baseContext().Deadline(); ok {
		t.Error("baseContext must not carry a deadline; per-request timeout belongs on the HTTP client")
	}
}

// TestDefaultRequestTimeoutReasonable guards the default: present (fail-fast) but
// generous enough not to cut a normal single request.
func TestDefaultRequestTimeoutReasonable(t *testing.T) {
	if defaultRequestTimeout < 30*time.Second || defaultRequestTimeout > 5*time.Minute {
		t.Errorf("defaultRequestTimeout = %v, want a generous-but-bounded fail-fast default", defaultRequestTimeout)
	}
}
