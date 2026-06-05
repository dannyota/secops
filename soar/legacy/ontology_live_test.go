package legacy

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
)

// TestLiveOntologyReads would exercise the read-only /ontology/ endpoints, but
// the only zero-argument ontology read — ListVisualFamilies — is already
// covered by TestLivePlaybooksReads in playbooks_live_test.go. Every other
// /ontology/ read needs a caller-supplied selector body (GetMappingRules,
// GetFamily, ExportOntology, ExportVisualFamily, the *Exists checks) or a
// specific family name/id (GetRelatedEntitiesByFamilyName), none of which can
// be safely synthesized on a tenant with no prior ontology setup. So there is
// nothing green to probe here.
func TestLiveOntologyReads(t *testing.T) {
	_, _ = liveClient(t)
	t.Skip("no read-only endpoints in this tag (ListVisualFamilies is covered in playbooks_live_test.go)")
}

// GROUP D (medium) — ontology.

// mappingRuleField is the real UDM entity field the mapping-rule lifecycle
// customizes. A field-mapping rule has no user-assignable name — its identity is
// the (source, securityEventFieldName) pair, and securityEventFieldName must be a
// known UDM field, not a free-form label — so the lifecycle pins a real field and
// uses a marker attribute to tell the throwaway customization apart.
const mappingRuleField = "SourceUserName"

// TestLiveOntologyMappingRuleCRUD exercises the field-mapping-rule surface
// (GetMappingRules / AddOrUpdateMappingRules / DeleteMappingRule). GetMappingRules
// returns, per field, the mapping that applies to a source: an un-customized field
// reads back as a default (id 0, no override); customizing one persists as a
// record with a server-assigned id; deleting that record reverts the field to its
// default. There is no per-rule name, so this is bespoke rather than runLifecycle.
//
// It customizes mappingRuleField on a FICTITIOUS source (no real event carries it,
// so the mapping can never apply to a real event — it is inert), using
// rawDataPrimaryFieldMatchTerm as a distinguishing marker via read-modify-write of
// the field's default row. Chain: read default -> customize -> verify -> edit ->
// verify -> delete -> verify reverted. Write-gated; reverts the field on cleanup.
func TestLiveOntologyMappingRuleCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	requireWrite(t)

	src := smokeLabel("mapping-src")
	sel := map[string]any{"source": src, "product": src, "eventName": src}

	// fetch returns a fresh copy of the inner mappingRule for mappingRuleField
	// under src.
	fetch := func(stage string) map[string]any {
		raw, err := lc.ListMappingRules(ctx, sel)
		for _, r := range objects(t, "mapping-rules "+stage, raw, err) {
			mr, _ := r["mappingRule"].(map[string]any)
			if mr != nil && strField("securityEventFieldName")(mr) == mappingRuleField {
				return mr
			}
		}
		t.Fatalf("%s: field %q not found for source", stage, mappingRuleField)
		return nil
	}
	marker := strField("rawDataPrimaryFieldMatchTerm")
	upsert := func(mr map[string]any) error {
		_, err := lc.AddOrUpdateMappingRules(ctx, []any{map[string]any{
			"mappingRule": mr, "ontologyConfigurationLevel": 0, "targetFieldType": 0, // Source level, Entity target
		}})
		return err
	}

	// Default row: no override yet (id 0).
	base := fetch("default")
	if id, _ := intField("id")(base); id != 0 {
		t.Fatalf("expected an un-customized field (id 0) before create, got id=%d", id)
	}

	// Create: customize the field via read-modify-write, tagging the marker.
	label := smokeLabel("mapping-rule")
	custom := cloneObj(base)
	custom["rawDataPrimaryFieldMatchTerm"] = label
	if err := upsert(custom); err != nil {
		t.Fatalf("customize mapping rule: %v", err)
	}
	reverted := false
	t.Cleanup(func() {
		if reverted {
			return
		}
		if _, err := lc.DeleteMappingRule(ctx, custom); err != nil {
			t.Logf("cleanup: could not revert mapping rule (%s) for throwaway source: %v", mappingRuleField, err)
		}
	})

	// Verify the customization persisted with a real id + our marker.
	got := fetch("customized")
	if id, _ := intField("id")(got); id == 0 {
		t.Fatalf("customized field still reads as a default (id 0)")
	}
	if marker(got) != label {
		t.Fatalf("marker not stored: got %q want %q", marker(got), label)
	}

	// Edit the marker; verify it round-trips.
	edited := cloneObj(got)
	edited["rawDataPrimaryFieldMatchTerm"] = label + "-edited"
	if err := upsert(edited); err != nil {
		t.Fatalf("edit mapping rule: %v", err)
	}
	if m := marker(fetch("edited")); m != label+"-edited" {
		t.Fatalf("edit not reflected: got %q", m)
	}

	// Delete; verify the field reverts to its default.
	if _, err := lc.DeleteMappingRule(ctx, edited); err != nil {
		t.Fatalf("delete mapping rule: %v", err)
	}
	rev := fetch("reverted")
	if id, _ := intField("id")(rev); id != 0 {
		t.Fatalf("delete did not revert the field to its default (id=%d)", id)
	}
	if marker(rev) != "" {
		t.Fatalf("delete did not clear the marker (%q)", marker(rev))
	}
	reverted = true
}

