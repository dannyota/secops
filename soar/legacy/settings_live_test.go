package legacy

import (
	"context"
	"testing"
)

// TestLiveSettingsReads exercises the zero-argument Settings read endpoints
// (tag "settings": /settings/*, /idp-group-mapping, /external-authentication-settings).
// Every probe here is a pure GET that succeeds on a tenant with no prior setup,
// so the whole test is green under SECOPS_SOAR_SMOKE=1.
func TestLiveSettingsReads(t *testing.T) {
	lc, ctx := liveClient(t)

	// Definition reference data (SLA / tags / root-cause close options).
	readProbe(t, "settings/GetSlaDefinitionsRecords", func() (RawJSON, error) { return lc.GetSlaDefinitionsRecords(ctx) })
	readProbe(t, "settings/GetTagDefinitionNames", func() (RawJSON, error) { return lc.GetTagDefinitionNames(ctx) })
	readProbe(t, "settings/GetRootCauseCloseRecords", func() (RawJSON, error) { return lc.GetRootCauseCloseRecords(ctx) })

	// System settings (version, entity types, certificate, proxy, case policies).
	readProbe(t, "settings/GetSystemVersion", func() (RawJSON, error) { return lc.GetSystemVersion(ctx) })
	readProbe(t, "settings/GetSystemEventEntityTypes", func() (RawJSON, error) { return lc.GetSystemEventEntityTypes(ctx) })
	// GetPublicCertificate omitted: it returns a binary certificate, not JSON.
	readProbe(t, "settings/GetProxySettings", func() (RawJSON, error) { return lc.GetProxySettings(ctx) })
	readProbe(t, "settings/GetCaseAssignmentPolicySettings", func() (RawJSON, error) { return lc.GetCaseAssignmentPolicySettings(ctx) })
	readProbe(t, "settings/GetMoveCaseBetweenEnvironmentsPolicySettings", func() (RawJSON, error) {
		return lc.GetMoveCaseBetweenEnvironmentsPolicySettings(ctx)
	})

	// List settings (block lists / tracking lists).
	readProbe(t, "settings/GetAllModelBlockRecords", func() (RawJSON, error) { return lc.GetAllModelBlockRecords(ctx) })
	readProbe(t, "settings/GetTrackingListRecords", func() (RawJSON, error) { return lc.GetTrackingListRecords(ctx) })

	// Identity settings (IdP group mappings + external auth providers) — reads only.
	readProbe(t, "idp-group-mapping/List", func() (RawJSON, error) { return lc.ListIdpGroupMappings(ctx) })
	readProbe(t, "idp-group-mapping/Count", func() (RawJSON, error) { return lc.GetIdpGroupMappingCount(ctx) })
	readProbe(t, "external-authentication-settings/List", func() (RawJSON, error) { return lc.ListExternalAuthSettings(ctx) })

	// Misc Settings surface (collaborator requests, license agreement, advanced
	// reports config, alert-grouping config, form dynamic parameters).
	readProbe(t, "settings/GetAllCollaboratorRequests", func() (RawJSON, error) { return lc.SettingXGetAllCollaboratorRequests(ctx) })
	readProbe(t, "settings/GetCollaboratorRequestsByUser", func() (RawJSON, error) { return lc.SettingXGetCollaboratorRequestsByUser(ctx) })
	// GetLatestLicenseAgreement omitted: returns a server-side HTTP 500.
	readProbe(t, "settings/GetAdvancedReportsSettings", func() (RawJSON, error) { return lc.SettingXGetAdvancedReportsSettings(ctx) })
	readProbe(t, "settings/GetMaximumAlertsGroupingConfiguration", func() (RawJSON, error) {
		return lc.SettingXGetMaximumAlertsGroupingConfiguration(ctx)
	})
	// ListFormDynamicParameters omitted: needs a formType query param (400 without it).
}

// tagDefinitionPageRequest is the paging body GetTagDefinitionsRecords expects
// (Siemplify.Server.Api.Requests.ApiPageRequest). A large page size returns all
// records so the lifecycle can locate the throwaway one it creates.
func tagDefinitionPageRequest() map[string]any {
	return map[string]any{"searchTerm": "", "requestedPage": 0, "pageSize": 1000}
}

