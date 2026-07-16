package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Chart-type ergonomics: generate a native-dashboard `visualization` object from a
// chart type + encode variables, instead of making the operator hand-author the
// echarts-style JSON (where a missing or mistyped encode var renders a silent
// blank chart). The encode variables must match the query's declared `match`/
// `outcome` aggregation variables — validated up front, so a typo fails clean.
//
// The shapes follow the documented native-dashboard chart model
// (series[].seriesType + encode, xAxes/yAxes, groupingType; pie uses
// encode.itemName/value). Marked for live-validation.

// chartVarRE matches a YARA-L aggregation variable token ("$lt", "$count").
var chartVarRE = regexp.MustCompile(`\$([A-Za-z_]\w*)`)

// queryVars returns the distinct aggregation variable names (without the `$`)
// declared/used in a stats query's match:/outcome:/order: projections — the names
// an encode mapping may reference.
func queryVars(query string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range chartVarRE.FindAllStringSubmatch(query, -1) {
		if name := m[1]; !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// reservedYaralKeywords are YARA-L 2.0 reserved words that cannot be used as an
// identifier (variable name) — case-insensitive. A `match:`/`outcome:` variable
// named with one of these compiles at author time but 400s at execute time with an
// opaque "no viable alternative" parser error, so the chart renders blank. Sourced
// from the YARA-L keyword reference (query definition, sections, modifiers,
// operators, and size specifiers).
var reservedYaralKeywords = map[string]bool{
	"rule": true, "private": true, "global": true,
	"meta": true, "strings": true, "condition": true, "events": true,
	"match": true, "outcome": true, "options": true, "dedup": true,
	"order": true, "limit": true, "select": true, "unselect": true,
	"and": true, "or": true, "not": true, "all": true, "any": true,
	"at": true, "contains": true, "startswith": true, "endswith": true,
	"icontains": true, "istartswith": true, "iendswith": true, "iequals": true,
	"matches": true, "in": true, "over": true, "nocase": true, "ascii": true,
	"wide": true, "fullword": true, "xor": true, "base64": true, "base64wide": true,
	"filesize": true, "entrypoint": true,
	"int8": true, "uint8": true, "int16": true, "uint16": true,
	"int32": true, "uint32": true, "int8be": true, "uint8be": true,
	"int16be": true, "uint16be": true, "int32be": true, "uint32be": true,
	// The dashboard query engine additionally reserves the singular/plural
	// family of its result nouns — both forms fail at render time.
	"rules": true, "event": true, "entity": true, "entities": true,
	"detection": true, "detections": true, "alert": true, "alerts": true,
}

// reservedQueryVars returns the distinct `$variable` names declared in a query that
// collide with a reserved YARA-L keyword (case-insensitive) — the names that will
// fail at execute time. Empty when the query is clean.
func reservedQueryVars(query string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range queryVars(query) {
		if reservedYaralKeywords[strings.ToLower(v)] && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// validateEncodeVars asserts every non-empty encode var maps to a column the query
// produces, so a chart can't be authored with an encode pointing at a non-existent
// column (the blank-chart failure mode). A stats query's columns are its `outcome`
// `$variables` AND its `match` field references (e.g. `match: target.hostname`), so
// an encode var is accepted if it is a declared `$var` OR appears as a field token
// in the query — only a genuine typo is rejected.
func validateEncodeVars(query string, vars ...string) error {
	if len(queryVars(query)) == 0 {
		return fmt.Errorf("--chart-type needs an aggregation query: the query declares no outcome $variables (a match:/outcome: projection) to map to chart axes")
	}
	for _, v := range vars {
		v = strings.TrimPrefix(strings.TrimSpace(v), "$")
		if v == "" {
			continue
		}
		if !tokenInQuery(query, v) {
			return fmt.Errorf("encode variable %q is not a column the query produces (an outcome $var or a match field) — check for a typo", v)
		}
	}
	return nil
}

// tokenInQuery reports whether v occurs in the query as a whole token — either a
// `$v` alias or a bare field token like `target.hostname` (so a dotted field path
// matches as one unit). Used to validate an encode mapping against the columns a
// stats query produces without parsing the full grammar.
func tokenInQuery(query, v string) bool {
	if strings.Contains(query, "$"+v) {
		return true
	}
	// A token boundary that does NOT split a dotted field path: the char before v
	// must not be a word char, `.`, or `$`, and the char after must not be a word
	// char or `.`.
	inWord := func(c byte, before bool) bool {
		return c == '_' || c == '.' || (before && c == '$') ||
			('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
	}
	for i := 0; ; i++ {
		j := strings.Index(query[i:], v)
		if j < 0 {
			return false
		}
		i += j
		if (i == 0 || !inWord(query[i-1], true)) &&
			(i+len(v) == len(query) || !inWord(query[i+len(v)], false)) {
			return true
		}
	}
}

// chartTypeIsTable reports whether a --chart-type denotes a table (which carries
// no visualization / encode mapping).
func chartTypeIsTable(chartType string) bool {
	return strings.EqualFold(strings.TrimSpace(chartType), "table")
}

// buildVisualization generates the visualization JSON for a chart type. x/y are
// the encode variable names (without `$`); seriesBy, when set, splits a bar/line
// into stacked series. "table" returns nil (no visualization → the server renders
// a table). The variables are NOT re-validated here (the caller validates against
// the query first).
func buildVisualization(chartType, x, y, seriesBy string) (json.RawMessage, error) {
	x = strings.TrimPrefix(strings.TrimSpace(x), "$")
	y = strings.TrimPrefix(strings.TrimSpace(y), "$")
	seriesBy = strings.TrimPrefix(strings.TrimSpace(seriesBy), "$")

	switch strings.ToLower(strings.TrimSpace(chartType)) {
	case "table":
		return nil, nil
	case "pie":
		if x == "" || y == "" {
			return nil, fmt.Errorf("a pie chart needs --x (category) and --y (value)")
		}
		return marshalViz(map[string]any{
			"series": []any{map[string]any{
				"seriesType": "PIE",
				"encode":     map[string]any{"itemName": x, "value": y},
			}},
		})
	case "bar", "line", "area":
		if x == "" || y == "" {
			return nil, fmt.Errorf("a %s chart needs --x (category) and --y (value)", chartType)
		}
		seriesType := strings.ToUpper(chartType)
		if seriesType == "AREA" {
			seriesType = "LINE"
		}
		viz := map[string]any{
			"xAxes": []any{map[string]any{"axisType": "CATEGORY", "displayName": x}},
			"yAxes": []any{map[string]any{"axisType": "VALUE", "displayName": y}},
		}
		if seriesBy != "" {
			viz["seriesColumn"] = []string{seriesBy}
			viz["groupingType"] = "Stacked"
			viz["legends"] = []any{map[string]any{"legendOrient": "HORIZONTAL", "top": 12}}
			viz["series"] = []any{map[string]any{
				"encode":     map[string]any{"x": x, "y": y},
				"seriesType": seriesType,
				"dataLabel":  map[string]any{},
				"stack":      "stack",
			}}
		} else {
			viz["groupingType"] = "Off"
			viz["series"] = []any{map[string]any{
				"encode":     map[string]any{"x": x, "y": y},
				"seriesType": seriesType,
				"dataLabel":  map[string]any{},
			}}
		}
		return marshalViz(viz)
	case "scatter", "scatterplot":
		if x == "" || y == "" {
			return nil, fmt.Errorf("a scatter chart needs --x and --y")
		}
		return marshalViz(map[string]any{
			"series": []any{map[string]any{
				"seriesType": "SCATTER",
				"encode":     map[string]any{"x": x, "y": y},
			}},
			"xAxes": []any{map[string]any{"axisType": "VALUE", "displayName": x}},
			"yAxes": []any{map[string]any{"axisType": "VALUE", "displayName": y}},
		})
	case "gauge":
		if y == "" {
			return nil, fmt.Errorf("a gauge chart needs --y (the value field)")
		}
		return marshalViz(map[string]any{
			"series": []any{map[string]any{
				"seriesType": "GAUGE",
				"encode":     map[string]any{"value": y},
			}},
		})
	case "metrics", "metric":
		if y == "" {
			return nil, fmt.Errorf("a metrics chart needs --y (the data field)")
		}
		return marshalViz(map[string]any{
			"series": []any{map[string]any{
				"seriesType": "METRICS",
				"encode":     map[string]any{"value": y},
			}},
		})
	case "map":
		if x == "" || y == "" {
			return nil, fmt.Errorf("a map chart needs --x (latitude) and --y (longitude); use --series-by for the count field")
		}
		encode := map[string]any{"lat": x, "lng": y}
		if seriesBy != "" {
			encode["value"] = seriesBy
		}
		return marshalViz(map[string]any{
			"series": []any{map[string]any{
				"seriesType": "MAP",
				"encode":     encode,
			}},
		})
	default:
		return nil, fmt.Errorf("invalid --chart-type %q (want area | bar | gauge | line | map | metrics | pie | scatter | table)", chartType)
	}
}

func marshalViz(v map[string]any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}
