package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"danny.vn/secops/chronicle"
)

// --- Curated rule-set deployment snapshot (pull_curated) --------------------

// curatedState is the root of the single deployments.yaml snapshot: a sorted
// list of categories, each carrying its sorted rule sets and their per-precision
// deployment state.
type curatedState struct {
	Categories []curatedCategory `yaml:"categories"`
}

type curatedCategory struct {
	DisplayName string           `yaml:"display_name"`
	ID          string           `yaml:"id"`
	RuleSets    []curatedRuleSet `yaml:"rule_sets"`
}

type curatedRuleSet struct {
	DisplayName string                       `yaml:"display_name"`
	ID          string                       `yaml:"id"`
	Description string                       `yaml:"description,omitempty"`
	Precisions  []string                     `yaml:"precisions,omitempty"`
	Severity    string                       `yaml:"severity,omitempty"`
	Deployments map[string]curatedDeployment `yaml:"deployments"`
}

type curatedDeployment struct {
	Enabled  bool `yaml:"enabled"`
	Alerting bool `yaml:"alerting"`
}

// PullCurated snapshots curated rule-set categories, their rule sets, and the
// per-precision deployment state into a single sorted curated/deployments.yaml
// for clean diffs. Categories and rule sets are sorted by display name (falling
// back to id); deployments are keyed by precision. Returns the number of
// categories written.
func PullCurated(ctx context.Context, c *chronicle.Client, outDir string) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}

	// 1) Categories and, per category, their rule sets.
	cats, err := c.ListCuratedRuleSetCategories(ctx)
	if err != nil {
		return 0, err
	}

	// Index rule sets by category id, then rule-set id, so deployments join O(1).
	type rsAcc struct {
		set curatedRuleSet
	}
	type catAcc struct {
		displayName string
		id          string
		ruleSets    map[string]*rsAcc
		order       []string // rule-set ids in arrival order (for stable pre-sort)
	}
	catIndex := make(map[string]*catAcc, len(cats))
	catOrder := make([]string, 0, len(cats))

	for _, cat := range cats {
		catID := segmentAfter(cat.Name, "curatedRuleSetCategories")
		acc := &catAcc{
			displayName: cat.DisplayName,
			id:          catID,
			ruleSets:    map[string]*rsAcc{},
		}
		catIndex[catID] = acc
		catOrder = append(catOrder, catID)

		sets, err := c.ListCuratedRuleSets(ctx, catID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  (warn) list curatedRuleSets for %s: %v\n", cat.DisplayName, err)
			continue
		}
		for _, rs := range sets {
			rsID := segmentAfter(rs.Name, "curatedRuleSets")
			acc.ruleSets[rsID] = &rsAcc{set: curatedRuleSet{
				DisplayName: rs.DisplayName,
				ID:          rsID,
				Description: rs.Description,
				Precisions:  rs.Precisions,
				Severity:    severityName(rs.Severity),
				Deployments: map[string]curatedDeployment{},
			}}
			acc.order = append(acc.order, rsID)
		}
	}

	// 2) All deployments in one batched call; join by parsing the resource Name.
	deps, err := c.ListCuratedRuleSetDeployments(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  (warn) list curated rule set deployments: %v\n", err)
	}
	for _, dep := range deps {
		catID := segmentAfter(dep.Name, "curatedRuleSetCategories")
		rsID := segmentAfter(dep.Name, "curatedRuleSets")
		precision := segmentAfter(dep.Name, "curatedRuleSetDeployments")
		if catID == "" || rsID == "" || precision == "" {
			continue
		}
		cat := catIndex[catID]
		if cat == nil {
			continue
		}
		rs := cat.ruleSets[rsID]
		if rs == nil {
			continue
		}
		rs.set.Deployments[precision] = curatedDeployment{
			Enabled:  dep.Enabled,
			Alerting: dep.Alerting,
		}
	}

	// 3) Materialize a sorted, list-shaped snapshot.
	state := curatedState{Categories: make([]curatedCategory, 0, len(catOrder))}

	sortedCatIDs := append([]string(nil), catOrder...)
	sort.SliceStable(sortedCatIDs, func(i, j int) bool {
		return sortKey(catIndex[sortedCatIDs[i]].displayName, sortedCatIDs[i]) <
			sortKey(catIndex[sortedCatIDs[j]].displayName, sortedCatIDs[j])
	})

	nSets, nEnabled := 0, 0
	for _, catID := range sortedCatIDs {
		cat := catIndex[catID]
		rsIDs := append([]string(nil), cat.order...)
		sort.SliceStable(rsIDs, func(i, j int) bool {
			return sortKey(cat.ruleSets[rsIDs[i]].set.DisplayName, rsIDs[i]) <
				sortKey(cat.ruleSets[rsIDs[j]].set.DisplayName, rsIDs[j])
		})
		ruleSets := make([]curatedRuleSet, 0, len(rsIDs))
		for _, rsID := range rsIDs {
			set := cat.ruleSets[rsID].set
			ruleSets = append(ruleSets, set)
			nSets++
			for _, d := range set.Deployments {
				if d.Enabled {
					nEnabled++
				}
			}
		}
		state.Categories = append(state.Categories, curatedCategory{
			DisplayName: cat.displayName,
			ID:          cat.id,
			RuleSets:    ruleSets,
		})
	}

	outFile := filepath.Join(outDir, "deployments.yaml")
	if err := writeYAML(outFile, state); err != nil {
		return 0, err
	}
	nCats := len(state.Categories)
	fmt.Printf("curated:      wrote %d categories, %d rule sets (%d deployments enabled) -> %s\n",
		nCats, nSets, nEnabled, outFile)
	return nCats, nil
}