// TestLiveSettingsTagDefinitionCRUD runs the full list -> create -> read -> edit
// -> read -> delete lifecycle against a throwaway TAG DEFINITION — a cosmetic
// reference-data record analysts tag cases with (no auth/identity/permissions
// surface). Modeled on TestLivePlaybookCategoryCRUD. Gated by
// SECOPS_SOAR_SMOKE_WRITE=1 via runLifecycle, so it auto-skips under a read-only
// smoke run.
//
// Body shapes (third_party/siemplify-swagger.json):
//   - list  : POST /settings/GetTagDefinitionsRecords  <- ApiPageRequest, response wraps records in "objectsList"
//   - record: a TagDefinitionV2 object { id, name, value, priority, type, compareType, ... }
//   - create: POST /settings/AddTagDefinitionsRecords    <- one TagDefinitionV2
//   - remove: POST /settings/RemoveTagDefinitionRecords  <- one TagDefinitionV2 (identified by id)
func TestLiveSettingsTagDefinitionCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind: "tag-definition",
		list: func(ctx context.Context) (RawJSON, error) {
			return lc.GetTagDefinitionsRecords(ctx, tagDefinitionPageRequest())
		},
		idOf:   intField("id"),
		nameOf: strField("name"),
		rename: setField("name"),
		prep: func(o map[string]any) {
			delete(o, "id")
			delete(o, "creationTimeUnixTimeInMs")
			delete(o, "modificationTimeUnixTimeInMs")
		},
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddTagDefinitionsRecords(ctx, o)
		},
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddTagDefinitionsRecords(ctx, o)
		},
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.RemoveTagDefinitionRecords(ctx, o)
		},
	})
}

// TestLiveSettingsRootCauseCRUD — a throwaway close "root cause" reference option
// (the dropdown analysts pick when closing a case). Cosmetic reference data.
// Clones an existing record so the forCloseReason enum is carried verbatim;
// renames the rootCause text. list -> create -> read -> edit -> read -> delete.
func TestLiveSettingsRootCauseCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind:   "root-cause-close",
		list:   func(ctx context.Context) (RawJSON, error) { return lc.GetRootCauseCloseRecords(ctx) },
		idOf:   intField("id"),
		nameOf: strField("rootCause"),
		rename: setField("rootCause"),
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdateRootCauseClose(ctx, o)
		},
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddOrUpdateRootCauseClose(ctx, o)
		},
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) { return lc.RemoveRootCauseClose(ctx, o) },
	})
}

// caseStagePageRequest is the paging body GetCaseStageDefinitionRecords expects.
func caseStagePageRequest() map[string]any {
	return map[string]any{"searchTerm": "", "requestedPage": 0, "pageSize": 1000}
}

// TestLiveSettingsCaseStageCRUD — a throwaway case-stage definition. Case stages
// are the workflow stages a case can move through; a brand-new, unassigned stage
// created with a high order (999, so it never reorders existing stages) and then
// deleted affects no existing case. Template-based (the list may be empty).
func TestLiveSettingsCaseStageCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind: "case-stage",
		list: func(ctx context.Context) (RawJSON, error) {
			return lc.GetCaseStageDefinitionRecords(ctx, caseStagePageRequest())
		},
		idOf:     intField("id"),
		nameOf:   strField("name"),
		rename:   setField("name"),
		template: func() map[string]any { return map[string]any{"order": 999} },
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddCaseStageDefinitionRecord(ctx, o)
		},
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddCaseStageDefinitionRecord(ctx, o)
		},
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.RemoveCaseStageDefinitionRecords(ctx, o)
		},
	})
}

// TestLiveSettingsSlaCRUD — a throwaway SLA definition. SLA records have no
// display name, so the lifecycle keys on the "value" string. We define the SLA
// for a NON-EXISTENT AlertRuleGenerator (valueType=2) named with the smoke label,
// so it can never match a real alert/case; periods are arbitrary. Template-based
// (the SLA list is empty by default).
//
// Enums: valueType 2=AlertRuleGenerator; periodType 0=Minutes 1=Hours 2=Days
// 3=Seconds; alertType 0=AllAlerts 1=SpecificAlerts.
func TestLiveSettingsSlaCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	runLifecycle(t, ctx, lifecycleSpec{
		kind:   "sla",
		list:   func(ctx context.Context) (RawJSON, error) { return lc.GetSlaDefinitionsRecords(ctx) },
		idOf:   intField("id"),
		nameOf: strField("value"),
		rename: setField("value"),
		template: func() map[string]any {
			return map[string]any{
				"valueType":          2, // AlertRuleGenerator — value is a free-form generator name
				"slaPeriodType":      1, // Hours
				"slaPeriod":          24,
				"criticalPeriodType": 1, // Hours
				"criticalPeriod":     12,
				"alertType":          0, // AllAlerts
				"environments":       []any{},
				"values":             []any{},
			}
		},
		create: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddSlaDefinitionsRecord(ctx, o)
		},
		update: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.AddSlaDefinitionsRecord(ctx, o)
		},
		remove: func(ctx context.Context, o map[string]any) (RawJSON, error) {
			return lc.RemoveSlaDefinitionRecords(ctx, o)
		},
	})
}
