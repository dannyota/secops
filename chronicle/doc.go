// Package chronicle is an unofficial Go SDK for the Google SecOps (Chronicle
// SIEM) modern REST API on the chronicle.googleapis.com host. The host serves
// v1 / v1beta / v1alpha; the SDK pins the working version per surface (prefer
// v1 > v1beta > v1alpha), with v1alpha as the default — see versions.go.
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
// Beyond parity: the *_write.go writers (rules, reflists, datatables, feeds,
// parsers, dashboards), alert.go, entity.go, investigations.go, ingest.go,
// data_export.go, stats.go, nl_search.go, gemini.go, watchlist.go, retrohunt.go,
// rule_exclusion.go, rule_results.go, ruletest.go, parser_extension.go,
// log_pipeline.go, log_search.go, log_meta.go, forwarders.go, ti.go (threat intel
// + IoCs), rbac.go (governance), curated_rules.go + curated_write.go, legacy.go
// (the SOAR-int ⇄ SIEM-uuid case bridge) and case.go (the alternate Chronicle-host
// cases path — see that file).
//
// Per-file build/validation status lives in docs/CATALOG.md; the forward plan
// and sequencing is docs/ROADMAP.md.
//
// # Not yet built
//
// Log-type classify/describe; and the CLI verbs over the already-built
// operational SDK (alerts act, stats, nl-search).
package chronicle
