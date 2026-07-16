package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func newEntitiesAuditCmd() *cobra.Command {
	var minRisk, limit int
	cmd := &cobra.Command{
		Use:   "audit [--min-risk N] [--limit N]",
		Short: "Read-only: cross-reference entity risk scores with watchlist membership",
		Long: "Audit detection posture: watchlist inventory + health (empty lists,\n" +
			"default multiplying factors), risk-score distribution, and coverage\n" +
			"gaps — entities at or above the risk threshold that appear on no\n" +
			"watchlist (membership via watchlists/{id}:listEntities). When the\n" +
			"instance does not serve entity listing, every high-risk entity is\n" +
			"reported unchecked and the result says so.\n\n" +
			"All reads are API-only — no mutations.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			return runEntitiesAudit(c, minRisk, limit)
		},
	}
	cmd.Flags().IntVar(&minRisk, "min-risk", 500, "minimum risk score to flag as high-risk (0-1000)")
	cmd.Flags().IntVar(&limit, "limit", 200, "max risk-score rows to fetch")
	return markJSON(cmd)
}

type auditResult struct {
	Watchlists    watchlistSummary `json:"watchlists"`
	RiskScores    riskSummary      `json:"riskScores"`
	CoverageGaps  []coverageGap    `json:"coverageGaps,omitempty"`
	GapCount      int              `json:"gapCount"`
	TotalAudited  int              `json:"totalAudited"`
	HighRiskCount int              `json:"highRiskCount"`
	// CrossReferenced reports whether CoverageGaps was filtered by real
	// watchlist membership; when false, CrossRefNote says why and every
	// high-risk entity is listed unchecked.
	CrossReferenced bool   `json:"crossReferenced"`
	CrossRefNote    string `json:"crossRefNote,omitempty"`
}

type watchlistSummary struct {
	Count         int              `json:"count"`
	Empty         []string         `json:"empty,omitempty"`
	DefaultFactor []string         `json:"defaultFactor,omitempty"`
	Details       []watchlistBrief `json:"details,omitempty"`
}

