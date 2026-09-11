package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/hev/puff/internal/client"
	"github.com/hev/puff/internal/debug"
	"github.com/hev/puff/internal/output"
	"github.com/spf13/cobra"
	"github.com/turbopuffer/turbopuffer-go"
)

var getCmd = &cobra.Command{
	Use:   "get ID",
	Short: "Get a document by ID from a namespace",
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
}

func init() {
	getCmd.Flags().StringP("namespace", "n", "", "Namespace to query")
	getCmd.Flags().StringP("region", "r", "", "Override the region")
	_ = getCmd.MarkFlagRequired("namespace")
	rootCmd.AddCommand(getCmd)
}

func runGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")
	region, _ := cmd.Flags().GetString("region")
	ctx := context.Background()

	output.StatusPrint(fmt.Sprintf("\nQuerying document with ID: %s from namespace: %s\n", id, namespace))

	ns, err := client.GetNamespace(namespace, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	debug.Log("Query Parameters", map[string]any{
		"filters": []any{"id", "Eq", id},
		"top_k":   1,
	})

	result, err := ns.Query(ctx, turbopuffer.NamespaceQueryParams{
		Filters:           turbopuffer.NewFilterEq("id", id),
		TopK:              turbopuffer.Int(1),
		IncludeAttributes: turbopuffer.IncludeAttributesParam{Bool: turbopuffer.Bool(true)},
		Consistency: turbopuffer.NamespaceQueryParamsConsistency{
			Level: turbopuffer.NamespaceQueryParamsConsistencyLevelEventual,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	debug.Log("Query Response", result)

	if len(result.Rows) == 0 {
		fmt.Println("Document not found")
		os.Exit(1)
	}

	doc := result.Rows[0]

	if output.IsPlain() {
		b, _ := json.Marshal(doc)
		fmt.Println(string(b))
	} else {
		fmt.Println("Document:")
		b, _ := json.MarshalIndent(doc, "", "  ")
		fmt.Println(string(b))
	}

	if result.Performance.QueryExecutionMs > 0 {
		output.StatusPrint(fmt.Sprintf("\nQuery took %.2fms", float64(result.Performance.QueryExecutionMs)))
	}
	return nil
}
