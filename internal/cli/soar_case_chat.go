package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newCaseChatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat <verb>",
		Short: "Case-wall chat: list, send, pin/unpin messages",
	}
	cmd.AddCommand(
		newCaseChatListCmd(),
		newCaseChatSendCmd(),
		newCaseChatUnreadCmd(),
	)
	return cmd
}

func newCaseChatListCmd() *cobra.Command {
	var caseID int
	cmd := &cobra.Command{
		Use:   "list --case-id N",
		Short: "List chat messages on a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			return preferModern("soar case chat list",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					raw, err := mc.CaseChatList(baseContext(), caseID)
					if err != nil {
						return err
					}
					return renderChatMessages(raw)
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					raw, err := lc.CaseChatList(baseContext(), caseID, nil)
					if err != nil {
						return err
					}
					return renderChatMessages(raw)
				},
			)
		},
	}
	cmd.Flags().IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
}

func newCaseChatSendCmd() *cobra.Command {
	var (
		caseID  int
		message string
		dryRun  bool
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "send --case-id N --message <text>",
		Short: "MUTATING (guarded): send a chat message to a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			message = strings.TrimSpace(message)
			if message == "" {
				return fmt.Errorf("--message is required")
			}
			label := fmt.Sprintf("case %d chat send", caseID)
			dr, ay := soarGuard(label, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Case: %d\nMessage: %s\n", caseID, truncate(message, 80))
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN — no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to apply without confirmation (pass --yes). Aborted.")
				return nil
			}
			body := map[string]any{"text": message}
			return preferModern("soar case chat send",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					_, err = mc.CaseChatSend(baseContext(), caseID, body)
					if err != nil {
						return err
					}
					fmt.Fprintln(os.Stdout, "message sent.")
					return nil
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					_, err = lc.CaseChatPost(baseContext(), caseID, body)
					if err != nil {
						return err
					}
					fmt.Fprintln(os.Stdout, "message sent.")
					return nil
				},
			)
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&message, "message", "", "message text (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

func newCaseChatUnreadCmd() *cobra.Command {
	var caseID int
	cmd := &cobra.Command{
		Use:   "unread-count --case-id N",
		Short: "Show unread message count for a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			return preferModern("soar case chat unread-count",
				func() error {
					mc, err := newSOARClient()
					if err != nil {
						return err
					}
					raw, err := mc.CaseChatUnreadCount(baseContext(), caseID)
					if err != nil {
						return err
					}
					if jsonOut {
						return writeRawJSON(os.Stdout, raw)
					}
					printGenericItemsSummary(cmd.OutOrStdout(), "unread count", raw)
					return nil
				},
				func() error {
					lc, err := newSOARLegacyClient()
					if err != nil {
						return err
					}
					raw, err := lc.CaseChatNewMessagesCount(baseContext(), caseID)
					if err != nil {
						return err
					}
					if jsonOut {
						return writeRawJSON(os.Stdout, raw)
					}
					printGenericItemsSummary(cmd.OutOrStdout(), "unread count", raw)
					return nil
				},
			)
		},
	}
	cmd.Flags().IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
}

// chatMsg covers both v1alpha (text/author/pinned) and legacy (text/username/pinned) field names.
type chatMsg struct {
	Text     string `json:"text"`
	Author   string `json:"author"`
	Username string `json:"username"`
	Pinned   bool   `json:"pinned"`
	ID       int    `json:"id"`
}

func renderChatMessages(raw json.RawMessage) error {
	if jsonOut {
		return writeRawJSON(os.Stdout, raw)
	}
	// The v1alpha response wraps in {"chatMessages": [...]}.
	var wrapped struct {
		ChatMessages []chatMsg `json:"chatMessages"`
	}
	var msgs []chatMsg
	if json.Unmarshal(raw, &wrapped) == nil && len(wrapped.ChatMessages) > 0 {
		msgs = wrapped.ChatMessages
	} else {
		_ = json.Unmarshal(raw, &msgs)
	}
	if len(msgs) == 0 {
		fmt.Fprintln(os.Stdout, "no chat messages.")
		return nil
	}
	for i := range msgs {
		m := &msgs[i]
		who := m.Author
		if who == "" {
			who = m.Username
		}
		pin := ""
		if m.Pinned {
			pin = " [pinned]"
		}
		fmt.Fprintf(os.Stdout, "[%d] %s%s: %s\n", m.ID, orDash(who), pin, truncate(strings.TrimSpace(m.Text), 120))
	}
	fmt.Fprintf(os.Stdout, "\n%d message(s).\n", len(msgs))
	return nil
}