// TestLiveOntologyVisualFamilyCRUD runs the lifecycle on a throwaway custom
// VISUAL FAMILY — an entity-grouping/visualization record (display only; it has
// no rules, so it affects no detection or real entity). The create wraps the
// model in "visualFamilyDataModel"; delete is by id via DeleteFamilyData.
// Write-gated; auto-skips under a read-only run.
func TestLiveOntologyVisualFamilyCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind:   "visual-family",
		list:   func(ctx context.Context) (RawJSON, error) { return lc.ListVisualFamilies(ctx) },
		idOf:   intField("id"),
		nameOf: strField("family"),
		rename: setField("family"),
		template: func() map[string]any {
			return map[string]any{"description": "secopsctl smoke test", "isCustom": true, "rules": []any{}}
		},
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdateVisualFamily(ctx, map[string]any{"visualFamilyDataModel": o})
		},
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdateVisualFamily(ctx, map[string]any{"visualFamilyDataModel": o})
		},
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			id, _ := intField("id")(o)
			return lc.DeleteFamilyData(ctx, strconv.Itoa(id))
		},
	})
}

// smokeFamilyID looks up a visual family by its "family" name and returns the
// server id. Non-fatal (returns false on any miss) so it is safe to call from a
// t.Cleanup, where the id may have changed (e.g. after an import recreates it).
func smokeFamilyID(ctx context.Context, lc *Client, name string) (int, bool) {
	raw, err := lc.ListVisualFamilies(ctx)
	if err != nil {
		return 0, false
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) != nil {
		return 0, false
	}
	for _, f := range arr {
		if s, _ := f["family"].(string); s == name {
			return intField("id")(f)
		}
	}
	return 0, false
}

// createSmokeFamily creates a throwaway custom visual family named label and
// returns its server id (resolved by re-listing). The caller owns deleting it.
func createSmokeFamily(t *testing.T, ctx context.Context, lc *Client, label string) int {
	t.Helper()
	body := map[string]any{"visualFamilyDataModel": map[string]any{
		"family": label, "description": "secopsctl smoke test", "isCustom": true, "rules": []any{},
	}}
	if _, err := lc.AddOrUpdateVisualFamily(ctx, body); err != nil {
		t.Fatalf("create visual family %q: %v", label, err)
	}
	id, ok := smokeFamilyID(ctx, lc, label)
	if !ok {
		t.Fatalf("created visual family %q not found after create", label)
	}
	return id
}

// familyRuleCount returns the number of rules attached to the named family, or
// -1 if the family is missing.
func familyRuleCount(t *testing.T, ctx context.Context, lc *Client, name string) int {
	t.Helper()
	raw, err := lc.ListVisualFamilies(ctx)
	f := findBy(objects(t, "visual-families", raw, err), func(o map[string]any) bool {
		return strField("family")(o) == name
	})
	if f == nil {
		return -1
	}
	rules, _ := f["rules"].([]any)
	return len(rules)
}

