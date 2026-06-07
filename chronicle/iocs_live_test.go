package chronicle_test

import (
	"testing"

	"danny.vn/secops/chronicle"
)

// TestLiveModernIoCsRead validates iocs:find with the correct body shape (a
// fieldAndValue indicator lookup). A benign domain that isn't an IoC simply
// yields no records — the point is the call returns 200, not 400. If it resolves
// to a record, GetIoC round-trips it by its resource id. Read-only.
func TestLiveModernIoCsRead(t *testing.T) {
	c, ctx := liveChronicle(t)

	iocs, err := c.FindIoCs(ctx, chronicle.FieldAndValue{Value: "example.com", ValueType: chronicle.IoCValueDomain})
	if err != nil {
		t.Fatalf("FindIoCs: %v", err)
	}
	t.Logf("FindIoCs(example.com) -> %d ioc(s)", len(iocs))

	if len(iocs) > 0 {
		got, err := c.GetIoC(ctx, iocs[0].ID)
		if err != nil {
			t.Fatalf("GetIoC(%q): %v", iocs[0].ID, err)
		}
		if got.Name != iocs[0].Name {
			t.Errorf("round-trip mismatch: find %q vs get %q", iocs[0].Name, got.Name)
		}
	}
}
