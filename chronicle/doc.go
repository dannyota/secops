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
// # Implemented surface
//
// Wave 1 (parity with the legacy tool): rules.go, reflists.go, datatables.go,
// dashboards.go, curated.go, feeds.go, parsers.go, search.go.
//
// Beyond parity (Wave 2, mostly landed): the *_write.go writers (rules,
// reflists, datatables, feeds, parsers, dashboards), case.go, alert.go,
// entity.go, investigations.go, ingest.go, data_export.go, stats.go,
// nl_search.go, gemini.go, watchlist.go, retrohunt.go, rule_exclusion.go,
// rule_results.go, parser_extension.go, log_pipeline.go, log_search.go,
// log_meta.go, legacy.go (the SOAR-int ⇄ SIEM-uuid case bridge).
//
// Per-file build/validation status lives in docs/CATALOG.md; the forward plan
// and sequencing is docs/ROADMAP.md.
//
// # Not yet built
//
// Forwarders and log-type classify/describe; and the CLI verbs over the
// already-built operational SDK (alerts/cases act, stats, nl-search).
package chronicle
