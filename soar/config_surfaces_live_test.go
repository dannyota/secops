package soar_test

import "testing"

func TestLiveConfigSurfacesRead(t *testing.T) {
	c, ctx := liveClient(t)
	checks := []struct {
		name string
		fn   func() (int, error)
	}{
		{"environments", func() (int, error) { r, e := c.ListEnvironments(ctx); return len(r), e }},
		{"socRoles", func() (int, error) { r, e := c.ListSocRoles(ctx); return len(r), e }},
		{"customLists", func() (int, error) { r, e := c.ListCustomLists(ctx); return len(r), e }},
		{"caseStageDefinitions", func() (int, error) { r, e := c.ListCaseStageDefinitions(ctx); return len(r), e }},
		{"caseCloseDefinitions", func() (int, error) { r, e := c.ListCaseCloseDefinitions(ctx); return len(r), e }},
		{"caseTagDefinitions", func() (int, error) { r, e := c.ListCaseTagDefinitions(ctx); return len(r), e }},
	}
	for _, ck := range checks {
		n, err := ck.fn()
		if err != nil {
			t.Errorf("%s: %v", ck.name, err)
			continue
		}
		t.Logf("OK %-22s %d", ck.name, n)
	}
}
