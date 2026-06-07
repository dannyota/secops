package mirror

import "danny.vn/secops/chronicle"

// The surface-family registry: one declarative entry per API family describing
// where it lives and how mature it is. It is the spine docs/CATALOG.md (the
// by-function status matrix) and docs/ARCHITECTURE.md §6 (the version table)
// derive from — the single place the map, the docs, and the code agree. A
// drift-guard test (surface_families_test.go) asserts the entries stay internally
// consistent, that SIEM versions are sourced from chronicle.APIVersions
// (versions.go), and that every reconcile surface is represented.
//
// Two orthogonal axes, both tracked here (do not conflate them):
//   - Area   — the FUNCTION grouping in CATALOG: SIEM / SOAR / Other.
//   - Plane  — the (host, transport) pairing in SURFACES: SIEM (chronicle/ADC),
//     SOAR-legacy (siemplify external API), SOAR-modern (siemplify v1alpha).
//
// A function can sit in one Area on a different Plane: the chronicle-host cases
// path is Area=SOAR (it's the cases function) on Plane=SIEM (the chronicle host).

// Area is the top-level functional grouping in docs/CATALOG.md.
type Area string

const (
	AreaSIEM  Area = "SIEM"
	AreaSOAR  Area = "SOAR"
	AreaOther Area = "Other" // cross-cutting: Threat Intel, Content Hub
)

// Plane is the (product, transport) pairing from docs/SURFACES.md.
type Plane string

const (
	PlaneSIEM       Plane = "SIEM"        // chronicle.googleapis.com, ADC/OAuth
	PlaneSOARLegacy Plane = "SOAR-legacy" // *.siemplify-soar.com, external /api/external/v1
	PlaneSOARModern Plane = "SOAR-modern" // *.siemplify-soar.com, v1alpha
)

// Host is the domain a family's calls go to.
type Host string

const (
	HostChronicle Host = "chronicle" // chronicle.googleapis.com
	HostSiemplify Host = "siemplify" // *.siemplify-soar.com
)

// Auth is the credential a family authenticates with.
type Auth string

const (
	AuthADC    Auth = "ADC"    // Google OAuth / ADC token
	AuthAppKey Auth = "AppKey" // Siemplify AppKey
)

// Generation is the API generation a family is operated on.
type Generation string

const (
	GenNew    Generation = "New"    // modern Google REST (projects/…/instances/… shape)
	GenLegacy Generation = "Legacy" // Siemplify external /api/external/v1
)

// FamilyLane is the §3 modeling lane (named FamilyLane to avoid colliding with the
// reconcile-engine vocabulary).
type FamilyLane string

const (
	LaneReconcile   FamilyLane = "reconcile"
	LaneRaw         FamilyLane = "raw"
	LaneImperative  FamilyLane = "imperative"
	LaneOperational FamilyLane = "operational"
)

// FamilyStatus mirrors the docs/CATALOG.md status legend.
type FamilyStatus string

const (
	StatusDesigned  FamilyStatus = "designed"  // 📐
	StatusBuilt     FamilyStatus = "built"     // 🔨
	StatusValidated FamilyStatus = "validated" // ✅
	StatusReadOnly  FamilyStatus = "read-only" // 🔒
	StatusBlocked   FamilyStatus = "blocked"   // ⛔
)

// SurfaceFamily is one API family's home and maturity.
type SurfaceFamily struct {
	Name        string
	Area        Area
	Plane       Plane
	Host        Host
	Auth        Auth
	Generation  Generation
	APIVersion  string // "" = N/A (the Legacy external API has no version ladder)
	Lane        FamilyLane
	Status      FamilyStatus
	SDKLocation string
}

// siemFamily builds a SIEM-plane (chronicle/ADC, New-generation) family whose API
// version is sourced from chronicle.APIVersions (versions.go) by key, so the
// registry version can never drift from the SDK pin.
func siemFamily(name, versionKey string, area Area, lane FamilyLane, status FamilyStatus, sdk string) SurfaceFamily {
	return SurfaceFamily{
		Name: name, Area: area, Plane: PlaneSIEM, Host: HostChronicle, Auth: AuthADC,
		Generation: GenNew, APIVersion: chronicle.APIVersionFor(versionKey),
		Lane: lane, Status: status, SDKLocation: sdk,
	}
}

