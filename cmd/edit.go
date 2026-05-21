package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/hev/tpuff/internal/client"
	"github.com/hev/tpuff/internal/debug"
	"github.com/spf13/cobra"
	"github.com/turbopuffer/turbopuffer-go"
)

var editCmd = &cobra.Command{
	Use:   "edit ID",
	Short: "Edit a document by ID from a namespace using vim",
	Args:  cobra.ExactArgs(1),
	RunE:  runEdit,
}

func init() {
	editCmd.Flags().StringP("namespace", "n", "", "Namespace to query")
	editCmd.Flags().StringP("region", "r", "", "Override the region")
	_ = editCmd.MarkFlagRequired("namespace")
	rootCmd.AddCommand(editCmd)
}

func runEdit(cmd *cobra.Command, args []string) error {
	id := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")
	region, _ := cmd.Flags().GetString("region")
	ctx := context.Background()

	fmt.Printf("\nFetching document with ID: %s from namespace: %s\n\n", id, namespace)

	ns, err := client.GetNamespace(namespace, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	debug.Log("Query Parameters", map[string]any{"filters": []any{"id", "Eq", id}, "top_k": 1})

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

	if len(result.Rows) == 0 {
		fmt.Println("Document not found")
		os.Exit(1)
	}

	doc := result.Rows[0]

	// Save vector and remove from edit view
	originalVector := doc["vector"]
	docWithoutVector := make(map[string]any)
	for k, v := range doc {
		if k != "vector" {
			docWithoutVector[k] = v
		}
	}

	originalContent, _ := json.MarshalIndent(docWithoutVector, "", "  ")

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "tpuff-edit-*.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp file: %s\n", err)
		os.Exit(1)
	}
	tmpPath := tmpFile.Name()
	_, _ = tmpFile.Write(originalContent)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	fmt.Println("Opening vim editor...")
	fmt.Println("Save and quit (:wq) to upsert changes, or quit without saving (:q!) to cancel.")
	fmt.Println()

	// Open vim
	vimCmd := exec.Command("vim", tmpPath)
	vimCmd.Stdin = os.Stdin
	vimCmd.Stdout = os.Stdout
	vimCmd.Stderr = os.Stderr
	if err := vimCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running vim: %s\n", err)
		os.Exit(1)
	}

	// Read edited content
	editedContent, err := os.ReadFile(tmpPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading edited file: %s\n", err)
		os.Exit(1)
	}

	if string(editedContent) == string(originalContent) {
		fmt.Println("\nNo changes made. Skipping upsert.")
		return nil
	}

	var editedDoc map[string]any
	if err := json.Unmarshal(editedContent, &editedDoc); err != nil {
		fmt.Fprintf(os.Stderr, "\nError: Invalid JSON format: %s\n", err)
		os.Exit(1)
	}

	// Restore vector and ID
	if originalVector != nil {
		editedDoc["vector"] = originalVector
	}
	editedDoc["id"] = id

	fmt.Println("\nUpserting document...")

	debug.Log("Write Parameters", map[string]any{"upsert_rows": []any{editedDoc}})

	_, err = ns.Write(ctx, turbopuffer.NamespaceWriteParams{
		UpsertRows: []turbopuffer.RowParam{turbopuffer.RowParam(editedDoc)},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	debug.Log("Write Response", "Success")
	fmt.Println("Document updated successfully")
	return nil
}
