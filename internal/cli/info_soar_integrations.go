package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

type soarIntegrationHealthRow struct {
	Key                       string   `json:"key"`
	DisplayName               string   `json:"display_name,omitempty"`
	Installed                 bool     `json:"installed"`
	Custom                    bool     `json:"custom,omitempty"`
	ConnectorInstances        int      `json:"connector_instances"`
	EnabledConnectorInstances int      `json:"enabled_connector_instances"`
	JobInstances              int      `json:"job_instances"`
	EnabledJobInstances       int      `json:"enabled_job_instances"`
	UnconfiguredRuntime       int      `json:"unconfigured_runtime,omitempty"`
	Environments              []string `json:"environments,omitempty"`
	Issues                    []string `json:"issues,omitempty"`
}

type integrationRuntimeRef struct {
	Kind          string
	Integration   string
	DisplayName   string
	Environment   string
	Enabled       bool
	HasEnabled    bool
	Configured    bool
	HasConfigured bool
}

func newInfoSOARIntegrationsCmd() *cobra.Command {
	return markJSON(&cobra.Command{
		Use:   "soar-integrations",
		Short: "Report SOAR integration runtime coverage (read-only)",
		Long: "Report installed SOAR integration packs and whether they have configured\n" +
			"connector/job runtime bound to environments. Read-only: uses the SOAR\n" +
			"v1alpha integration catalog plus legacy connector/job runtime cards.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := baseContext()
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}

			installed, err := c.ListIntegrations(ctx)
			if err != nil {
				return err
			}
			connectors, err := lc.ListConnectorCards(ctx)
			if err != nil {
				return fmt.Errorf("list connector runtime cards: %w", err)
			}
			jobs, err := lc.ListInstalledJobs(ctx)
			if err != nil {
				return fmt.Errorf("list job runtime cards: %w", err)
			}

			rows, err := buildSOARIntegrationHealth(installed, connectors, jobs)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(struct {
					Integrations []soarIntegrationHealthRow `json:"integrations"`
				}{Integrations: rows})
			}
			emitSOARIntegrationHealth(os.Stdout, rows)
			return nil
		},
	})
}

