package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/hev/puff/internal/client"
	"github.com/hev/puff/internal/debug"
	"github.com/hev/puff/internal/output"
	"github.com/spf13/cobra"
	"github.com/turbopuffer/turbopuffer-go"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan a namespace and extract all unique values of a field",
	RunE:  runScan,
}

func init() {
	scanCmd.Flags().StringP("namespace", "n", "", "Namespace to scan")
	scanCmd.Flags().String("field", "", "Field name to extract unique values from")
	scanCmd.Flags().StringP("region", "r", "", "Override the region")
	scanCmd.Flags().Int("page-size", 1000, "Batch size per query")
	_ = scanCmd.MarkFlagRequired("namespace")
	_ = scanCmd.MarkFlagRequired("field")
	rootCmd.AddCommand(scanCmd)
}

func runScan(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	field, _ := cmd.Flags().GetString("field")
	region, _ := cmd.Flags().GetString("region")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	ctx := context.Background()

	ns, err := client.GetNamespace(namespace, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	// Get total count for progress
	md, err := ns.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
	totalDocs := int64(0)
	if err == nil {
		totalDocs = md.ApproxRowCount
	}

	uniqueValues := make(map[string]bool)
	var lastID string
	totalScanned := 0

	for {
		queryParams := turbopuffer.NamespaceQueryParams{
			RankBy:            turbopuffer.NewRankByAttribute("id", turbopuffer.RankByAttributeOrderAsc),
			IncludeAttributes: turbopuffer.IncludeAttributesParam{StringArray: []string{field}},
			TopK:              turbopuffer.Int(int64(pageSize)),
			Consistency: turbopuffer.NamespaceQueryParamsConsistency{
				Level: turbopuffer.NamespaceQueryParamsConsistencyLevelStrong,
			},
		}

		if lastID != "" {
			queryParams.Filters = turbopuffer.NewFilterGt("id", lastID)
		}

		debug.Log("Scan query", map[string]any{
			"last_id":   lastID,
			"page_size": pageSize,
		})

		result, err := ns.Query(ctx, queryParams)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		for _, row := range result.Rows {
			if v, ok := row[field]; ok && v != nil {
				uniqueValues[fmt.Sprintf("%v", v)] = true
			}
			if id, ok := row["id"]; ok {
				lastID = fmt.Sprintf("%v", id)
			}
		}

		totalScanned += len(result.Rows)

		if !output.IsPlain() {
			if totalDocs > 0 {
				fmt.Fprintf(os.Stderr, "\r%d unique values — scanned %d/%d documents",
					len(uniqueValues), totalScanned, totalDocs)
			} else {
				fmt.Fprintf(os.Stderr, "\r%d unique values — scanned %d documents",
					len(uniqueValues), totalScanned)
			}
		}

		debug.Log("Scan page", map[string]any{
			"rows":          len(result.Rows),
			"total_scanned": totalScanned,
			"unique_values": len(uniqueValues),
			"last_id":       lastID,
		})

		if len(result.Rows) < pageSize {
			break
		}
	}

	if !output.IsPlain() {
		fmt.Fprintf(os.Stderr, "\nDone. Scanned %d documents, found %d unique values.\n",
			totalScanned, len(uniqueValues))
	}

	// Output sorted unique values as JSON array
	sorted := make([]string, 0, len(uniqueValues))
	for v := range uniqueValues {
		sorted = append(sorted, v)
	}
	sort.Strings(sorted)

	b, _ := json.Marshal(sorted)
	fmt.Println(string(b))
	return nil
}
