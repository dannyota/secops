package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func init() { rootCmd.AddCommand(newAuditCmd()) }

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit commands: user activity summary",
	}
	cmd.AddCommand(newAuditUserCmd())
	return cmd
}

type activityCategory struct {
	Name  string
	Query func(email string) string
}

var userActivityCategories = []activityCategory{
	{"login", func(email string) string {
		return fmt.Sprintf(`metadata.event_type = "USER_LOGIN" AND principal.user.emailAddresses = "%s"`, email)
	}},
	{"admin", func(email string) string {
		return fmt.Sprintf(`(metadata.event_type = "USER_CHANGE_PERMISSIONS" OR metadata.event_type = "USER_CHANGE_PASSWORD" OR metadata.event_type = "USER_CREATION" OR metadata.event_type = "USER_DELETION" OR metadata.event_type = "GROUP_MODIFICATION") AND principal.user.emailAddresses = "%s"`, email)
	}},
	{"password", func(email string) string {
		return fmt.Sprintf(`metadata.event_type = "USER_CHANGE_PASSWORD" AND (principal.user.emailAddresses = "%s" OR target.user.emailAddresses = "%s")`, email, email)
	}},
	{"oauth", func(email string) string {
		return fmt.Sprintf(`metadata.event_type = "USER_RESOURCE_ACCESS" AND principal.user.emailAddresses = "%s" AND target.application != ""`, email)
	}},
	{"iam", func(email string) string {
		return fmt.Sprintf(`(metadata.event_type = "USER_CHANGE_PERMISSIONS" OR metadata.event_type = "RESOURCE_PERMISSIONS_CHANGE") AND principal.user.emailAddresses = "%s"`, email)
	}},
	{"resource", func(email string) string {
		return fmt.Sprintf(`(metadata.event_type = "RESOURCE_READ" OR metadata.event_type = "RESOURCE_WRITTEN" OR metadata.event_type = "RESOURCE_CREATION" OR metadata.event_type = "RESOURCE_DELETION") AND principal.user.emailAddresses = "%s"`, email)
	}},
}

type auditUserResult struct {
	Email       string                `json:"email"`
	From        string                `json:"from"`
	To          string                `json:"to"`
	Categories  []auditCategoryResult `json:"categories"`
	TotalEvents int                   `json:"totalEvents"`
}

type auditCategoryResult struct {
	Name   string            `json:"name"`
	Query  string            `json:"query"`
	Count  int               `json:"count"`
	Events []json.RawMessage `json:"events,omitempty"`
}

func newAuditUserCmd() *cobra.Command {
	var hours int
	var fromTS, toTS string
	var categories string
	var limit int
	var localFormat string
	cmd := &cobra.Command{
		Use:   "user <email>",
		Short: "Run standard activity queries for one user across categories (read-only)",
		Long: "Run 6 standard UDM activity queries for one user account and output results\n" +
			"grouped by category: login, admin, password, oauth, iam, resource.\n\n" +
			"Categories:\n" +
			"  login     USER_LOGIN events\n" +
			"  admin     permission/password/account changes, group modifications\n" +
			"  password  USER_CHANGE_PASSWORD (as principal or target)\n" +
			"  oauth     USER_RESOURCE_ACCESS with a target application\n" +
			"  iam       permission changes (USER_CHANGE_PERMISSIONS, RESOURCE_PERMISSIONS_CHANGE)\n" +
			"  resource  RESOURCE_READ/WRITTEN/CREATION/DELETION\n\n" +
			"Default: all categories. Use --categories to select a subset.\n" +
			"All queries are read-only. Auto-chunks windows >90 days.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			email := args[0]
			if !strings.Contains(email, "@") {
				return fmt.Errorf("expected an email address, got %q", email)
			}

			start, end, err := resolveWindow(hours, fromTS, toTS)
			if err != nil {
				return err
			}

			cats := filterCategories(categories)
			if len(cats) == 0 {
				return fmt.Errorf("no valid categories selected (available: login, admin, password, oauth, iam, resource)")
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}

			result, err := runAuditUser(c, email, start, end, cats, limit)
			if err != nil {
				return err
			}

			format := effectiveFormat(localFormat)
			return renderAuditUser(result, format)
		},
	}
	cmd.Flags().IntVar(&hours, "hours", 24, "look-back window in hours (used when --from is not set)")
	cmd.Flags().StringVar(&fromTS, "from", "", "start of window (RFC3339 / ISO-8601 / YYYY-MM-DD)")
	cmd.Flags().StringVar(&toTS, "to", "", "end of window (default: now)")
	cmd.Flags().StringVar(&categories, "categories", "", `comma-separated subset (default: all). Values: login,admin,password,oauth,iam,resource`)
	cmd.Flags().IntVar(&limit, "limit", 1000, "max events per category")
	cmd.Flags().StringVar(&localFormat, "format", "", "output format: table | json | jsonl | csv (default: table)")
	return markJSON(cmd)
}

