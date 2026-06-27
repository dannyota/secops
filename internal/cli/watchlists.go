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
	cmd.AddCommand(newWatchlistsListCmd(), newWatchlistsGetCmd(), newWatchlistsAddEntityCmd(),
		newWatchlistsCreateCmd(), newWatchlistsDeleteCmd(), newWatchlistsRemoveEntityCmd())
	return cmd
}

func newWatchlistsCreateCmd() *cobra.Command {
	var name, displayName, description string
	var factor float64
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "create --name <id> --display-name <s> [--description s] [--factor f]",
		Short: "MUTATING (guarded): create a watchlist (tracking/hunting list)",
		Long: "Create a watchlist for tracking or hunting — a named set of entities whose\n" +
			"membership can feed the risk-score multiplier. --factor is the risk\n" +
			"multiplier (default 1.0). Guarded: dry-run by default, --yes to apply.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			action := fmt.Sprintf("watchlists create %q (factor %.2f)", name, factor)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				w, err := c.CreateWatchlist(baseContext(), name, displayName, description, factor)
				if err != nil {
					return err
				}
				if jsonOut {
					return json.NewEncoder(os.Stdout).Encode(w)
				}
				fmt.Fprintf(os.Stdout, "Created watchlist %s.\n", w.WatchlistID())
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "watchlist id/name (required)")
	f.StringVar(&displayName, "display-name", "", "human-readable display name (required)")
	f.StringVar(&description, "description", "", "optional description")
	f.Float64Var(&factor, "factor", 1.0, "risk-score multiplying factor")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("display-name")
	return markJSON(cmd)
}

func newWatchlistsDeleteCmd() *cobra.Command {
	var force, dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "delete <id> [--force]",
		Short: "MUTATING (guarded): delete a watchlist",
		Long: "Delete a watchlist by id. --force removes it even if it still has members.\n" +
			"Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			action := fmt.Sprintf("watchlists delete %s (force=%v)", args[0], force)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				if err := c.DeleteWatchlist(baseContext(), args[0], force); err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "Deleted watchlist %s.\n", args[0])
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "delete even if the watchlist has members")
	guardRunFlags(cmd, &dryRun, &yes)
	return cmd
}

func newWatchlistsRemoveEntityCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "remove-entity <entity-resource-name>",
		Short: "MUTATING (guarded): take one entity off a watchlist",
		Long: "Remove an entity membership from a watchlist by its full entity resource\n" +
			"name (the name a watchlist's membership listing returns) — the inverse of\n" +
			"`add-entity`. Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			action := fmt.Sprintf("watchlists remove-entity %s", args[0])
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				return c.RemoveWatchlistEntity(baseContext(), args[0])
			})
		},
	}
	guardRunFlags(cmd, &dryRun, &yes)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	// Output is always JSON (the full watchlist object), like `rules alerts`.
	return markJSON(cmd)
}