// soarModern builds a SOAR-modern (siemplify/AppKey, v1alpha) family.
func soarModern(name string, area Area, lane FamilyLane, status FamilyStatus, sdk string) SurfaceFamily {
	return SurfaceFamily{
		Name: name, Area: area, Plane: PlaneSOARModern, Host: HostSiemplify, Auth: AuthAppKey,
		Generation: GenNew, APIVersion: "v1alpha", // the SOAR host serves v1alpha only
		Lane: lane, Status: status, SDKLocation: sdk,
	}
}

// soarLegacy builds a SOAR-legacy (siemplify/AppKey, external API) family. The
// external API has no version ladder, so APIVersion is "".
func soarLegacy(name string, lane FamilyLane, status FamilyStatus, sdk string) SurfaceFamily {
	return SurfaceFamily{
		Name: name, Area: AreaSOAR, Plane: PlaneSOARLegacy, Host: HostSiemplify, Auth: AuthAppKey,
		Generation: GenLegacy, APIVersion: "", Lane: lane, Status: status, SDKLocation: sdk,
	}
}

// SurfaceFamilies is the full registry. Keep it in lock-step with CATALOG.md and
// the §6 version table; the drift-guard test enforces the version + consistency
// invariants.
var SurfaceFamilies = buildSurfaceFamilies()

