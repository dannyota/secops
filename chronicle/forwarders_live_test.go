package chronicle_test

import (
	"errors"
	"testing"

	"danny.vn/secops/chronicle"
)

func TestLiveForwardersRead(t *testing.T) {
	c, ctx := liveChronicle(t)
	fwds, err := c.ListForwarders(ctx)
	if err != nil {
		t.Fatalf("ListForwarders: %v", err)
	}
	t.Logf("forwarders: %d", len(fwds))
	if len(fwds) == 0 {
		t.Skip("no forwarders to exercise collectors")
	}
	cols, err := c.ListCollectors(ctx, fwds[0].ForwarderID())
	if err != nil {
		var apiErr *chronicle.APIError
		if errors.As(err, &apiErr) && apiErr.Status >= 500 {
			t.Skipf("ListCollectors: backend flaky (HTTP %d)", apiErr.Status)
		}
		t.Fatalf("ListCollectors: %v", err)
	}
	t.Logf("collectors on %s: %d", fwds[0].ForwarderID(), len(cols))
}

var (
	_ = (*chronicle.Client).CreateForwarder
	_ = (*chronicle.Client).UpdateForwarder
	_ = (*chronicle.Client).DeleteForwarder
	_ = (*chronicle.Client).GetCollector
)