func buildSOARIntegrationHealth(installed []soar.Integration, connectorsRaw, jobsRaw json.RawMessage) ([]soarIntegrationHealthRow, error) {
	rows := make(map[string]*soarIntegrationHealthRow)
	aliases := make(map[string]string)

	rowFor := func(key string) *soarIntegrationHealthRow {
		key = strings.TrimSpace(key)
		if key == "" {
			key = "(unknown)"
		}
		norm := integrationAliasKey(key)
		if canonical, ok := aliases[norm]; ok {
			return rows[canonical]
		}
		if r, ok := rows[key]; ok {
			return r
		}
		rows[key] = &soarIntegrationHealthRow{Key: key}
		aliases[norm] = key
		return rows[key]
	}
	addAlias := func(canonical, alias string) {
		alias = strings.TrimSpace(alias)
		if canonical == "" || alias == "" {
			return
		}
		aliases[integrationAliasKey(alias)] = canonical
		if base := baseIntegrationAlias(alias); base != "" {
			aliases[integrationAliasKey(base)] = canonical
		}
	}

	for _, i := range installed {
		key := firstInfoNonEmpty(i.Identifier, i.Name, i.ProdIdentifier, i.DisplayName)
		if key == "" {
			continue
		}
		r := rowFor(key)
		r.Installed = true
		r.Custom = i.Custom
		r.DisplayName = firstInfoNonEmpty(i.DisplayName, r.DisplayName, key)
		for _, alias := range []string{i.Identifier, i.Name, i.ProdIdentifier, i.DisplayName} {
			addAlias(r.Key, alias)
		}
	}

	runtimeRefs, err := collectConnectorRuntimeRefs(connectorsRaw)
	if err != nil {
		return nil, fmt.Errorf("decode connector runtime cards: %w", err)
	}
	jobRefs, err := collectJobRuntimeRefs(jobsRaw)
	if err != nil {
		return nil, fmt.Errorf("decode job runtime cards: %w", err)
	}
	runtimeRefs = append(runtimeRefs, jobRefs...)

	envs := make(map[string]map[string]struct{})
	enabledKnown := make(map[string]int)
	for _, ref := range runtimeRefs {
		r := rowFor(ref.Integration)
		if r.DisplayName == "" {
			r.DisplayName = ref.DisplayName
		}
		if ref.DisplayName != "" {
			addAlias(r.Key, ref.DisplayName)
		}
		switch ref.Kind {
		case "connector":
			r.ConnectorInstances++
			if ref.HasEnabled && ref.Enabled {
				r.EnabledConnectorInstances++
			}
		case "job":
			r.JobInstances++
			if ref.HasEnabled && ref.Enabled {
				r.EnabledJobInstances++
			}
		}
		if ref.HasEnabled {
			enabledKnown[r.Key]++
		}
		if ref.HasConfigured && !ref.Configured {
			r.UnconfiguredRuntime++
		}
		if ref.Environment != "" {
			if envs[r.Key] == nil {
				envs[r.Key] = make(map[string]struct{})
			}
			envs[r.Key][ref.Environment] = struct{}{}
		}
	}

	out := make([]soarIntegrationHealthRow, 0, len(rows))
	for key, r := range rows {
		if set := envs[key]; len(set) > 0 {
			r.Environments = sortedSet(set)
		}
		totalRuntime := r.ConnectorInstances + r.JobInstances
		totalEnabled := r.EnabledConnectorInstances + r.EnabledJobInstances
		switch {
		case r.Installed && totalRuntime == 0:
			r.Issues = append(r.Issues, "config_without_runtime")
		case !r.Installed && totalRuntime > 0:
			r.Issues = append(r.Issues, "runtime_without_installed_pack")
		}
		if totalRuntime > 0 && enabledKnown[key] == totalRuntime && totalEnabled == 0 {
			r.Issues = append(r.Issues, "runtime_disabled")
		}
		if r.UnconfiguredRuntime > 0 {
			r.Issues = append(r.Issues, "unconfigured_runtime")
		}
		out = append(out, *r)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Installed != out[j].Installed {
			return out[i].Installed
		}
		return strings.ToLower(out[i].Key) < strings.ToLower(out[j].Key)
	})
	return out, nil
}

func emitSOARIntegrationHealth(w io.Writer, rows []soarIntegrationHealthRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no SOAR integrations found.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "INTEGRATION\tINSTALLED\tRUNTIME\tENABLED\tENVIRONMENTS\tISSUES")
	for _, r := range rows {
		runtime := fmt.Sprintf("conn=%d job=%d", r.ConnectorInstances, r.JobInstances)
		enabled := fmt.Sprintf("conn=%d job=%d", r.EnabledConnectorInstances, r.EnabledJobInstances)
		issues := "-"
		if len(r.Issues) > 0 {
			issues = strings.Join(r.Issues, ",")
		}
		fmt.Fprintf(tw, "%s\t%t\t%s\t%s\t%s\t%s\n",
			truncate(firstInfoNonEmpty(r.DisplayName, r.Key), 42),
			r.Installed,
			runtime,
			enabled,
			dashIfEmpty(strings.Join(r.Environments, ",")),
			issues,
		)
	}
	_ = tw.Flush()
}

func collectConnectorRuntimeRefs(raw json.RawMessage) ([]integrationRuntimeRef, error) {
	items, err := decodeInfoRawObjects(raw)
	if err != nil {
		return nil, err
	}
	var refs []integrationRuntimeRef
	for _, item := range items {
		if cards, ok := rawArrayField(item, "cards"); ok {
			for _, card := range cards {
				cm, ok := card.(map[string]any)
				if !ok {
					continue
				}
				refs = append(refs, runtimeRefFromObject("connector", cm, item))
			}
			continue
		}
		refs = append(refs, runtimeRefFromObject("connector", item, nil))
	}
	return refs, nil
}

