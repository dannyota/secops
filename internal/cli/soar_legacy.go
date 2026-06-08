package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// newSOARLegacyCmd is the raw escape hatch for external-API operations not yet
// modeled as engine surfaces — so the full Siemplify surface is reachable as
// config-as-code (GET/POST-read to pull JSON, a guarded mutating method to push
// it back) without a typed wrapper per endpoint.
func newSOARLegacyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "legacy",
		Short: "Escape hatch: call any Siemplify external-API op (/api/external/v1)",
	}
	cmd.AddCommand(newSOARLegacyCallCmd())
	return cmd
}

func newSOARLegacyCallCmd() *cobra.Command {
	var (
		method   string
		body     string
		write    bool
		readOnly bool
		yes      bool
		out      string
		dryRun   bool
	)
	cmd := &cobra.Command{
		Use:   "call <op>",
		Short: "Call an external-API op, e.g. integrations/GetInstalledIntegrations",
		Long: "op is the path under /api/external/v1 (leading slash optional). GET is\n" +
			"read-only. The legacy API uses POST for BOTH reads and writes, so a POST\n" +
			"must declare intent: --read for a read-only call, or --write for a mutation.\n" +
			"PUT/DELETE/--write print the LIVE banner and require --yes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			op := args[0]
			method = strings.ToUpper(strings.TrimSpace(method))
			if method == "" {
				method = "GET"
			}
			// (--read and --write are mutually exclusive — enforced by cobra below.)

			var payload any
			if body != "" {
				raw, err := os.ReadFile(body)
				if err != nil {
					return err
				}
				if !json.Valid(raw) {
					return fmt.Errorf("%s is not valid JSON", body)
				}
				payload = json.RawMessage(raw)
			}

			// --read asserts a read-only call; it cannot apply to an inherently
			// mutating method (a contradiction the user should resolve, not us).
			if readOnly && (method == "PUT" || method == "DELETE") {
				return fmt.Errorf("--read is a read-only assertion and cannot be combined with --method %s", method)
			}

			// A POST can read or write on this API, so it is fail-closed: without an
			// explicit --read or --write it is refused rather than run ungated (a
			// forgotten --write on a write-POST would otherwise deploy live silently).
			if method == "POST" && !write && !readOnly {
				return fmt.Errorf("POST on the legacy API can read OR write; pass --read for a read-only call or --write (with --yes) for a mutation")
			}

			// Dry-run previews the composed request and sends nothing (the only
			// mutating path in the tool that previously lacked a dry-run).
			if dryRun {
				if jsonOut {
					return emitJSON(struct {
						Method string          `json:"method"`
						Op     string          `json:"op"`
						Write  bool            `json:"write"`
						DryRun bool            `json:"dry_run"`
						Body   json.RawMessage `json:"body,omitempty"`
					}{Method: method, Op: op, Write: write || method == "PUT" || method == "DELETE", DryRun: true, Body: bodyRaw(payload)})
				}
				fmt.Fprintf(os.Stdout, "DRY RUN — would send:\n  %s %s\n", method, op)
				if payload != nil {
					fmt.Fprintf(os.Stdout, "  body:\n%s\n", indentJSON(payload.(json.RawMessage)))
				} else {
					fmt.Fprintln(os.Stdout, "  body: (none)")
				}
				fmt.Fprintln(os.Stdout, "Re-run without --dry-run to send.")
				return nil
			}

			if write || method == "PUT" || method == "DELETE" {
				legacyCallBanner(method, op)
				if !yes {
					fmt.Fprintln(os.Stdout, "Refusing a mutating call without --yes. Aborted.")
					return nil
				}
			}

			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			resp, err := lc.Raw(baseContext(), method, op, payload)
			if err != nil {
				return err
			}
			pretty := indentJSON(resp)
			if out != "" {
				// 0600: a legacy response can carry sensitive operational data
				// (case/entity content, settings), so don't leave it world-readable.
				if err := os.WriteFile(out, pretty, 0o600); err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "wrote %d bytes -> %s\n", len(pretty), out)
				return nil
			}
			_, err = os.Stdout.Write(pretty)
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&method, "method", "GET", "HTTP method (GET, POST, PUT, DELETE)")
	f.StringVar(&body, "body", "", "JSON file to send as the request body")
	f.BoolVar(&write, "write", false, "mark this call as mutating (LIVE banner + --yes); required for a write-POST")
	f.BoolVar(&readOnly, "read", false, "assert a read-only POST (skips the mutation guard)")
	f.BoolVar(&yes, "yes", false, "confirm a mutating call")
	f.StringVar(&out, "out", "", "write the response to this file (0600) instead of stdout")
	f.BoolVar(&dryRun, "dry-run", false, "preview the composed request (method, op, body); send nothing")
	cmd.MarkFlagsMutuallyExclusive("read", "write")
	return cmd
}

// bodyRaw returns the request payload as json.RawMessage for the --json preview,
// or nil when there is no body.
func bodyRaw(payload any) json.RawMessage {
	if payload == nil {
		return nil
	}
	if rm, ok := payload.(json.RawMessage); ok {
		return rm
	}
	return nil
}

// legacyCallBanner warns before a mutating raw external-API call.
func legacyCallBanner(method, op string) {
	bar := strings.Repeat("!", 72)
	fmt.Fprintln(os.Stdout, bar)
	fmt.Fprintln(os.Stdout, "!! LIVE external-API call to a PRODUCTION SOAR tenant !!")
	fmt.Fprintf(os.Stdout, "!! %s %s\n", method, op)
	fmt.Fprintln(os.Stdout, bar)
}

// indentJSON pretty-prints a raw response for stdout/file output.
func indentJSON(raw json.RawMessage) []byte {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return append([]byte(nil), raw...)
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}