func buildSurfaceFamilies() []SurfaceFamily {
	explicit := []SurfaceFamily{
		// --- SIEM (chronicle host, ADC) -------------------------------------
		siemFamily("rules", "rules", AreaSIEM, LaneReconcile, StatusValidated, "chronicle/rules.go"),
		siemFamily("reference_lists", "reference_lists", AreaSIEM, LaneReconcile, StatusValidated, "chronicle/reflists.go"),
		siemFamily("data_tables", "data_tables", AreaSIEM, LaneReconcile, StatusValidated, "chronicle/datatables.go"),
		siemFamily("feeds", "feeds", AreaSIEM, LaneReconcile, StatusValidated, "chronicle/feeds.go"),
		siemFamily("parsers", "parsers", AreaSIEM, LaneReconcile, StatusValidated, "chronicle/parsers.go"),
		siemFamily("dashboards", "dashboards", AreaSIEM, LaneReconcile, StatusValidated, "chronicle/dashboards.go"),
		siemFamily("rule_exclusions", "rule_exclusions", AreaSIEM, LaneReconcile, StatusValidated, "chronicle/rule_exclusion.go"),
		siemFamily("metric_definitions", "metric_definitions", AreaSIEM, LaneReconcile, StatusBuilt, "chronicle/metrics.go"),
		siemFamily("scheduled_reports", "scheduled_reports", AreaSIEM, LaneReconcile, StatusBuilt, "chronicle/scheduled_reports.go"),
		siemFamily("datataps", "datataps", AreaSIEM, LaneReconcile, StatusValidated, "chronicle/datataps.go"),
		siemFamily("error_notifications", "error_notifications", AreaSIEM, LaneReconcile, StatusBuilt, "chronicle/error_notifications.go"),
		siemFamily("enrichment_controls", "enrichment_controls", AreaSIEM, LaneImperative, StatusBuilt, "chronicle/enrichment_controls.go"),
		siemFamily("curated_rules", "curated_rules", AreaSIEM, LaneImperative, StatusValidated, "chronicle/curated_rules.go"),
		siemFamily("forwarders", "forwarders", AreaSIEM, LaneReconcile, StatusValidated, "chronicle/forwarders.go"),
		siemFamily("governance", "governance", AreaSIEM, LaneImperative, StatusBuilt, "chronicle/rbac.go"),
		siemFamily("events", "search", AreaSIEM, LaneOperational, StatusBuilt, "chronicle/search.go"),
		siemFamily("alerts", "alerts", AreaSIEM, LaneOperational, StatusValidated, "chronicle/alert.go"),
		siemFamily("entities", "entities", AreaSIEM, LaneOperational, StatusDesigned, "chronicle/entity.go"),
		siemFamily("watchlists", "watchlists", AreaSIEM, LaneOperational, StatusValidated, "chronicle/watchlist.go"),

		// --- Other features (cross-cutting; host varies) --------------------
		// Threat Intel answers on the chronicle host (SIEM plane), Content Hub /
		// integrations on the siemplify host (SOAR-modern plane).
		siemFamily("threat_intel", "threat_intel", AreaOther, LaneOperational, StatusValidated, "chronicle/ti.go"),
		soarModern("content_hub", AreaOther, LaneImperative, StatusValidated, "soar/marketplace.go"),
		soarModern("integrations", AreaOther, LaneImperative, StatusValidated, "soar/integrations.go"),

		// --- SOAR: cases (one function, several paths) ----------------------
		soarModern("cases", AreaSOAR, LaneOperational, StatusValidated, "soar/cases.go"),
		soarLegacy("case-verbs", LaneImperative, StatusValidated, "soar/legacy/cases.go"),
		soarLegacy("bulk-close", LaneImperative, StatusBuilt, "soar/legacy/cases_bulk.go"),
		// The alternate, unused Chronicle-host cases path: Area=SOAR (the cases
		// function) but Plane=SIEM (it answers on the chronicle host). 500s at
		// every version, so it is blocked — the working path is "cases" above.
		{
			Name: "cases (chronicle alt)", Area: AreaSOAR, Plane: PlaneSIEM,
			Host: HostChronicle, Auth: AuthADC, Generation: GenNew,
			APIVersion: chronicle.APIVersionFor("cases_chronicle_alt"),
			Lane:       LaneOperational, Status: StatusBlocked, SDKLocation: "chronicle/case.go",
		},

		// --- SOAR: other operational / imperative / raw ---------------------
		soarModern("grouping", AreaSOAR, LaneRaw, StatusBuilt, "soar/grouping.go"),
		soarLegacy("settings", LaneImperative, StatusBuilt, "soar/legacy/settings.go"),
		soarLegacy("legacy-call", LaneRaw, StatusValidated, "soar/legacy"),
	}

	// --- SOAR reconcile-lane config surfaces (siemplify/AppKey, external) ---
	// One entry per engine-backed SOAR surface (names mirror soarSurfaceDefs).
	soarReconcile := []struct {
		name   string
		status FamilyStatus
	}{
		{"webhooks", StatusBuilt},
		{"environments", StatusReadOnly},
		{"networks", StatusValidated},
		{"tracking-lists", StatusValidated},
		{"soc-roles", StatusValidated},
		{"idp", StatusValidated},
		{"visual-families", StatusValidated},
		{"sla-definitions", StatusValidated},
		{"case-stages", StatusValidated},
		{"case-tags", StatusBuilt},
		{"close-root-causes", StatusValidated},
		{"blacklists", StatusValidated},
		{"playbook-categories", StatusValidated},
		{"playbooks", StatusValidated},
		{"connectors", StatusValidated},
		{"jobs", StatusValidated},
	}
	fams := make([]SurfaceFamily, 0, len(explicit)+len(soarReconcile))
	fams = append(fams, explicit...)
	for _, s := range soarReconcile {
		fams = append(fams, soarLegacy(s.name, LaneReconcile, s.status, "soar/legacy"))
	}
	return fams
}

// SurfaceFamilyByName returns the registry entries with the given Name (a family
// can appear under more than one generation/path, e.g. cases).
func SurfaceFamilyByName(name string) []SurfaceFamily {
	var out []SurfaceFamily
	for _, f := range SurfaceFamilies {
		if f.Name == name {
			out = append(out, f)
		}
	}
	return out
}