// --- Featured / Content-Hub curated rules (pull_curated_rules) --------------

// curatedRuleRecord is the per-rule <slug>.yaml companion to the <slug>.yaral
// source. Empty/zero fields are dropped via omitempty so the snapshot stays
// terse and diff-friendly, mirroring the legacy record keys.
type curatedRuleRecord struct {
	DisplayName           string             `yaml:"display_name"`
	RuleID                string             `yaml:"rule_id,omitempty"`
	Name                  string             `yaml:"name,omitempty"`
	Category              string             `yaml:"category,omitempty"`
	CategoryID            string             `yaml:"category_id,omitempty"`
	RuleSet               curatedRuleSetLink `yaml:"rule_set,omitempty"`
	Severity              string             `yaml:"severity,omitempty"`
	Precision             string             `yaml:"precision,omitempty"`
	Type                  string             `yaml:"type,omitempty"`
	Author                string             `yaml:"author,omitempty"`
	Certified             bool               `yaml:"certified,omitempty"`
	Description           string             `yaml:"description,omitempty"`
	Tactics               []string           `yaml:"tactics,omitempty"`
	Techniques            []curatedTechnique `yaml:"techniques,omitempty"`
	LiveStatusEnabled     bool               `yaml:"live_status_enabled,omitempty"`
	AlertingStatusEnabled bool               `yaml:"alerting_status_enabled,omitempty"`
	RuleTextHidden        bool               `yaml:"rule_text_hidden,omitempty"`
	NonUpgradable         bool               `yaml:"non_upgradable,omitempty"`
	PrivateRule           bool               `yaml:"private_rule,omitempty"`
	CreateTime            string             `yaml:"create_time,omitempty"`
	UpdateTime            string             `yaml:"update_time,omitempty"`
}

// curatedRuleSetLink is the trimmed rule-set linkage embedded in a rule record.
type curatedRuleSetLink struct {
	DisplayName string `yaml:"display_name,omitempty"`
	ID          string `yaml:"id,omitempty"`
	Name        string `yaml:"name,omitempty"`
}

// curatedTechnique is a single MITRE ATT&CK technique reference; kept as id + name.
type curatedTechnique struct {
	ID   string `yaml:"id,omitempty"`
	Name string `yaml:"name,omitempty"`
}

// curatedIndexEntry is one row of the flat _index.yaml listing.
type curatedIndexEntry struct {
	DisplayName string   `yaml:"display_name"`
	Category    string   `yaml:"category"`
	RuleSet     string   `yaml:"rule_set"`
	Severity    string   `yaml:"severity,omitempty"`
	Precision   string   `yaml:"precision,omitempty"`
	Techniques  []string `yaml:"techniques,omitempty"`
	Live        bool     `yaml:"live"`
	Alerting    bool     `yaml:"alerting"`
	Hidden      bool     `yaml:"hidden"`
	File        string   `yaml:"file"`
}

// curatedIndex is the _index.yaml summary, bucketed by category.
type curatedIndex struct {
	Filter     string                          `yaml:"filter,omitempty"`
	TotalRules int                             `yaml:"total_rules"`
	HiddenText int                             `yaml:"hidden_text_rules"`
	Categories map[string]curatedCategoryStats `yaml:"categories"`
}

type curatedCategoryStats struct {
	Total           int      `yaml:"total"`
	LiveEnabled     int      `yaml:"live_enabled"`
	AlertingEnabled int      `yaml:"alerting_enabled"`
	HiddenText      int      `yaml:"hidden_text"`
	RuleSets        []string `yaml:"rule_sets"`
}

