package soar_test

import "testing"

func TestLiveMarketplaceRead(t *testing.T) {
	c, ctx := liveClient(t)
	mi, err := c.ListMarketplaceIntegrations(ctx)
	if err != nil {
		t.Fatalf("ListMarketplaceIntegrations: %v", err)
	}
	t.Logf("marketplaceIntegrations: %d", len(mi))
	if len(mi) > 0 {
		got, err := c.GetMarketplaceIntegration(ctx, mi[0].Identifier)
		if err != nil {
			t.Errorf("GetMarketplaceIntegration(%q): %v", mi[0].Identifier, err)
		} else {
			t.Logf("get %s -> displayName=%q", got.Identifier, got.DisplayName)
		}
	}
	cp, err := c.ListContentPacks(ctx)
	if err != nil {
		t.Errorf("ListContentPacks: %v", err)
	} else {
		t.Logf("contentPacks: %d", len(cp))
	}
}
