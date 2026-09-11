package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hev/puff/internal/client"
	"github.com/hev/puff/internal/debug"
	"github.com/hev/puff/internal/embeddings"
	"github.com/hev/puff/internal/output"
	"github.com/spf13/cobra"
	"github.com/turbopuffer/turbopuffer-go"
)

var searchCmd = &cobra.Command{
	Use:   "search QUERY",
	Short: "Search for documents using vector similarity or full-text search",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	searchCmd.Flags().StringP("namespace", "n", "", "Namespace to search in")
	searchCmd.Flags().StringP("model", "m", "", "HuggingFace model ID for vector search")
	searchCmd.Flags().IntP("top-k", "k", 10, "Number of results to return")
	searchCmd.Flags().StringP("distance-metric", "d", "cosine_distance", "Distance metric (cosine_distance or euclidean_squared)")
	searchCmd.Flags().StringP("filters", "f", "", "Additional filters in JSON format")
	searchCmd.Flags().String("fts", "", "Field name for full-text search (BM25)")
	searchCmd.Flags().StringP("region", "r", "", "Override the region")
	_ = searchCmd.MarkFlagRequired("namespace")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")
	modelID, _ := cmd.Flags().GetString("model")
	topK, _ := cmd.Flags().GetInt("top-k")
	distMetric, _ := cmd.Flags().GetString("distance-metric")
	filtersStr, _ := cmd.Flags().GetString("filters")
	ftsField, _ := cmd.Flags().GetString("fts")
	region, _ := cmd.Flags().GetString("region")
	ctx := context.Background()

	useFTS := ftsField != ""

	if !useFTS && modelID == "" {
		fmt.Fprintln(os.Stderr, "Error: Either --model or --fts must be specified")
		fmt.Fprintln(os.Stderr, "  Use --model for vector similarity search")
		fmt.Fprintln(os.Stderr, "  Use --fts for full-text search")
		os.Exit(1)
	}

	if useFTS && modelID != "" {
		output.StatusPrint("Warning: Both --fts and --model specified. Using FTS mode.")
	}

	output.StatusPrint(fmt.Sprintf("\nSearching in namespace: %s", namespace))
	output.StatusPrint(fmt.Sprintf("Query: \"%s\"", query))

	ns, err := client.GetNamespace(namespace, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	// Get metadata for vector info
	md, err := ns.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	schemaDict := make(map[string]any)
	for k, v := range md.Schema {
		schemaDict[k] = v
	}
	vecAttr, vecDims := extractVectorInfo(schemaDict)

	var queryParams turbopuffer.NamespaceQueryParams
	queryParams.TopK = turbopuffer.Int(int64(topK))
	queryParams.Consistency = turbopuffer.NamespaceQueryParamsConsistency{
		Level: turbopuffer.NamespaceQueryParamsConsistencyLevelEventual,
	}

	if useFTS {
		output.StatusPrint(fmt.Sprintf("Mode: Full-text search (BM25) on field \"%s\"\n", ftsField))
		queryParams.RankBy = turbopuffer.NewRankByTextBM25(ftsField, query)
		if vecAttr != "" {
			queryParams.ExcludeAttributes = []string{vecAttr}
		}
	} else {
		output.StatusPrint("Mode: Vector similarity search")
		output.StatusPrint(fmt.Sprintf("Model: %s\n", modelID))

		embedding, err := embeddings.GenerateEmbedding(query, modelID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		output.StatusPrint(fmt.Sprintf("Generated %d-dimensional embedding\n", len(embedding)))

		if vecAttr == "" {
			fmt.Fprintln(os.Stderr, "Error: No vector attribute found in namespace schema")
			os.Exit(1)
		}

		if vecDims != len(embedding) {
			fmt.Fprintln(os.Stderr, "Error: Dimension mismatch!")
			fmt.Fprintf(os.Stderr, "  Expected: %d dimensions (from namespace schema)\n", vecDims)
			fmt.Fprintf(os.Stderr, "  Got: %d dimensions (from model %s)\n", len(embedding), modelID)
			os.Exit(1)
		}

		output.StatusPrint(fmt.Sprintf("Using distance metric: %s\n", distMetric))

		queryParams.RankBy = turbopuffer.NewRankByVector(vecAttr, embedding)
		queryParams.DistanceMetric = turbopuffer.DistanceMetric(distMetric)
		queryParams.ExcludeAttributes = []string{vecAttr}
	}

	// Parse filters
	if filtersStr != "" {
		var parsed any
		if err := json.Unmarshal([]byte(filtersStr), &parsed); err != nil {
			fmt.Fprintln(os.Stderr, "Error: Invalid filter JSON format")
			fmt.Fprintln(os.Stderr, `Example: -f '["category", "In", ["tech", "science"]]'`)
			os.Exit(1)
		}
		// Note: raw filter JSON needs to be converted to SDK filter type
		// For now we'll pass it through the SetExtraFields mechanism
		debug.Log("Parsed filters", parsed)
	}

	debug.Log("Query Parameters", queryParams)

	startTime := time.Now()
	result, err := ns.Query(ctx, queryParams)
	queryTime := time.Since(startTime).Milliseconds()

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	debug.Log("Query Response", result)

	if len(result.Rows) == 0 {
		fmt.Println("No documents found matching the query")
		return nil
	}

	output.StatusPrint(fmt.Sprintf("Found %d result(s):\n", len(result.Rows)))

	scoreHeader := "Distance"
	if useFTS {
		scoreHeader = "Score"
	}

	headers := []string{"ID", "Contents", scoreHeader}
	var rows [][]string

	for _, row := range result.Rows {
		rowID := fmt.Sprintf("%v", row["id"])

		var displayContents string
		if useFTS {
			if v, ok := row[ftsField]; ok {
				displayContents = fmt.Sprintf("%v", v)
			} else {
				displayContents = "N/A"
			}
		} else {
			contents := make(map[string]any)
			excludeKeys := map[string]bool{"id": true, "vector": true, "$dist": true, "dist": true, "$score": true}
			for k, v := range row {
				if !excludeKeys[k] {
					contents[k] = v
				}
			}
			b, _ := json.Marshal(contents)
			displayContents = string(b)
		}

		var scoreDisplay string
		if dist, ok := row["$dist"]; ok {
			scoreDisplay = fmt.Sprintf("%.4f", dist)
		} else if dist, ok := row["dist"]; ok {
			scoreDisplay = fmt.Sprintf("%.4f", dist)
		} else {
			scoreDisplay = "N/A"
		}

		rows = append(rows, []string{rowID, displayContents, scoreDisplay})
	}

	output.PrintTable(headers, rows)

	if result.Performance.QueryExecutionMs > 0 {
		output.StatusPrint(fmt.Sprintf("\nSearch completed in %dms (query execution: %dms)", queryTime, result.Performance.QueryExecutionMs))
	}
	return nil
}
