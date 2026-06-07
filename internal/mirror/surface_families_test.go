package mirror

import (
	"testing"

	"danny.vn/secops/chronicle"
)

// TestSurfaceFamiliesConsistent enforces the invariants that keep the registry
// honest: host↔auth↔plane pairings, and the generation↔version rules (Legacy has
// no version; the SOAR host is v1alpha-only). A new entry that violates the model
// fails here.
func TestSurfaceFamiliesConsistent(t *testing.T) {
	validArea := map[Area]bool{AreaSIEM: true, AreaSOAR: true, AreaOther: true}
	validLane := map[FamilyLane]bool{LaneReconcile: true, LaneRaw: true, LaneImperative: true, LaneOperational: true}
	validStatus := map[FamilyStatus]bool{StatusDesigned: true, StatusBuilt: true, StatusValidated: true, StatusReadOnly: true, StatusBlocked: true}
	validVersion := map[string]bool{"v1": true, "v1beta": true, "v1alpha": true}

	for _, f := range SurfaceFamilies {
		if f.Name == "" || f.SDKLocation == "" {
			t.Errorf("family %+v: empty Name or SDKLocation", f)
		}
		if !validArea[f.Area] {
			t.Errorf("%s: invalid Area %q", f.Name, f.Area)
		}
		if !validLane[f.Lane] {
			t.Errorf("%s: invalid Lane %q", f.Name, f.Lane)
		}
		if !validStatus[f.Status] {
			t.Errorf("%s: invalid Status %q", f.Name, f.Status)
		}

		// Host ↔ Auth.
		switch f.Host {
		case HostChronicle:
			if f.Auth != AuthADC {
				t.Errorf("%s: chronicle host must use ADC, got %q", f.Name, f.Auth)
			}
		case HostSiemplify:
			if f.Auth != AuthAppKey {
				t.Errorf("%s: siemplify host must use AppKey, got %q", f.Name, f.Auth)
			}
		default:
			t.Errorf("%s: invalid Host %q", f.Name, f.Host)
		}

		// Plane ↔ Host.
		switch f.Plane {
		case PlaneSIEM:
			if f.Host != HostChronicle {
				t.Errorf("%s: SIEM plane must be on the chronicle host", f.Name)
			}
			if f.Generation != GenNew {
				t.Errorf("%s: chronicle is modern-only — SIEM plane must be New generation", f.Name)
			}
		case PlaneSOARLegacy, PlaneSOARModern:
			if f.Host != HostSiemplify {
				t.Errorf("%s: SOAR plane must be on the siemplify host", f.Name)
			}
		default:
			t.Errorf("%s: invalid Plane %q", f.Name, f.Plane)
		}

		// Generation ↔ APIVersion.
		switch f.Generation {
		case GenLegacy:
			if f.APIVersion != "" {
				t.Errorf("%s: Legacy generation has no version ladder, want APIVersion=\"\", got %q", f.Name, f.APIVersion)
			}
		case GenNew:
			if !validVersion[f.APIVersion] {
				t.Errorf("%s: New generation needs a v1/v1beta/v1alpha version, got %q", f.Name, f.APIVersion)
			}
		default:
			t.Errorf("%s: invalid Generation %q", f.Name, f.Generation)
		}

		// The SOAR host serves v1alpha only.
		if f.Plane == PlaneSOARModern && f.APIVersion != "v1alpha" {
			t.Errorf("%s: SOAR-modern plane is v1alpha-only, got %q", f.Name, f.APIVersion)
		}
	}
}

// TestChronicleAPIVersionsGolden locks chronicle.APIVersions (versions.go) to the
// versions documented in docs/ARCHITECTURE.md §6. If a pin changes here, this test
// fails until the docs are updated in the same change — the "cannot silently
// drift" guarantee the registry is supposed to provide.
func TestChronicleAPIVersionsGolden(t *testing.T) {
	want := map[string]string{
		"rules":               "v1alpha",
		"reference_lists":     "v1alpha",
		"data_tables":         "v1alpha",
		"feeds":               "v1alpha",
		"parsers":             "v1alpha",
		"dashboards":          "v1alpha",
		"rule_exclusions":     "v1alpha",
		"search":              "v1alpha",
		"entities":            "v1alpha",
		"alerts":              "v1alpha",
		"metric_definitions":  "v1alpha",
		"scheduled_reports":   "v1alpha",
		"datataps":            "v1alpha",
		"error_notifications": "v1alpha",
		"enrichment_controls": "v1alpha",
		"federation_groups":   "v1alpha",
		"tenants":             "v1alpha",
		"curated_rules":       "v1alpha", // v1alpha ONLY (v1/v1beta 404)
		"threat_intel":        "v1",
		"governance":          "v1",
		"watchlists":          "v1",
		"forwarders":          "v1beta",
		"bigquery_export":     "v1",
		"coverage":            "v1",
		// alternate, unused chronicle-host cases path (500s at every version)
		"cases_chronicle_alt": "v1beta",
	}
	if len(chronicle.APIVersions) != len(want) {
		t.Errorf("chronicle.APIVersions has %d keys, docs §6 expects %d — update one to match the other",
			len(chronicle.APIVersions), len(want))
	}
	for k, v := range want {
		if got := chronicle.APIVersions[k]; got != v {
			t.Errorf("chronicle.APIVersions[%q] = %q, docs §6 expects %q", k, got, v)
		}
	}
	for k := range chronicle.APIVersions {
		if _, ok := want[k]; !ok {
			t.Errorf("chronicle.APIVersions has unexpected key %q not covered by the §6 golden set", k)
		}
	}
}

// TestEveryReconcileSurfaceHasFamily guarantees that every engine-backed reconcile
// surface (SIEM and SOAR) has a registry entry — so adding a surface without
// registering it (the silent-drift failure mode) fails the build's test run.
func TestEveryReconcileSurfaceHasFamily(t *testing.T) {
	reconcileFamily := map[string]bool{}
	for _, f := range SurfaceFamilies {
		if f.Lane == LaneReconcile {
			reconcileFamily[f.Name] = true
		}
	}
	for _, d := range soarSurfaceDefs {
		if !reconcileFamily[d.name] {
			t.Errorf("SOAR reconcile surface %q has no SurfaceFamily entry (Lane=reconcile)", d.name)
		}
	}
	for _, d := range siemSurfaceDefs {
		if !reconcileFamily[d.name] {
			t.Errorf("SIEM reconcile surface %q has no SurfaceFamily entry (Lane=reconcile)", d.name)
		}
	}
}
