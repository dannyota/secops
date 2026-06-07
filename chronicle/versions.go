package chronicle

// API version pins for the Chronicle (SIEM) host, in one place.
//
// The chronicle.googleapis.com host serves v1 / v1beta / v1alpha. We prefer
// v1 > v1beta > v1alpha and pin the highest version that answers per surface
// family; DefaultAPIVersion (client.go) is the fallback for the families that
// ride the default (rules, reference_lists, data_tables, feeds, parsers,
// dashboards, rule_exclusions, curated*, search, entity, alerts, ingest, the
// legacy: bridge).
//
// This is the single source of truth for SIEM versions. The surface-family
// registry derives APIVersion from APIVersions below, a drift-guard test asserts
// the two agree, and docs/design/architecture.md §6 mirrors it. When an endpoint moves,
// change it HERE (and re-run the version probe).
const (
	tiAPIVersion             = "v1"     // threatCollections + modern IoCs (all three answer → newest)
	rbacAPIVersion           = "v1"     // dataAccessLabels/Scopes + riskConfig (newest that answers)
	watchlistsAPIVersion     = "v1"     // entity watchlists, reads and writes (all three answer → newest)
	forwardersAPIVersion     = "v1beta" // forwarders + collectors (v1 404s)
	bigQueryExportAPIVersion = "v1"     // Advanced BigQuery Export get/update (v1 + v1alpha answer → newest)
	coverageAPIVersion       = "v1"     // MITRE coverageDetails list (v1 + v1alpha answer → newest)
)

// APIVersions maps every Chronicle-host surface family to the API version it is
// pinned to, keyed by the family name used in the surface registry and
// docs/design/architecture.md §6.
var APIVersions = map[string]string{
	// Ride the v1alpha default (DefaultAPIVersion).
	"rules":               DefaultAPIVersion,
	"reference_lists":     DefaultAPIVersion,
	"data_tables":         DefaultAPIVersion,
	"feeds":               DefaultAPIVersion,
	"parsers":             DefaultAPIVersion,
	"dashboards":          DefaultAPIVersion,
	"rule_exclusions":     DefaultAPIVersion,
	"search":              DefaultAPIVersion,
	"entities":            DefaultAPIVersion,
	"alerts":              DefaultAPIVersion,
	"metric_definitions":  DefaultAPIVersion,
	"scheduled_reports":   DefaultAPIVersion,
	"datataps":            DefaultAPIVersion,
	"error_notifications": DefaultAPIVersion,
	"enrichment_controls": DefaultAPIVersion,
	"federation_groups":   DefaultAPIVersion,
	"tenants":             DefaultAPIVersion,
	"curated_rules":       DefaultAPIVersion, // v1alpha ONLY here (v1/v1beta 404)
	// Pinned per surface (newest that answers).
	"threat_intel":    tiAPIVersion,
	"governance":      rbacAPIVersion,
	"watchlists":      watchlistsAPIVersion,
	"forwarders":      forwardersAPIVersion,
	"bigquery_export": bigQueryExportAPIVersion,
	"coverage":        coverageAPIVersion,
	// Alternate, unused Chronicle-host cases path: 500s at every version, so the
	// v1beta segment is not a working pin (see case.go). The working case path is
	// on the SOAR host (soar.ListCases).
	"cases_chronicle_alt": caseAPIVersion,
}

// APIVersionFor returns the pinned API version for a Chronicle-host surface
// family, or DefaultAPIVersion when the family rides the default or is unknown.
func APIVersionFor(family string) string {
	if v, ok := APIVersions[family]; ok {
		return v
	}
	return DefaultAPIVersion
}
