package cli

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

func newSOARAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit <verb>",
		Short: "Browse SOAR audit logs and notifications",
	}
	cmd.AddCommand(newAuditListCmd(), newNotificationsCmd(), newReportTemplatesCmd())
	return cmd
}

func newAuditListCmd() *cobra.Command {
	var pageSize int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent SOAR audit log entries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{
				"requestedPage": 0,
				"pageSize":      pageSize,
			}
			return preferModern("soar audit list",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					raw, err := mc.AuditGetData(baseContext(), body)
					if err != nil {
						return err
					}
					if jsonOut {
						return writeRawJSON(os.Stdout, raw)
					}
					printGenericItemsSummary(cmd.OutOrStdout(), "audit entries", raw)
					return nil
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					raw, err := lc.AuditGetData(baseContext(), body)
					if err != nil {
						return err
					}
					if jsonOut {
						return writeRawJSON(os.Stdout, raw)
					}
					printGenericItemsSummary(cmd.OutOrStdout(), "audit entries", raw)
					return nil
				},
			)
		},
	}
	cmd.Flags().IntVar(&pageSize, "page-size", 50, "max entries per page")
	return markJSON(cmd)
}

func newNotificationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notifications <verb>",
		Short: "Manage user notifications (list, unread count, close)",
	}
	cmd.AddCommand(
		newNotificationsListCmd(),
		newNotificationsUnreadCmd(),
		newNotificationsCloseCmd(),
	)
	return cmd
}

func newNotificationsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List current user's notifications",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.NotificationListUser(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "notifications", raw)
			return nil
		},
	}
	return markJSON(cmd)
}

func newNotificationsUnreadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unread",
		Short: "Show unread notification count",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.NotificationGetUnreadCount(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "unread count", raw)
			return nil
		},
	}
	return markJSON(cmd)
}

func newNotificationsCloseCmd() *cobra.Command {
	var (
		id     int
		all    bool
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "close [--id N | --all]",
		Short: "MUTATING (guarded): close (dismiss) notifications",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && id <= 0 {
				return fmt.Errorf("pass --id <notification-id> or --all")
			}
			var label string
			if all {
				label = "notifications close-all"
			} else {
				label = "notification close " + strconv.Itoa(id)
			}
			return soarGuardedMutation(label, dryRun, yes, func() error {
				lc, err := newSOARLegacyClient()
				if err != nil {
					return err
				}
				if all {
					_, err = lc.NotificationCloseAll(baseContext())
				} else {
					_, err = lc.NotificationCloseUser(baseContext(), id)
				}
				return err
			})
		},
	}
	f := cmd.Flags()
	f.IntVar(&id, "id", 0, "notification record id to close")
	f.BoolVar(&all, "all", false, "close all notifications")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("id", "all")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

func newReportTemplatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report-templates",
		Short: "List SOAR report templates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.ReportGetTemplates(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "report templates", raw)
			return nil
		},
	}
	return markJSON(cmd)
}