// contentMetadata is the slice of contentMetadata we read off each rule.
type contentMetadata struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Categories  []string `json:"categories"`
	Severity    string   `json:"severity"`
	Author      string   `json:"author"`
	Certified   bool     `json:"certified"`
	Description string   `json:"description"`
	CreateTime  string   `json:"createTime"`
	UpdateTime  string   `json:"updateTime"`
}

// curatedRuleContent is the slice of curatedRuleContent we read off each rule.
type curatedRuleContent struct {
	Precision  string             `json:"precision"`
	Tactics    []string           `json:"tactics"`
	Techniques []curatedTechnique `json:"techniques"`
}

// ruleSetLink is the slice of the embedded ruleSet object we read off each rule.
type ruleSetLink struct {
	DisplayName    string `json:"displayName"`
	ID             string `json:"id"`
	CuratedRuleSet string `json:"curatedRuleSet"`
}

// PullCuratedRules snapshots featured/Content-Hub curated rules with full
// YARA-L source plus per-rule metadata. Layout under outDir:
//
//	<category_slug>/<rule_set_slug>/<rule_slug>.yaral
//	<category_slug>/<rule_set_slug>/<rule_slug>.yaml
//	_index.yaml
//
// Hidden-source rules (RuleTextHidden) get a stub .yaral note rather than being
// skipped. Slug collisions within a rule set are disambiguated with a short id
// suffix. filter is passed straight to the API. Returns the number of rules.
func PullCuratedRules(ctx context.Context, c *chronicle.Client, outDir, filter string) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}

	rules, err := c.ListFeaturedContentRules(ctx, filter)
	if err != nil {
		return 0, err
	}

	indexEntries := make([]curatedIndexEntry, 0, len(rules))
	hiddenCount := 0
	written := 0

	for _, rule := range rules {
		meta := decodeContentMetadata(rule.ContentMetadata)
		content := decodeCuratedRuleContent(rule.CuratedRuleContent)
		rs := decodeRuleSetLink(rule.RuleSet)

		display := meta.DisplayName
		if display == "" {
			display = "unnamed"
		}
		ruleSlug := Slugify(display)

		ruleID := meta.ID
		if ruleID == "" {
			ruleID = segmentAfter(rule.Name, "featuredContentRules")
		}

		catDisplay := "Uncategorized"
		if len(meta.Categories) > 0 && meta.Categories[0] != "" {
			catDisplay = meta.Categories[0]
		} else if rule.CategoryID != "" {
			catDisplay = rule.CategoryID
		}
		catSlug := Slugify(catDisplay)

		rsDisplay := rs.DisplayName
		if rsDisplay == "" {
			rsDisplay = "Unassigned"
		}
		rsSlug := Slugify(rsDisplay)

		targetDir := filepath.Join(outDir, catSlug, rsSlug)
		if _, err := EnsureDir(targetDir); err != nil {
			return written, err
		}

		// Disambiguate slug collisions within a rule set with a short id suffix.
		if fileExists(filepath.Join(targetDir, ruleSlug+".yaral")) {
			short := strings.TrimPrefix(ruleID, "ur_")
			if len(short) > 8 {
				short = short[:8]
			}
			if short == "" {
				short = "x"
			}
			ruleSlug = ruleSlug + "__" + short
		}

		yaralPath := filepath.Join(targetDir, ruleSlug+".yaral")
		if rule.RuleTextHidden && rule.RuleText == "" {
			stub := fmt.Sprintf(
				"// YARA-L source hidden by vendor (ruleTextHidden=true)\n"+
					"// Rule ID: %s\n"+
					"// Display:  %s\n",
				ruleID, display)
			if err := os.WriteFile(yaralPath, []byte(stub), 0o644); err != nil {
				return written, err
			}
			hiddenCount++
		} else {
			if err := os.WriteFile(yaralPath, []byte(rule.RuleText), 0o644); err != nil {
				return written, err
			}
		}

		severity := severityName(rule.Severity)
		if severity == "" {
			severity = meta.Severity
		}

		record := curatedRuleRecord{
			DisplayName: display,
			RuleID:      ruleID,
			Name:        rule.Name,
			Category:    catDisplay,
			CategoryID:  rule.CategoryID,
			RuleSet: curatedRuleSetLink{
				DisplayName: rsDisplay,
				ID:          rs.ID,
				Name:        rs.CuratedRuleSet,
			},
			Severity:              severity,
			Precision:             content.Precision,
			Type:                  rule.Type,
			Author:                meta.Author,
			Certified:             meta.Certified,
			Description:           meta.Description,
			Tactics:               content.Tactics,
			Techniques:            content.Techniques,
			LiveStatusEnabled:     rule.LiveStatusEnabled,
			AlertingStatusEnabled: rule.AlertingStatusEnabled,
			RuleTextHidden:        rule.RuleTextHidden,
			NonUpgradable:         rule.NonUpgradable,
			PrivateRule:           rule.PrivateRule,
			CreateTime:            meta.CreateTime,
			UpdateTime:            meta.UpdateTime,
		}
		if err := writeYAML(filepath.Join(targetDir, ruleSlug+".yaml"), record); err != nil {
			return written, err
		}

		techIDs := make([]string, 0, len(content.Techniques))
		for _, t := range content.Techniques {
			techIDs = append(techIDs, t.ID)
		}
		indexEntries = append(indexEntries, curatedIndexEntry{
			DisplayName: display,
			Category:    catDisplay,
			RuleSet:     rsDisplay,
			Severity:    severity,
			Precision:   content.Precision,
			Techniques:  techIDs,
			Live:        rule.LiveStatusEnabled,
			Alerting:    rule.AlertingStatusEnabled,
			Hidden:      rule.RuleTextHidden,
			File:        catSlug + "/" + rsSlug + "/" + ruleSlug + ".yaral",
		})
		written++
	}

	// Index, bucketed by category.
	byCat := map[string][]curatedIndexEntry{}
	for _, e := range indexEntries {
		byCat[e.Category] = append(byCat[e.Category], e)
	}
	catSummary := make(map[string]curatedCategoryStats, len(byCat))
	for cat, entries := range byCat {
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].RuleSet != entries[j].RuleSet {
				return entries[i].RuleSet < entries[j].RuleSet
			}
			return entries[i].DisplayName < entries[j].DisplayName
		})
		stats := curatedCategoryStats{Total: len(entries)}
		setSeen := map[string]bool{}
		var sets []string
		for _, r := range entries {
			if r.Live {
				stats.LiveEnabled++
			}
			if r.Alerting {
				stats.AlertingEnabled++
			}
			if r.Hidden {
				stats.HiddenText++
			}
			if !setSeen[r.RuleSet] {
				setSeen[r.RuleSet] = true
				sets = append(sets, r.RuleSet)
			}
		}
		sort.Strings(sets)
		stats.RuleSets = sets
		catSummary[cat] = stats
	}

	index := curatedIndex{
		Filter:     filter,
		TotalRules: written,
		HiddenText: hiddenCount,
		Categories: catSummary,
	}
	if err := writeYAML(filepath.Join(outDir, "_index.yaml"), index); err != nil {
		return written, err
	}

	fmt.Printf("curated-rules: wrote %d rules (%d with hidden YARA-L) across %d categories -> %s/\n",
		written, hiddenCount, len(byCat), outDir)
	return written, nil
}

