package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/hev/puff/internal/client"
	"github.com/hev/puff/internal/debug"
	"github.com/hev/puff/internal/metadata"
	"github.com/hev/puff/internal/output"
	"github.com/spf13/cobra"
	"github.com/turbopuffer/turbopuffer-go"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List namespaces or documents in a namespace",
	RunE:    runList,
}

func init() {
	listCmd.Flags().StringP("namespace", "n", "", "Namespace to list documents from")
	listCmd.Flags().IntP("top-k", "k", 10, "Number of documents to return")
	listCmd.Flags().StringP("region", "r", "", "Override the region")
	listCmd.Flags().BoolP("all", "A", false, "Query all regions")
	listCmd.Flags().Bool("recall", false, "Include recall estimation (slower)")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	topK, _ := cmd.Flags().GetInt("top-k")
	region, _ := cmd.Flags().GetString("region")
	allRegions, _ := cmd.Flags().GetBool("all")
	includeRecall, _ := cmd.Flags().GetBool("recall")
	ctx := context.Background()

	if allRegions {
		includeRecall = true
	}

	if allRegions && region != "" {
		fmt.Fprintln(os.Stderr, "Error: Cannot use both --all and --region flags together")
		os.Exit(1)
	}

	if namespace != "" {
		if allRegions {
			fmt.Fprintln(os.Stderr, "Error: --all flag is not supported when querying a specific namespace")
			os.Exit(1)
		}
		return displayNamespaceDocuments(ctx, namespace, topK, region)
	}
	return displayNamespaces(ctx, allRegions, region, includeRecall)
}

func extractVectorInfo(schemaData map[string]any) (string, int) {
	re := regexp.MustCompile(`^\[(\d+)\]f(?:16|32)$`)
	for attrName, attrConfig := range schemaData {
		var typeStr string
		switch v := attrConfig.(type) {
		case string:
			typeStr = v
		case map[string]any:
			if t, ok := v["type"].(string); ok {
				typeStr = t
			}
		default:
			// SDK returns typed structs (e.g. AttributeSchemaConfig).
			// Use JSON round-trip to extract the "type" field.
			if b, err := json.Marshal(v); err == nil {
				var obj struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(b, &obj) == nil {
					typeStr = obj.Type
				}
			}
		}
		if typeStr != "" {
			if m := re.FindStringSubmatch(typeStr); m != nil {
				dims, _ := strconv.Atoi(m[1])
				return attrName, dims
			}
		}
	}
	return "", 0
}