type watchlistBrief struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"displayName"`
	Factor      float64 `json:"multiplyingFactor"`
	EntityCount int     `json:"entityCount,omitempty"`
}

type riskSummary struct {
	Fetched  int `json:"fetched"`
	MinScore int `json:"minScore"`
	MaxScore int `json:"maxScore"`
	Users    int `json:"users"`
	Assets   int `json:"assets"`
}

type coverageGap struct {
	Entity     string `json:"entity"`
	EntityType string `json:"entityType"`
	RiskScore  int    `json:"riskScore"`
	Detections int    `json:"detections"`
}

func runEntitiesAudit(c *chronicle.Client, minRisk, limit int) error {
	ctx := baseContext()

	printProgress("watchlists", 0, 0)
	wls, err := c.ListWatchlists(ctx, 0)
	if err != nil {
		clearProgress()
		return fmt.Errorf("list watchlists: %w", err)
	}

	printProgress("watchlist entities", 0, 0)
	members, memberErr := watchlistMembership(ctx, c, wls)

	printProgress("risk scores", 0, 0)
	scores, err := c.QueryEntityRiskScores(ctx, "", "riskScore desc", limit)
	if err != nil {
		clearProgress()
		return fmt.Errorf("query risk scores: %w", err)
	}
	clearProgress()

	result := buildAuditResult(wls, scores, minRisk, members)
	result.CrossReferenced = memberErr == nil
	if memberErr != nil {
		result.CrossRefNote = "watchlist membership unavailable (" + memberErr.Error() + ") — high-risk entities listed unchecked"
	}

	if jsonOut {
		return emitJSON(result)
	}
	printAuditReport(result)
	return nil
}

func buildAuditResult(wls []chronicle.Watchlist, scores []chronicle.EntityRiskScore, minRisk int, members map[string]bool) auditResult {
	var r auditResult
	r.TotalAudited = len(scores)

	r.Watchlists.Count = len(wls)
	for _, w := range wls {
		brief := watchlistBrief{
			ID:          w.WatchlistID(),
			DisplayName: w.DisplayName,
			Factor:      w.MultiplyingFactor,
		}
		ec := parseEntityCount(w.EntityCount)
		brief.EntityCount = ec
		if ec == 0 {
			r.Watchlists.Empty = append(r.Watchlists.Empty, w.DisplayName)
		}
		if w.MultiplyingFactor == 1.0 || w.MultiplyingFactor == 0 {
			r.Watchlists.DefaultFactor = append(r.Watchlists.DefaultFactor, w.DisplayName)
		}
		r.Watchlists.Details = append(r.Watchlists.Details, brief)
	}

	if len(scores) > 0 {
		r.RiskScores.Fetched = len(scores)
		r.RiskScores.MinScore = scores[len(scores)-1].RiskScore
		r.RiskScores.MaxScore = scores[0].RiskScore
		for _, s := range scores {
			et := entityTypeFromScore(s)
			if et == "USER" {
				r.RiskScores.Users++
			} else {
				r.RiskScores.Assets++
			}
		}
	}

	for _, s := range scores {
		if s.RiskScore < minRisk {
			continue
		}
		r.HighRiskCount++
		if members != nil && members[strings.ToLower(entityIndicatorLabel(s))] {
			continue // already on a watchlist — covered, not a gap
		}
		r.CoverageGaps = append(r.CoverageGaps, coverageGap{
			Entity:     entityIndicatorLabel(s),
			EntityType: entityTypeFromScore(s),
			RiskScore:  s.RiskScore,
			Detections: s.DetectionsCount,
		})
	}
	r.GapCount = len(r.CoverageGaps)

	sort.Slice(r.CoverageGaps, func(i, j int) bool {
		return r.CoverageGaps[i].RiskScore > r.CoverageGaps[j].RiskScore
	})

	return r
}

func parseEntityCount(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var ec struct {
		Asset int `json:"asset"`
		User  int `json:"user"`
	}
	if json.Unmarshal(raw, &ec) != nil {
		return 0
	}
	return ec.Asset + ec.User
}

func entityTypeFromScore(s chronicle.EntityRiskScore) string {
	if t := s.Entity.Metadata.EntityType; t != "" {
		return t
	}
	return "UNKNOWN"
}

func entityIndicatorLabel(s chronicle.EntityRiskScore) string {
	for _, v := range s.EntityIndicator {
		if v != "" {
			return v
		}
	}
	return s.EntityID
}

// watchlistMembership fetches every watchlist's entities and collects their
// indicator strings (normalized lowercase) so risk-scored entities can be
// checked for membership. The first listEntities failure aborts the
// cross-reference and is returned — the audit then reports high-risk entities
// unchecked instead of failing outright.
func watchlistMembership(ctx context.Context, c *chronicle.Client, wls []chronicle.Watchlist) (map[string]bool, error) {
	members := make(map[string]bool)
	for _, w := range wls {
		ents, err := c.ListWatchlistEntities(ctx, w.WatchlistID(), 0)
		if err != nil {
			return nil, fmt.Errorf("watchlist %s: %w", w.DisplayName, err)
		}
		for _, e := range ents {
			for _, ind := range entityIndicatorStrings(e) {
				members[strings.ToLower(ind)] = true
			}
		}
	}
	return members, nil
}

// entityIndicatorStrings collects the scalar strings under a watchlist
// entity's "entity" subtree — the indicator fields (userid, hostname, ip,
// email, …) a risk-score row's entityIndicator may carry — without
// enumerating every UDM noun field.
func entityIndicatorStrings(raw json.RawMessage) []string {
	var obj struct {
		Entity json.RawMessage `json:"entity"`
	}
	if json.Unmarshal(raw, &obj) != nil || len(obj.Entity) == 0 {
		return nil
	}
	var node any
	if json.Unmarshal(obj.Entity, &node) != nil {
		return nil
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			if t != "" {
				out = append(out, t)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(node)
	return out
}

func printAuditReport(r auditResult) {
	fmt.Println("=== Entity Risk / Watchlist Audit ===")
	fmt.Println()

	fmt.Printf("Watchlists: %d defined\n", r.Watchlists.Count)
	if r.Watchlists.Count == 0 {
		fmt.Println("  ⚠ No watchlists configured — consider creating watchlists for high-value entities.")
	} else {
		for _, d := range r.Watchlists.Details {
			entities := ""
			if d.EntityCount > 0 {
				entities = fmt.Sprintf(", %d entities", d.EntityCount)
			}
			fmt.Printf("  %-30s factor=%.1f%s\n", d.DisplayName, d.Factor, entities)
		}
		if len(r.Watchlists.Empty) > 0 {
			fmt.Printf("  ⚠ Empty watchlists (%d): %s\n", len(r.Watchlists.Empty), strings.Join(r.Watchlists.Empty, ", "))
		}
		if len(r.Watchlists.DefaultFactor) > 0 {
			fmt.Printf("  ⚠ Default factor (1.0): %s\n", strings.Join(r.Watchlists.DefaultFactor, ", "))
		}
	}

	fmt.Println()
	fmt.Printf("Risk scores: %d entities fetched (score range %d–%d, %d users / %d assets)\n",
		r.RiskScores.Fetched, r.RiskScores.MinScore, r.RiskScores.MaxScore,
		r.RiskScores.Users, r.RiskScores.Assets)

	fmt.Println()
	if r.CrossRefNote != "" {
		fmt.Printf("Note: %s\n", r.CrossRefNote)
	}
	if r.GapCount == 0 {
		if r.CrossReferenced && r.HighRiskCount > 0 {
			fmt.Printf("Coverage: all %d high-risk entities are already on a watchlist.\n", r.HighRiskCount)
		} else {
			fmt.Println("Coverage: no high-risk entities found above the threshold.")
		}
	} else {
		if r.CrossReferenced {
			fmt.Printf("Coverage gaps — high-risk entities on no watchlist: %d (of %d high-risk)\n", r.GapCount, r.HighRiskCount)
		} else {
			fmt.Printf("High-risk entities above threshold (membership unchecked): %d\n", r.GapCount)
		}
		for _, g := range r.CoverageGaps {
			fmt.Printf("  %-6s %-50s score=%d detections=%d\n",
				g.EntityType, g.Entity, g.RiskScore, g.Detections)
		}
		fmt.Println()
		fmt.Println("Tip: add high-risk entities to a watchlist with `secopsctl lists watchlists add-entity`")
	}

	fmt.Fprintf(os.Stderr, "\n%d entities audited.\n", r.TotalAudited)
}
