package legacy

import (
	"encoding/json"
	"testing"
)

// TestLiveCasesOverviewReads exercises the safe, zero-setup read endpoints of the
// case-overview / predefined-widgets surface. Runs under SECOPS_SOAR_SMOKE=1.
//
// The two list endpoints need no arguments and work on a fresh tenant. The full
// template details read needs a template identifier, so it is derived from the
// first overview template card (and skipped when there are no templates yet).
func TestLiveCasesOverviewReads(t *testing.T) {
	lc, ctx := liveClient(t)

	cards := readProbe(t, "case-overview/ListTemplateCards", func() (RawJSON, error) {
		return lc.CaseOverviewListTemplateCards(ctx)
	})
	readProbe(t, "case-overview/ListPredefinedWidgets", func() (RawJSON, error) {
		return lc.CaseOverviewListPredefinedWidgets(ctx)
	})

	// Derive a template identifier from the first card, then fetch its full
	// details. Skip silently when the tenant has no overview templates.
	if id := firstTemplateIdentifier(cards); id != "" {
		readProbe(t, "case-overview/GetFullTemplateDetails", func() (RawJSON, error) {
			return lc.CaseOverviewGetFullTemplateDetails(ctx, id)
		})
	} else {
		t.Log("case-overview/GetFullTemplateDetails           SKIP no overview templates to derive an identifier")
	}
}

// firstTemplateIdentifier pulls the "identifier" of the first overview template
// card from a ListTemplateCards response, or "" if none is available.
func firstTemplateIdentifier(cards RawJSON) string {
	if len(cards) == 0 {
		return ""
	}
	var arr []struct {
		Identifier string `json:"identifier"`
	}
	if json.Unmarshal(cards, &arr) != nil {
		return ""
	}
	for _, c := range arr {
		if c.Identifier != "" {
			return c.Identifier
		}
	}
	return ""
}

// No CRUD test: the only mutable resource in this tag is the overview template
// (CaseOverviewSaveTemplate), but the legacy surface exposes no delete/remove
// method for it, so a list -> create -> update -> delete lifecycle cannot be
// completed safely. Reads only.
