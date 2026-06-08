package cli

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

func init() {
	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show the resolved instance configuration (no API call)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			inst, err := loadInstance()
			if err != nil {
				return err
			}
			m := inst.AsMap()
			src := inst.SourcePath()
			if src == "" {
				src = "(none — environment only)"
			}
			m["config_source"] = src

			if jsonOut {
				b, err := json.MarshalIndent(m, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(b))
				return nil
			}

			keys := make([]string, 0, len(m))
			width := 0
			for k := range m {
				keys = append(keys, k)
				if len(k) > width {
					width = len(k)
				}
			}
			sort.Strings(keys)

			fmt.Println("SecOps instance configuration")
			fmt.Println("----------------------------------------")
			for _, k := range keys {
				fmt.Printf("  %-*s  %s\n", width, k, m[k])
			}
			return nil
		},
	}
	infoCmd.AddCommand(newInfoSOARIntegrationsCmd())
	rootCmd.AddCommand(infoCmd)
}
