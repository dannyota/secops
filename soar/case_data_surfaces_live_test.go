package soar_test

import "testing"

// TestLiveCaseDataSurfacesRead validates the read paths of the modern v1alpha
// Case Data surfaces (Wave 16) on the SOAR host. Read-only; gated on
// SECOPS_SOAR_SMOKE=1.
func TestLiveCaseDataSurfacesRead(t *testing.T) {
	c, ctx := liveClient(t)
	checks := []struct {
		name string
		fn   func() (int, error)
	}{
		{"customFields", func() (int, error) { r, e := c.ListCustomFields(ctx); return len(r), e }},
		{"calculatedFieldDefinitions", func() (int, error) { r, e := c.ListCalculatedFieldDefinitions(ctx); return len(r), e }},
		{"propertySchemaDefinitions", func() (int, error) { r, e := c.ListPropertySchemaDefinitions(ctx); return len(r), e }},
	}
	for _, ck := range checks {
		n, err := ck.fn()
		if err != nil {
			t.Errorf("%s: %v", ck.name, err)
			continue
		}
		t.Logf("OK %-28s %d", ck.name, n)
	}
}
