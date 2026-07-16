package cli

import (
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
		Short: "Read-only: watchlist health + high-risk entities by risk score",
		Long: "Audit detection posture: watchlist inventory + health (empty lists,\n" +
			"default multiplying factors) and the entities at or above the risk\n" +
			"threshold, with the risk-score distribution. Per-entity watchlist\n" +
			"membership is not cross-referenced; treat the high-risk list as\n" +
			"candidates to verify against watchlists.\n\n" +
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

	printProgress("risk scores", 0, 0)
	scores, err := c.QueryEntityRiskScores(ctx, "", "riskScore desc", limit)
	if err != nil {
		clearProgress()
		return fmt.Errorf("query risk scores: %w", err)
	}
	clearProgress()

	result := buildAuditResult(wls, scores, minRisk)

	if jsonOut {
		return emitJSON(result)
	}
	printAuditReport(result)
	return nil
}

func buildAuditResult(wls []chronicle.Watchlist, scores []chronicle.EntityRiskScore, minRisk int) auditResult {
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
	if r.GapCount == 0 {
		fmt.Println("Coverage: no high-risk entities found above the threshold.")
	} else {
		fmt.Printf("High-risk entities above threshold: %d\n", r.GapCount)
		for _, g := range r.CoverageGaps {
			fmt.Printf("  %-6s %-50s score=%d detections=%d\n",
				g.EntityType, g.Entity, g.RiskScore, g.Detections)
		}
		fmt.Println()
		fmt.Println("Tip: add high-risk entities to a watchlist with `secopsctl lists watchlists add-entity`")
	}

	fmt.Fprintf(os.Stderr, "\n%d entities audited.\n", r.TotalAudited)
}