// TestLiveOntologyVisualFamilyRuleCRUD exercises the visual-family-RULE surface
// (AddOrUpdateVisualFamilyRules / DeleteVisualFamilyRule). A rule has no single
// name — it is a composite of source/destination/relation strings keyed to a
// parent family by name — so this is bespoke rather than runLifecycle. It:
// creates a throwaway parent family, borrows a VALID rule shape from a built-in
// family's existing rule (the entity-type + relation strings are Siemplify
// product-ontology constants, not tenant data), re-points it at the throwaway
// family, adds it, verifies it attaches, deletes it, verifies it detaches, then
// deletes the family. Write-gated; cleans up the family even on failure.
func TestLiveOntologyVisualFamilyRuleCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	requireWrite(t)

	// Borrow a valid rule shape from any family that already has one.
	raw, err := lc.ListVisualFamilies(ctx)
	var donor map[string]any
	for _, f := range objects(t, "visual-families", raw, err) {
		rules, _ := f["rules"].([]any)
		if len(rules) > 0 {
			if r0, ok := rules[0].(map[string]any); ok {
				donor = r0
				break
			}
		}
	}
	if donor == nil {
		t.Skip("no existing visual family with a rule to borrow a valid rule shape from")
	}

	label := smokeLabel("visual-family-rule")
	famID := createSmokeFamily(t, ctx, lc, label)
	familyDeleted := false
	t.Cleanup(func() {
		if familyDeleted {
			return
		}
		if id, ok := smokeFamilyID(ctx, lc, label); ok {
			if _, err := lc.DeleteFamilyData(ctx, strconv.Itoa(id)); err != nil {
				t.Logf("cleanup: could not delete throwaway visual family %q: %v", label, err)
			}
		}
	})

	// Build a rule referencing the throwaway family, copying the donor's valid
	// source/destination/relation strings.
	rule := cloneObj(donor)
	delete(rule, "id")
	delete(rule, "creationTimeUnixTimeInMs")
	delete(rule, "modificationTimeUnixTimeInMs")
	rule["visualFamily"] = label

	if _, err := lc.AddOrUpdateVisualFamilyRules(ctx, []any{rule}); err != nil {
		t.Fatalf("add visual family rule: %v", err)
	}
	if n := familyRuleCount(t, ctx, lc, label); n < 1 {
		t.Fatalf("rule not attached to family %q after add (rules=%d)", label, n)
	}

	if _, err := lc.DeleteVisualFamilyRule(ctx, rule); err != nil {
		t.Fatalf("delete visual family rule: %v", err)
	}
	if n := familyRuleCount(t, ctx, lc, label); n != 0 {
		t.Fatalf("rule still present on family %q after delete (rules=%d)", label, n)
	}

	if _, err := lc.DeleteFamilyData(ctx, strconv.Itoa(famID)); err != nil {
		t.Fatalf("delete visual family: %v", err)
	}
	familyDeleted = true
}

// TestLiveOntologyVisualFamilyExportImport exercises the ontology-as-code
// round-trip (ExportVisualFamily -> ImportVisualFamily) on a self-contained
// throwaway family: create it, export it to an {fileName, blob} bundle, delete
// it, import the bundle back, and verify it reappears. Only ever touches the
// throwaway family we created. Write-gated; cleans up via name lookup (the id
// changes when the import recreates it).
func TestLiveOntologyVisualFamilyExportImport(t *testing.T) {
	lc, ctx := liveClient(t)
	requireWrite(t)

	label := smokeLabel("visual-family-io")
	famID := createSmokeFamily(t, ctx, lc, label)
	t.Cleanup(func() {
		if id, ok := smokeFamilyID(ctx, lc, label); ok {
			if _, err := lc.DeleteFamilyData(ctx, strconv.Itoa(id)); err != nil {
				t.Logf("cleanup: could not delete throwaway visual family %q: %v", label, err)
			}
		}
	})

	// Export the throwaway family to a bundle.
	raw, err := lc.ExportVisualFamily(ctx, map[string]any{"familyIds": []any{famID}})
	if err != nil {
		t.Fatalf("export visual family: %v", err)
	}
	var file struct {
		FileName string `json:"fileName"`
		Blob     string `json:"blob"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode export ApiFile: %v", err)
	}
	if file.Blob == "" {
		t.Fatalf("export returned an empty blob")
	}

	// Delete it so the import has to recreate it.
	if _, err := lc.DeleteFamilyData(ctx, strconv.Itoa(famID)); err != nil {
		t.Fatalf("delete before import: %v", err)
	}
	if _, ok := smokeFamilyID(ctx, lc, label); ok {
		t.Fatalf("visual family %q still present after pre-import delete", label)
	}

	// Import the bundle back and verify the family reappears.
	if _, err := lc.ImportVisualFamily(ctx, map[string]any{"fileName": file.FileName, "blob": file.Blob}); err != nil {
		t.Fatalf("import visual family: %v", err)
	}
	if _, ok := smokeFamilyID(ctx, lc, label); !ok {
		t.Fatalf("visual family %q not present after import", label)
	}
}