func displayNamespaceDocuments(ctx context.Context, namespace string, topK int, region string) error {
	ns, err := client.GetNamespace(namespace, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	output.StatusPrint(fmt.Sprintf("\nQuerying namespace: %s (top %d results)\n", namespace, topK))

	md, err := ns.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	// Build schema dict from metadata
	schemaDict := make(map[string]any)
	for k, v := range md.Schema {
		schemaDict[k] = v
	}

	vecAttr, dims := extractVectorInfo(schemaDict)
	if vecAttr == "" {
		fmt.Fprintln(os.Stderr, "Error: No vector attribute found in namespace schema")
		os.Exit(1)
	}

	output.StatusPrint(fmt.Sprintf("Using %d-dimensional zero vector for query\n", dims))

	zeroVec := make([]float32, dims)

	debug.Log("Query Parameters", map[string]any{
		"rank_by":            []any{vecAttr, "ANN", "zero_vector"},
		"top_k":              topK,
		"exclude_attributes": []string{vecAttr},
	})

	result, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		RankBy:            turbopuffer.NewRankByVector(vecAttr, zeroVec),
		TopK:              turbopuffer.Int(int64(topK)),
		ExcludeAttributes: []string{vecAttr},
		Consistency: turbopuffer.NamespaceQueryParamsConsistency{
			Level: turbopuffer.NamespaceQueryParamsConsistencyLevelEventual,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	if len(result.Rows) == 0 {
		fmt.Println("No documents found in namespace")
		return nil
	}

	output.StatusPrint(fmt.Sprintf("Found %d document(s):\n", len(result.Rows)))

	headers := []string{"ID", "Contents"}
	var rows [][]string
	for _, row := range result.Rows {
		rowID := fmt.Sprintf("%v", row["id"])
		contents := make(map[string]any)
		excludeKeys := map[string]bool{"id": true, "vector": true, "$dist": true, "dist": true}
		for k, v := range row {
			if !excludeKeys[k] {
				contents[k] = v
			}
		}
		contentsJSON, _ := json.Marshal(contents)
		rows = append(rows, []string{rowID, string(contentsJSON)})
	}

	output.PrintTable(headers, rows)

	if result.Performance.QueryExecutionMs > 0 {
		output.StatusPrint(fmt.Sprintf("\nQuery took %.2fms", float64(result.Performance.QueryExecutionMs)))
	}
	return nil
}

func displayNamespaces(ctx context.Context, allRegions bool, region string, includeRecall bool) error {
	nsWithMeta := metadata.FetchNamespacesWithMetadata(ctx, allRegions, region, includeRecall)

	if len(nsWithMeta) == 0 {
		fmt.Println("No namespaces found")
		return nil
	}

	output.StatusPrint(fmt.Sprintf("\nFound %d namespace(s):\n", len(nsWithMeta)))

	// Sort by updated_at descending
	sort.Slice(nsWithMeta, func(i, j int) bool {
		ti := time.Time{}
		tj := time.Time{}
		if nsWithMeta[i].Metadata != nil {
			ti = nsWithMeta[i].Metadata.UpdatedAt
		}
		if nsWithMeta[j].Metadata != nil {
			tj = nsWithMeta[j].Metadata.UpdatedAt
		}
		return ti.After(tj)
	})

	headers := []string{"Namespace"}
	if allRegions {
		headers = append(headers, "Region")
	}
	headers = append(headers, "Rows", "Logical Bytes", "Index Status", "Unindexed Bytes")
	if includeRecall {
		headers = append(headers, "Recall")
	}
	headers = append(headers, "Updated")

	var rows [][]string
	for _, item := range nsWithMeta {
		var row []string
		row = append(row, item.NamespaceID)
		if allRegions {
			row = append(row, item.Region)
		}

		if item.Metadata != nil {
			indexStatus := metadata.GetIndexStatus(item.Metadata)
			unindexed := metadata.GetUnindexedBytes(item.Metadata)

			row = append(row,
				formatNumber(item.Metadata.ApproxRowCount),
				formatBytes(item.Metadata.ApproxLogicalBytes),
				indexStatus,
				formatBytes(unindexed),
			)
			if includeRecall {
				row = append(row, formatRecall(item.Recall))
			}
			row = append(row, formatUpdatedAt(item.Metadata.UpdatedAt))
		} else {
			row = append(row, "N/A", "N/A", "N/A", "N/A")
			if includeRecall {
				row = append(row, "N/A")
			}
			row = append(row, "N/A")
		}
		rows = append(rows, row)
	}

	output.PrintTable(headers, rows)
	return nil
}

func formatBytes(b int64) string {
	if b == 0 {
		return "0 B"
	}
	const k = 1024
	sizes := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	fb := float64(b)
	for fb >= float64(k) && i < len(sizes)-1 {
		fb /= float64(k)
		i++
	}
	return fmt.Sprintf("%.2f %s", fb, sizes[i])
}

func formatNumber(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func formatUpdatedAt(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	now := time.Now()
	if t.Year() == now.Year() && t.Month() == now.Month() && t.Day() == now.Day() {
		return t.Format("3:04 pm")
	}
	return t.Format("Jan 2, 2006")
}

func formatRecall(rd *metadata.RecallData) string {
	if rd == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.1f%%", rd.AvgRecall*100)
}