func collectJobRuntimeRefs(raw json.RawMessage) ([]integrationRuntimeRef, error) {
	items, err := decodeInfoRawObjects(raw)
	if err != nil {
		return nil, err
	}
	refs := make([]integrationRuntimeRef, 0, len(items))
	for _, item := range items {
		refs = append(refs, runtimeRefFromObject("job", item, nil))
	}
	return refs, nil
}

func runtimeRefFromObject(kind string, obj, fallback map[string]any) integrationRuntimeRef {
	key, display := integrationIdentity(obj)
	if key == "" && fallback != nil {
		key, display = integrationIdentity(fallback)
	}
	if key == "" {
		key = firstString(obj, "integrationIdentifier", "integrationName", "integrationId", "integration")
	}
	if display == "" {
		display = key
	}
	enabled, hasEnabled := firstBool(obj, "isEnabled", "enabled", "isActive", "active")
	configured, hasConfigured := firstBool(obj, "isConfigured", "configured")
	return integrationRuntimeRef{
		Kind:          kind,
		Integration:   key,
		DisplayName:   display,
		Environment:   environmentName(obj),
		Enabled:       enabled,
		HasEnabled:    hasEnabled,
		Configured:    configured,
		HasConfigured: hasConfigured,
	}
}

func integrationIdentity(m map[string]any) (key, display string) {
	if nested, ok := rawMapField(m, "integration"); ok {
		key = firstString(nested, "identifier", "uniqueIdentifier", "name", "prodIdentifier", "integrationIdentifier", "displayName")
		display = firstString(nested, "displayName", "name", "identifier", "uniqueIdentifier", "prodIdentifier")
		if key != "" || display != "" {
			return key, display
		}
	}
	if s := firstString(m, "integration"); s != "" {
		return s, s
	}
	key = firstString(m, "integrationIdentifier", "integrationId", "integrationName", "integration_id")
	display = firstString(m, "integrationDisplayName", "integrationName", "integration_identifier", "integrationIdentifier")
	return key, display
}

func environmentName(m map[string]any) string {
	if nested, ok := rawMapField(m, "environment"); ok {
		if s := firstString(nested, "displayName", "name", "identifier", "id"); s != "" {
			return s
		}
	}
	return firstString(m, "environmentName", "environmentIdentifier", "environmentId", "environment")
}

func decodeInfoRawObjects(raw json.RawMessage) ([]map[string]any, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return rawObjectsFromValue(v), nil
}

func rawObjectsFromValue(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		return rawObjectsFromArray(x)
	case map[string]any:
		for _, key := range []string{
			"items", "data", "objects", "results", "integrations", "connectors",
			"connectorCards", "jobs", "jobInstances",
		} {
			if a, ok := rawArrayField(x, key); ok {
				return rawObjectsFromArray(a)
			}
		}
		return []map[string]any{x}
	default:
		return nil
	}
}

func rawObjectsFromArray(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func rawArrayField(m map[string]any, key string) ([]any, bool) {
	v, ok := rawLookup(m, key)
	if !ok {
		return nil, false
	}
	a, ok := v.([]any)
	return a, ok
}

func rawMapField(m map[string]any, key string) (map[string]any, bool) {
	v, ok := rawLookup(m, key)
	if !ok {
		return nil, false
	}
	mv, ok := v.(map[string]any)
	return mv, ok
}

func rawLookup(m map[string]any, key string) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := rawLookup(m, key)
		if !ok {
			continue
		}
		if s := stringValue(v); s != "" {
			return s
		}
	}
	return ""
}

func firstBool(m map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		v, ok := rawLookup(m, key)
		if !ok {
			continue
		}
		switch x := v.(type) {
		case bool:
			return x, true
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true", "yes", "enabled":
				return true, true
			case "false", "no", "disabled":
				return false, true
			}
		}
	}
	return false, false
}

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case int:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	default:
		return ""
	}
}

func firstInfoNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func integrationAliasKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func baseIntegrationAlias(s string) string {
	before, _, ok := strings.Cut(s, "__")
	if !ok {
		return ""
	}
	return strings.TrimSpace(before)
}
