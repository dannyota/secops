package soar_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestLiveCaseDataWriteSmoke validates the modern v1alpha "Case Data" writes
// (Wave 16) on the SOAR host — create→get→delete on inert, self-identifying
// throwaways: a propertySchemaDefinition, a custom field, and a calculated-field
// definition (which needs a Free-Text custom field as its target, so a target
// field is created first and deleted last). The marker is alphanumeric so it is a
// valid CaseCustom.<field> identifier. Cleanup sweeps calc → customFields →
// propertySchema (dependency order) and fails loudly on residue. Gated on
// SECOPS_SOAR_SMOKE=1 + SECOPS_SOAR_SMOKE_WRITE=1.
func TestLiveCaseDataWriteSmoke(t *testing.T) {
	c, ctx := liveClient(t)
	if os.Getenv("SECOPS_SOAR_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SOAR_SMOKE_WRITE=1 to run (creates + deletes throwaway case-data objects)")
	}
	tok := fmt.Sprintf("secopsctlsmoke%d", time.Now().UnixNano())

	sweepers := []struct {
		name string
		list func() ([]json.RawMessage, error)
		del  func(context.Context, string) error
	}{
		{"calculatedFieldDefinitions", func() ([]json.RawMessage, error) { return c.ListCalculatedFieldDefinitions(ctx) }, c.DeleteCalculatedFieldDefinition},
		{"customFields", func() ([]json.RawMessage, error) { return c.ListCustomFields(ctx) }, c.DeleteCustomField},
		{"propertySchemaDefinitions", func() ([]json.RawMessage, error) { return c.ListPropertySchemaDefinitions(ctx) }, c.DeletePropertySchemaDefinition},
	}
	t.Cleanup(func() {
		for _, s := range sweepers {
			items, err := s.list()
			if err != nil {
				t.Errorf("cleanup: list %s failed, residue may remain: %v", s.name, err)
				continue
			}
			for _, it := range items {
				if bytes.Contains(it, []byte(tok)) {
					id := idFromRaw(it)
					if err := s.del(ctx, id); err != nil {
						t.Errorf("CLEANUP FAILED to delete leftover %s/%s: %v", s.name, id, err)
					} else {
						t.Logf("cleanup: removed leftover %s/%s", s.name, id)
					}
				}
			}
		}
	})

	// 1. propertySchemaDefinitions: create → get → delete (all-scalar).
	ps, err := c.CreatePropertySchemaDefinition(ctx, map[string]any{
		"rawFieldName": tok, "displayName": tok, "groupName": tok,
	})
	if err != nil {
		t.Errorf("[propertySchemaDefinitions] create (no 500 expected): %v", err)
	} else {
		id := idFromRaw(ps)
		if got, err := c.GetPropertySchemaDefinition(ctx, id); err != nil || !bytes.Contains(got, []byte(tok)) {
			t.Errorf("[propertySchemaDefinitions] get(%s): err=%v", id, err)
		}
		if err := c.DeletePropertySchemaDefinition(ctx, id); err != nil {
			t.Errorf("[propertySchemaDefinitions] delete(%s): %v", id, err)
		} else {
			t.Logf("[propertySchemaDefinitions] create→get→delete OK (id=%s)", id)
		}
	}

	// 2. customFields: create → get → delete (standalone FREE_TEXT, Case scope).
	cf, err := c.CreateCustomField(ctx, map[string]any{
		"displayName": tok + "cf", "type": "FREE_TEXT", "scopes": "Case",
	})
	if err != nil {
		t.Errorf("[customFields] create (no 500 expected): %v", err)
	} else {
		id := idFromRaw(cf)
		if got, err := c.GetCustomField(ctx, id); err != nil || !bytes.Contains(got, []byte(tok)) {
			t.Errorf("[customFields] get(%s): err=%v", id, err)
		}
		if err := c.DeleteCustomField(ctx, id); err != nil {
			t.Errorf("[customFields] delete(%s): %v", id, err)
		} else {
			t.Logf("[customFields] create→get→delete OK (id=%s)", id)
		}
	}

	// 3. calculatedFieldDefinitions: create a Free-Text Case field as target, then
	// the calc, then tear down calc → field (the calc depends on the field).
	tf, err := c.CreateCustomField(ctx, map[string]any{
		"displayName": tok, "type": "FREE_TEXT", "scopes": "Case",
	})
	if err != nil {
		t.Errorf("[calc] create target field (no 500 expected): %v", err)
		return
	}
	tfID := idFromRaw(tf)
	calc, err := c.CreateCalculatedFieldDefinition(ctx, map[string]any{
		"displayName": tok + "calc", "calculationType": "SET_VALUE", "outputType": "TEXT",
		"targetField": "CaseCustom." + tok, "formulaExpression": `"Standard"`,
	})
	if err != nil {
		t.Errorf("[calculatedFieldDefinitions] create (no 500 expected): %v", err)
	} else {
		id := idFromRaw(calc)
		if got, err := c.GetCalculatedFieldDefinition(ctx, id); err != nil || !bytes.Contains(got, []byte(tok)) {
			t.Errorf("[calculatedFieldDefinitions] get(%s): err=%v", id, err)
		}
		if err := c.DeleteCalculatedFieldDefinition(ctx, id); err != nil {
			t.Errorf("[calculatedFieldDefinitions] delete(%s): %v", id, err)
		} else {
			t.Logf("[calculatedFieldDefinitions] create→get→delete OK (id=%s, target CaseCustom.%s)", id, tok)
		}
	}
	if err := c.DeleteCustomField(ctx, tfID); err != nil {
		t.Errorf("[calc] delete target field(%s): %v", tfID, err)
	}
}
