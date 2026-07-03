package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func newTIAssociationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "associations <id>...",
		Short: "Show IoC associations (malware families, threat actors) by id",
		Long: "Fetch one or more IoC associations by short id (e.g.\n" +
			"malware--<uuid>, threat-actor--<uuid>) or full resource name.\n" +
			"Ids are chunked automatically to stay within URL-length limits.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			assocs, err := c.BatchGetIocAssociations(baseContext(), args...)
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(assocs)
			}
			if len(assocs) == 0 {
				fmt.Fprintln(os.Stdout, "no IoC associations returned (ATI may not be licensed).")
				return nil
			}
			fmt.Fprintf(os.Stdout, "%-18s %-40s %s\n", "TYPE", "ID", "DISPLAY_NAME")
			for i := range assocs {
				a := &assocs[i]
				fmt.Fprintf(os.Stdout, "%-18s %-40s %s\n",
					orDash(a.AssociationType),
					orDash(a.ID),
					truncate(a.ThreatDisplayName, 72),
				)
			}
			fmt.Fprintf(os.Stdout, "\n%d association(s)\n", len(assocs))
			return nil
		},
	}
	return markJSON(cmd)
}

func newTIRelatedAssociationsCmd() *cobra.Command {
	var (
		assocType string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "related-associations <id>",
		Short: "List IoC associations related to a threat resource",
		Long: "Fetch IoC associations related to an IoC, another IoC association,\n" +
			"or a threat collection. The id is resolved to the appropriate resource\n" +
			"type based on its prefix (iocs/, iocAssociations/, threatCollections/,\n" +
			"or short forms like malware--<uuid>, threat-actor--<uuid>,\n" +
			"campaign--<uuid>, report--<uuid>).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			q := relatedAssociationQuery(args[0], assocType, limit)
			assocs, err := c.FetchRelatedAssociations(baseContext(), q)
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(assocs)
			}
			if len(assocs) == 0 {
				fmt.Fprintln(os.Stdout, "no related associations found.")
				return nil
			}
			fmt.Fprintf(os.Stdout, "%-18s %-40s %s\n", "TYPE", "ID", "DISPLAY_NAME")
			for i := range assocs {
				a := &assocs[i]
				fmt.Fprintf(os.Stdout, "%-18s %-40s %s\n",
					orDash(a.AssociationType),
					orDash(a.ID),
					truncate(a.ThreatDisplayName, 72),
				)
			}
			fmt.Fprintf(os.Stdout, "\n%d association(s)\n", len(assocs))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&assocType, "type", "", "filter by type: malware, threat-actor, or toolkit (default: all)")
	f.IntVar(&limit, "limit", 25, "maximum associations to return")
	return markJSON(cmd)
}

// relatedAssociationQuery constructs a RelatedAssociationQuery by guessing the
// resource kind from the id prefix.
func relatedAssociationQuery(id, assocType string, limit int) chronicle.RelatedAssociationQuery {
	q := chronicle.RelatedAssociationQuery{
		PageSize: limit,
		MaxPages: 1,
	}
	switch strings.ToLower(assocType) {
	case "malware", "malware_family":
		q.Type = chronicle.AssociationMalware
	case "threat-actor", "threat_actor":
		q.Type = chronicle.AssociationThreatActor
	case "toolkit", "software-toolkit", "software_toolkit":
		q.Type = chronicle.AssociationSoftwareToolkit
	}
	// Route id to the right field based on prefix.
	lower := strings.ToLower(id)
	switch {
	case strings.HasPrefix(lower, "malware--") || strings.HasPrefix(lower, "threat-actor--"):
		q.IocAssociation = id
	case strings.HasPrefix(lower, "campaign--") || strings.HasPrefix(lower, "report--") ||
		strings.HasPrefix(lower, "actor--") || strings.HasPrefix(lower, "vulnerability--"):
		q.ThreatCollection = id
	case strings.Contains(id, "iocAssociations/"):
		q.IocAssociation = id
	case strings.Contains(id, "threatCollections/"):
		q.ThreatCollection = id
	case strings.Contains(id, "iocs/"):
		q.Ioc = id
	default:
		// Fall back to IoC (a raw indicator value).
		q.Ioc = id
	}
	return q
}
