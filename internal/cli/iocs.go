package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// newIoCsCmd builds the modern IoC command group — a read-only lookup over the
// SIEM-plane `iocs` surface (resolve an indicator value to its IoC record). It is
// the operational twin of `ti` (threat collections); both are read-only because
// Threat Intelligence is upstream-sourced.
func newIoCsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "iocs",
		Short: "Indicators of Compromise (read-only): resolve a value to its IoC record",
		Long: "Look up an indicator (hash, domain, IP) against the tenant's IoC matches.\n" +
			"Read-only — Threat Intelligence is Google/Mandiant-sourced.",
	}
	cmd.AddCommand(newIoCsFindCmd(), newIoCsGetCmd())
	return cmd
}

func newIoCsFindCmd() *cobra.Command {
	var typ string
	cmd := &cobra.Command{
		Use:   "find <value> [value...]",
		Short: "Resolve indicator value(s) to IoC records (iocs:find)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			lookups := make([]chronicle.FieldAndValue, 0, len(args))
			for _, v := range args {
				vt, terr := iocValueType(v, typ)
				if terr != nil {
					return terr
				}
				lookups = append(lookups, chronicle.FieldAndValue{Value: v, ValueType: vt})
			}
			iocs, err := c.FindIoCs(baseContext(), lookups...)
			if err != nil {
				return err
			}
			return emitIoCs(os.Stdout, iocs)
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "indicator type: md5|sha1|sha256|domain|ip (default: auto-detect)")
	return cmd
}

func newIoCsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <ioc-id>",
		Short: "Get one IoC by its resource id (from `iocs find`, not the raw value)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ioc, err := c.GetIoC(baseContext(), args[0])
			if err != nil {
				return err
			}
			return emitIoCs(os.Stdout, []chronicle.Ioc{*ioc})
		},
	}
	return cmd
}

// iocValueType resolves the IoCValueType for value v: an explicit --type override
// wins; otherwise it is inferred from the value's shape (IP, hex hash length, or
// a dotted name → domain). Returns an error when the type can't be inferred.
func iocValueType(v, override string) (chronicle.IoCValueType, error) {
	if override != "" {
		switch strings.ToLower(override) {
		case "md5":
			return chronicle.IoCValueMD5, nil
		case "sha1":
			return chronicle.IoCValueSHA1, nil
		case "sha256":
			return chronicle.IoCValueSHA256, nil
		case "domain":
			return chronicle.IoCValueDomain, nil
		case "ip":
			return chronicle.IoCValueIP, nil
		default:
			return "", fmt.Errorf("unknown --type %q (want md5|sha1|sha256|domain|ip)", override)
		}
	}
	if net.ParseIP(v) != nil {
		return chronicle.IoCValueIP, nil
	}
	if h := strings.ToLower(v); isHex(h) {
		switch len(h) {
		case 32:
			return chronicle.IoCValueMD5, nil
		case 40:
			return chronicle.IoCValueSHA1, nil
		case 64:
			return chronicle.IoCValueSHA256, nil
		}
	}
	if strings.Contains(v, ".") {
		return chronicle.IoCValueDomain, nil
	}
	return "", fmt.Errorf("cannot infer indicator type for %q — pass --type", v)
}

// isHex reports whether s is a non-empty lowercase hex string.
func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// emitIoCs prints IoC records as a compact table, or the raw server objects as a
// JSON array under --json.
func emitIoCs(w io.Writer, iocs []chronicle.Ioc) error {
	if jsonOut {
		parts := make([]json.RawMessage, 0, len(iocs))
		for i := range iocs {
			if len(iocs[i].Raw) > 0 {
				parts = append(parts, iocs[i].Raw)
			}
		}
		b, err := json.Marshal(parts)
		if err != nil {
			return err
		}
		return writeRawJSON(w, b)
	}
	if len(iocs) == 0 {
		fmt.Fprintln(w, "no IoC records matched.")
		return nil
	}
	// The resource id and full record are long; show the human-useful columns here
	// and leave the id/name to --json (the id feeds `iocs get`).
	fmt.Fprintf(w, "%-18s %s\n", "TYPE", "INDICATOR")
	for i := range iocs {
		ic := &iocs[i]
		fmt.Fprintf(w, "%-18s %s\n", orDash(ic.IocType), ic.DisplayName)
	}
	fmt.Fprintf(w, "\n%d IoC record(s) — `--json` for full records + ids\n", len(iocs))
	return nil
}

func init() { rootCmd.AddCommand(newIoCsCmd()) }
