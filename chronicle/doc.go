// Package chronicle is an unofficial Go SDK for the Google SecOps (Chronicle
// SIEM) v1alpha API.
//
// It is a pure API client: methods take typed requests and return typed
// responses, performing no local file I/O. The pull/push file-mirroring layer
// lives in internal/mirror; future SecOps products (SOAR, etc.) live in sibling
// packages so this one stays focused on the SIEM API.
//
// # Design notes (improving on the official Python wrapper)
//
//   - Resource project form is explicit per endpoint (see resource.go), not
//     discovered via 404-then-retry.
//   - Responses are typed structs, not map[string]any with .get() lookups.
//   - Failures surface as a typed *APIError carrying status + body.
//   - One generic paginator (paginate) handles nextPageToken everywhere.
//   - Auth is split and lazy (see package danny.vn/secops/auth).
//
// # Implemented surface (Wave 1 — parity with the legacy tool)
//
//	rules.go       ListRules, GetRule, ValidateRule, CreateRule,
//	               ListRuleDeployments, UpdateRuleDeployment
//	reflists.go    ListReferenceLists (FULL view)
//	datatables.go  ListDataTables, ListDataTableRows
//	dashboards.go  ListNativeDashboards, ExportDashboard
//	curated.go     curated rule-set categories / sets / deployments,
//	               ListFeaturedContentRules
//	feeds.go       ListFeeds
//	parsers.go     ListParsers (per log type)
//	search.go      SearchUDM
//
// # Reserved for later waves (see docs/ROADMAP.md)
//
// Wave 2 finishes the secops-wrapper surface: entity.go (summarize_entity,
// IoCs), rule writes + retrohunts + exclusions + detections/errors, cases.go &
// alerts.go (+ bulk ops), reference-list/data-table/feed/parser/dashboard
// writes, ingest.go (ingest_log/ingest_udm), forwarders.go, log_pipeline.go,
// data_export.go, watchlists.go, stats.go, nl_search.go, gemini.go.
package chronicle