func filterCategories(sel string) []activityCategory {
	if sel == "" {
		return userActivityCategories
	}
	wanted := map[string]bool{}
	for s := range strings.SplitSeq(sel, ",") {
		s = strings.TrimSpace(strings.ToLower(s))
		if s != "" {
			wanted[s] = true
		}
	}
	var out []activityCategory
	for _, cat := range userActivityCategories {
		if wanted[cat.Name] {
			out = append(out, cat)
		}
	}
	return out
}

func runAuditUser(c *chronicle.Client, email string, start, end time.Time, cats []activityCategory, limit int) (*auditUserResult, error) {
	ctx := baseContext()
	result := &auditUserResult{
		Email: email,
		From:  start.Format(time.RFC3339),
		To:    end.Format(time.RFC3339),
	}

	for _, cat := range cats {
		query := cat.Query(email)
		fmt.Fprintf(os.Stderr, "auditing %s activity…", cat.Name)

		events, err := c.SearchUDM(ctx, query, start, end, limit)
		if err != nil {
			clearProgress()
			return nil, fmt.Errorf("%s: %w", cat.Name, err)
		}
		fmt.Fprintf(os.Stderr, " %d events\n", len(events))

		cr := auditCategoryResult{
			Name:   cat.Name,
			Query:  query,
			Count:  len(events),
			Events: events,
		}
		result.Categories = append(result.Categories, cr)
		result.TotalEvents += len(events)
	}
	return result, nil
}

func renderAuditUser(r *auditUserResult, format string) error {
	switch format {
	case "json":
		return emitJSON(r)

	case "jsonl":
		enc := json.NewEncoder(os.Stdout)
		for _, cat := range r.Categories {
			for _, ev := range cat.Events {
				var m map[string]json.RawMessage
				if json.Unmarshal(ev, &m) == nil {
					catJSON, _ := json.Marshal(cat.Name)
					m["_category"] = catJSON
					if err := enc.Encode(m); err != nil {
						return err
					}
				} else {
					if err := enc.Encode(ev); err != nil {
						return err
					}
				}
			}
		}
		return nil

	case "csv":
		header := []string{"category", "count"}
		var rows [][]string
		for _, cat := range r.Categories {
			rows = append(rows, []string{cat.Name, fmt.Sprintf("%d", cat.Count)})
		}
		rows = append(rows, []string{"TOTAL", fmt.Sprintf("%d", r.TotalEvents)})
		return printCSVTo(os.Stdout, header, rows)

	default: // table
		fmt.Printf("User activity audit: %s\n", r.Email)
		fmt.Printf("Window: %s → %s\n\n", r.From, r.To)

		tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "CATEGORY\tEVENTS")
		for _, cat := range r.Categories {
			fmt.Fprintf(tw, "%s\t%d\n", cat.Name, cat.Count)
		}
		fmt.Fprintf(tw, "\t\n")
		fmt.Fprintf(tw, "TOTAL\t%d\n", r.TotalEvents)
		if err := tw.Flush(); err != nil {
			return err
		}

		if r.TotalEvents > 0 {
			fmt.Println("\nTip: use --format json for full event data, or --categories login to narrow scope.")
		}
		return nil
	}
}

// auditUserQueryForCategory returns the UDM query for a named category.
// Exported for testing.
func auditUserQueryForCategory(name, email string) string {
	for _, cat := range userActivityCategories {
		if cat.Name == name {
			return cat.Query(email)
		}
	}
	return ""
}
