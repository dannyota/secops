package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// iocsFindBatch is the per-request lookup cap for `iocs find`: the iocs:find
// endpoint returns at most 1000 records (in request order), so larger inputs are
// chunked to this size and aggregated rather than silently truncated.
const iocsFindBatch = 1000

// newIoCsCmd builds the modern IoC command group — a read-only lookup over the
// SIEM-plane `iocs` surface (resolve an indicator value to its IoC record). It is
// the operational twin of `ti` (threat collections); both are read-only because
// Threat Intelligence is upstream-sourced.
func newIoCsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "indicators",
		Aliases: []string{"iocs"},
		Short:   "Indicators of Compromise (read-only): resolve a value to its IoC record",
		Long: "Look up an indicator (hash, domain, IP) against the tenant's IoC matches.\n" +
			"Read-only — Threat Intelligence is Google/Mandiant-sourced.",
	}
	cmd.AddCommand(newIoCsFindCmd(), newIoCsGetCmd(), newIoCsRelatedCmd())
	return cmd
}

func newIoCsFindCmd() *cobra.Command {
	var (
		typ      string
		fromFile string
	)
	cmd := &cobra.Command{
		Use:   "find [value...]",
		Short: "Resolve indicator value(s) to IoC records (iocs:find)",
		Long: "Resolve one or more indicators (hash/domain/IP) to their IoC records. Pass\n" +
			"values as arguments and/or read them from a file (or stdin with `--from-file -`),\n" +
			"one per line (blank lines and # comments skipped). Requests are chunked at\n" +
			"1000 indicators (the endpoint's per-request cap) and the results aggregated.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			values := append([]string{}, args...)
			if fromFile != "" {
				fileVals, ferr := readIndicatorList(cmd, fromFile)
				if ferr != nil {
					return fmt.Errorf("read --from-file %s: %w", fromFile, ferr)
				}
				values = append(values, fileVals...)
			}
			if len(values) == 0 {
				return fmt.Errorf("no indicators given (pass values as arguments or --from-file <path>)")
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			lookups := make([]chronicle.FieldAndValue, 0, len(values))
			for _, v := range values {
				vt, terr := iocValueType(v, typ)
				if terr != nil {
					return terr
				}
				lookups = append(lookups, chronicle.FieldAndValue{Value: v, ValueType: vt})
			}
			// iocs:find caps the response at 1000 records, in request order — so a
			// single call with more indicators would silently drop the overflow.
			// Chunk the lookups to stay under the cap and aggregate the results.
			var iocs []chronicle.Ioc
			for chunk := range slices.Chunk(lookups, iocsFindBatch) {
				found, ferr := c.FindIoCs(baseContext(), chunk...)
				if ferr != nil {
					return ferr
				}
				iocs = append(iocs, found...)
			}
			return emitIoCs(os.Stdout, iocs)
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "indicator type: md5|sha1|sha256|domain|ip (default: auto-detect)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "read indicators from a file (one per line; '-' for stdin)")
	return markJSON(cmd)
}

// readLines reads every line from path (or stdin for "-") verbatim — no trimming
// or comment filtering — dropping only a trailing newline. Use it for content
// where blank or #-prefixed lines are meaningful (e.g. parser sample logs).
func readLines(cmd *cobra.Command, path string) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = cmd.InOrStdin()
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	s := strings.TrimRight(string(data), "\n")
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
}

// readIndicatorList reads newline-delimited indicators from path (or stdin when
// path is "-"), skipping blank lines and # comments.
func readIndicatorList(cmd *cobra.Command, path string) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = cmd.InOrStdin()
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	var out []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
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
	return markJSON(cmd)
}

func newIoCsRelatedCmd() *cobra.Command {
	var (
		collectionType string
		limit          int
		orderBy        string
	)
	cmd := &cobra.Command{
		Use:   "related <ioc-id>",
		Short: "List threat collections related to an IoC resource",
		Long: "List campaigns and/or reports related to an IoC resource id. The id is the\n" +
			"resource id from `iocs find --json`, not the raw indicator value.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 1 || limit > 40 {
				return fmt.Errorf("--limit must be between 1 and 40")
			}
			types, err := relatedThreatCollectionTypes(collectionType)
			if err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			var out []chronicle.ThreatCollection
			for _, typ := range types {
				tcs, terr := c.FetchRelatedThreatCollections(baseContext(), chronicle.RelatedThreatCollectionQuery{
					Type:     typ,
					Ioc:      args[0],
					OrderBy:  orderBy,
					PageSize: limit,
					MaxPages: 1,
				})
				if terr != nil {
					return terr
				}
				out = append(out, tcs...)
			}
			return emitThreatCollections(os.Stdout, out)
		},
	}
	f := cmd.Flags()
	f.StringVar(&collectionType, "collection-type", "all", "related threat collection type: campaign|report|all")
	f.IntVar(&limit, "limit", 25, "maximum related collections per type (API max 40)")
	f.StringVar(&orderBy, "order", "last_modification_date-", "server order key")
	return markJSON(cmd)
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

func relatedThreatCollectionTypes(v string) ([]chronicle.RelatedThreatCollectionType, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "all":
		return []chronicle.RelatedThreatCollectionType{
			chronicle.RelatedThreatCollectionCampaign,
			chronicle.RelatedThreatCollectionReport,
		}, nil
	case "campaign", "campaigns":
		return []chronicle.RelatedThreatCollectionType{chronicle.RelatedThreatCollectionCampaign}, nil
	case "report", "reports":
		return []chronicle.RelatedThreatCollectionType{chronicle.RelatedThreatCollectionReport}, nil
	default:
		return nil, fmt.Errorf("unknown --collection-type %q (want campaign|report|all)", v)
	}
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
