package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// newWatchlistsCmd is the watchlists surface (SIEM plane): read-only list/get
// plus the guarded membership write — putting a compromised user/asset ON a
// watchlist is a standard containment/tracking response action (membership
// also feeds the risk-score multiplier rules can key on).
func newWatchlistsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watchlists",
		Short: "SIEM entity watchlists: list/get (read-only) + guarded add-entity",
	}
	cmd.AddCommand(newWatchlistsListCmd(), newWatchlistsGetCmd(), newWatchlistsAddEntityCmd())
	return cmd
}

func newWatchlistsAddEntityCmd() *cobra.Command {
	var (
		ip, mac, hostname, userID, email, namespace string
		dryRun, yes                                 bool
	)
	cmd := &cobra.Command{
		Use:   "add-entity <watchlist-id> (--ip A | --mac M | --hostname H | --user U | --email E)",
		Short: "MUTATING (guarded): put one entity on a watchlist (containment/tracking)",
		Long: "Add an asset or user to a SIEM watchlist (entities:add). Exactly one\n" +
			"selector is set per the API contract. Guarded: dry-run by default, --yes to\n" +
			"apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entity, label, err := buildWatchlistEntity(ip, mac, hostname, userID, email, namespace)
			if err != nil {
				return err
			}
			action := fmt.Sprintf("watchlists add-entity %s (%s)", args[0], label)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				_, err = c.AddWatchlistEntity(baseContext(), args[0], entity)
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&ip, "ip", "", "asset IP address")
	f.StringVar(&mac, "mac", "", "asset MAC address")
	f.StringVar(&hostname, "hostname", "", "asset hostname")
	f.StringVar(&userID, "user", "", "user id")
	f.StringVar(&email, "email", "", "user email address")
	f.StringVar(&namespace, "namespace", "", "optional entity namespace")
	guardRunFlags(cmd, &dryRun, &yes)
	return cmd
}

// buildWatchlistEntity maps the selector flags to the Entity oneof — exactly
// one selector must be set (the API contract).
func buildWatchlistEntity(ip, mac, hostname, userID, email, namespace string) (chronicle.WatchlistEntity, string, error) {
	var (
		e     = chronicle.WatchlistEntity{Namespace: namespace}
		label string
		set   int
	)
	if ip != "" {
		e.Asset = map[string]any{"ip": []string{ip}}
		label, set = "asset ip "+ip, set+1
	}
	if mac != "" {
		e.Asset = map[string]any{"mac": []string{mac}}
		label, set = "asset mac "+mac, set+1
	}
	if hostname != "" {
		e.Asset = map[string]any{"hostname": hostname}
		label, set = "asset hostname "+hostname, set+1
	}
	if userID != "" {
		e.User = map[string]any{"userid": userID}
		label, set = "user "+userID, set+1
	}
	if email != "" {
		e.User = map[string]any{"email_addresses": []string{email}}
		label, set = "user email "+email, set+1
	}
	if set != 1 {
		return e, "", fmt.Errorf("set exactly one of --ip / --mac / --hostname / --user / --email")
	}
	return e, label, nil
}

func newWatchlistsListCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List watchlists",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			wls, err := c.ListWatchlists(baseContext(), limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(wls)
			}
			for _, w := range wls {
				fmt.Fprintf(os.Stdout, "%-28s %s\n", w.WatchlistID(), w.DisplayName)
			}
			fmt.Fprintf(os.Stdout, "\n%d watchlist(s)\n", len(wls))
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 100, "max watchlists to fetch/show")
	return cmd
}

func newWatchlistsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get one watchlist by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			w, err := c.GetWatchlist(baseContext(), args[0])
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(w)
		},
	}
	return cmd
}

func init() { rootCmd.AddCommand(newWatchlistsCmd()) }