// --- helpers ----------------------------------------------------------------

// segmentAfter returns the path segment immediately following marker in a
// "/"-delimited resource name (e.g. segmentAfter(".../curatedRuleSets/x", "curatedRuleSets")
// is "x"). Returns "" if marker is absent or is the final segment.
func segmentAfter(name, marker string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		if p == marker && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// severityName extracts a severity's display name, tolerating a nil pointer.
func severityName(s *chronicle.Severity) string {
	if s == nil {
		return ""
	}
	return s.DisplayName
}

// sortKey returns display if non-empty, else fallback — the legacy sort key.
func sortKey(display, fallback string) string {
	if display != "" {
		return display
	}
	return fallback
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// decodeContentMetadata parses the freeform contentMetadata blob, tolerating an
// empty or malformed value.
func decodeContentMetadata(raw json.RawMessage) contentMetadata {
	var m contentMetadata
	if len(raw) == 0 {
		return m
	}
	_ = json.Unmarshal(raw, &m)
	return m
}

// decodeCuratedRuleContent parses the freeform curatedRuleContent blob.
func decodeCuratedRuleContent(raw json.RawMessage) curatedRuleContent {
	var c curatedRuleContent
	if len(raw) == 0 {
		return c
	}
	_ = json.Unmarshal(raw, &c)
	return c
}

// decodeRuleSetLink parses the embedded ruleSet linkage blob.
func decodeRuleSetLink(raw json.RawMessage) ruleSetLink {
	var r ruleSetLink
	if len(raw) == 0 {
		return r
	}
	_ = json.Unmarshal(raw, &r)
	return r
}
