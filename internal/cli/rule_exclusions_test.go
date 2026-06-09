package cli

import (
	"testing"

	"danny.vn/secops/chronicle"
)

func TestBuildRuleExclusionDeployUpdate(t *testing.T) {
	current := &chronicle.RuleExclusionDeployment{Archived: true, Etag: "etag-1"}

	t.Run("enable unarchives", func(t *testing.T) {
		upd, desired, err := buildRuleExclusionDeployUpdate(true, false, false, current)
		if err != nil {
			t.Fatal(err)
		}
		if upd.Enabled == nil || !*upd.Enabled || upd.Archived == nil || *upd.Archived {
			t.Fatalf("update = %+v", upd)
		}
		if upd.Etag != "etag-1" || desired != "enabled=true archived=false" {
			t.Fatalf("etag/desired = %q %q", upd.Etag, desired)
		}
	})

	t.Run("disable preserves archive state", func(t *testing.T) {
		upd, desired, err := buildRuleExclusionDeployUpdate(false, true, false, current)
		if err != nil {
			t.Fatal(err)
		}
		if upd.Enabled == nil || *upd.Enabled || upd.Archived != nil {
			t.Fatalf("update = %+v", upd)
		}
		if desired != "enabled=false archived=true" {
			t.Fatalf("desired = %q", desired)
		}
	})

	t.Run("archive disables", func(t *testing.T) {
		upd, desired, err := buildRuleExclusionDeployUpdate(false, false, true, current)
		if err != nil {
			t.Fatal(err)
		}
		if upd.Enabled == nil || *upd.Enabled || upd.Archived == nil || !*upd.Archived {
			t.Fatalf("update = %+v", upd)
		}
		if desired != "enabled=false archived=true" {
			t.Fatalf("desired = %q", desired)
		}
	})
}

func TestBuildRuleExclusionDeployUpdateRequiresOneAction(t *testing.T) {
	if _, _, err := buildRuleExclusionDeployUpdate(false, false, false, nil); err == nil {
		t.Fatal("expected missing action error")
	}
	if _, _, err := buildRuleExclusionDeployUpdate(true, true, false, nil); err == nil {
		t.Fatal("expected conflicting action error")
	}
}
