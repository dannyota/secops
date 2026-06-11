package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// newSOARUsersCmd is the read-only SOAR user directory — the assignee lookup
// `soar case assign --user` needs (the case read shows an assignee's display name,
// not the id this flag consumes).
func newSOARUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "SOAR user directory (assignee lookup for `case assign --user`)",
	}
	cmd.AddCommand(newSOARUsersListCmd())
	return cmd
}

func newSOARUsersListCmd() *cobra.Command {
	var (
		includeOff bool
		grep       string
	)
	cmd := &cobra.Command{
		Use:   "list [--grep TEXT] [--all]",
		Short: "Read-only: list SOAR users (the USERNAME column is the value for `case assign --user`)",
		Long: "List SOAR users so an assignee's id is discoverable for `soar case assign\n" +
			"--user <USERNAME>`. Disabled accounts are hidden unless --all. --grep filters\n" +
			"case-insensitively over username / name / email. Reads only; metadata only\n" +
			"(no avatar, no secret).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			users, err := lc.ListUserProfiles(baseContext())
			if err != nil {
				return err
			}
			users = filterUsers(users, grep, includeOff)
			sort.Slice(users, func(i, j int) bool { return users[i].UserName < users[j].UserName })
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(users)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "USERNAME\tNAME\tEMAIL\tSOC ROLE\tPERMISSION GROUP\tDISABLED")
			for _, u := range users {
				disabled := ""
				if u.IsDisabled {
					disabled = "yes"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					u.UserName, dashIfEmpty(u.FullName()), dashIfEmpty(u.Email),
					dashIfEmpty(u.SOCRole), dashIfEmpty(u.PermissionGroup), dashIfEmpty(disabled))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "\n%d user(s). Assign with: secopsctl soar case assign --id <N> --user <USERNAME>\n", len(users))
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&includeOff, "all", false, "include disabled accounts (hidden by default)")
	f.StringVar(&grep, "grep", "", "filter over username/name/email (case-insensitive)")
	return markJSON(cmd)
}

// filterUsers drops disabled accounts (unless includeOff) and applies the grep
// substring over username/name/email.
func filterUsers(users []legacy.UserProfile, grep string, includeOff bool) []legacy.UserProfile {
	g := strings.ToLower(strings.TrimSpace(grep))
	out := users[:0:0]
	for _, u := range users {
		if u.IsDisabled && !includeOff {
			continue
		}
		if g != "" {
			hay := strings.ToLower(u.UserName + " " + u.FullName() + " " + u.Email)
			if !strings.Contains(hay, g) {
				continue
			}
		}
		out = append(out, u)
	}
	return out
}
