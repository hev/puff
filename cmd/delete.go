package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hev/puff/internal/client"
	"github.com/hev/puff/internal/debug"
	"github.com/spf13/cobra"
	"github.com/turbopuffer/turbopuffer-go"
)

var deleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"rm"},
	Short:   "Delete namespace(s)",
	RunE:    runDelete,
}

func init() {
	deleteCmd.Flags().StringP("namespace", "n", "", "Namespace to delete")
	deleteCmd.Flags().Bool("all", false, "Delete all namespaces")
	deleteCmd.Flags().String("prefix", "", "Delete all namespaces starting with prefix")
	deleteCmd.Flags().StringP("region", "r", "", "Override the region")
	rootCmd.AddCommand(deleteCmd)
}

func runDelete(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	deleteAll, _ := cmd.Flags().GetBool("all")
	prefix, _ := cmd.Flags().GetString("prefix")
	region, _ := cmd.Flags().GetString("region")
	ctx := context.Background()

	optCount := 0
	if namespace != "" {
		optCount++
	}
	if deleteAll {
		optCount++
	}
	if prefix != "" {
		optCount++
	}

	if optCount == 0 {
		fmt.Fprintln(os.Stderr, "Error: You must specify either -n <namespace>, --all, or --prefix <prefix>")
		os.Exit(1)
	}
	if optCount > 1 {
		fmt.Fprintln(os.Stderr, "Error: Cannot use multiple deletion options together")
		os.Exit(1)
	}

	c, err := client.GetClient(region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	if namespace != "" {
		return deleteSingle(ctx, c, namespace)
	} else if deleteAll {
		return deleteAllNamespaces(ctx, c)
	} else {
		return deleteByPrefix(ctx, c, prefix)
	}
}

func deleteSingle(ctx context.Context, c *turbopuffer.Client, namespace string) error {
	fmt.Printf("\nYou are about to delete namespace: %s\n", namespace)
	fmt.Println("This action cannot be undone.")
	fmt.Println()

	if !promptYN("Are you sure? (y/n)") {
		fmt.Println("Deletion cancelled.")
		return nil
	}

	fmt.Printf("\nDeleting namespace %s...\n", namespace)
	ns := c.Namespace(namespace)
	debug.Log("Delete Parameters", map[string]any{"namespace": namespace})
	_, err := deleteNamespace(ctx, &ns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	debug.Log("Delete Response", "Success")
	fmt.Printf("Namespace %s deleted successfully!\n", namespace)
	return nil
}

func deleteAllNamespaces(ctx context.Context, c *turbopuffer.Client) error {
	fmt.Println("\nDANGER ZONE")
	fmt.Println("You are about to delete ALL namespaces!")
	fmt.Println("This will permanently destroy all your data.")
	fmt.Println()

	nsIDs, err := listAllNamespaceIDs(ctx, c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	if len(nsIDs) == 0 {
		fmt.Println("No namespaces found. Nothing to delete.")
		return nil
	}

	fmt.Printf("Found %d namespace(s):\n", len(nsIDs))
	for _, id := range nsIDs {
		fmt.Printf("  - %s\n", id)
	}

	fmt.Println()
	fmt.Println("To confirm, please type: yolo")
	fmt.Println()
	fmt.Print("> ")
	var answer string
	_, _ = fmt.Scanln(&answer)

	if answer != "yolo" {
		fmt.Println("\nWise choice! Your data lives to see another day.")
		return nil
	}

	fmt.Println("\nYOLO MODE ACTIVATED!")
	fmt.Println("Deleting all namespaces...")
	fmt.Println()

	successCount := 0
	failCount := 0
	for _, id := range nsIDs {
		debug.Log("Deleting namespace", map[string]any{"namespace": id})
		ns := c.Namespace(id)
		_, err := deleteNamespace(ctx, &ns)
		if err != nil {
			fmt.Printf("  Failed to delete: %s (%s)\n", id, err)
			failCount++
		} else {
			fmt.Printf("  Deleted: %s\n", id)
			successCount++
		}
	}

	fmt.Println("\nDeletion complete!")
	fmt.Printf("Successfully deleted: %d\n", successCount)
	if failCount > 0 {
		fmt.Printf("Failed: %d\n", failCount)
	}
	return nil
}

func deleteByPrefix(ctx context.Context, c *turbopuffer.Client, prefix string) error {
	fmt.Printf("\nSearching for namespaces with prefix: %s\n\n", prefix)

	nsIDs, err := listAllNamespaceIDs(ctx, c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	var matching []string
	for _, id := range nsIDs {
		if strings.HasPrefix(strings.ToLower(id), strings.ToLower(prefix)) {
			matching = append(matching, id)
		}
	}

	if len(matching) == 0 {
		fmt.Printf("No namespaces found with prefix \"%s\".\n", prefix)
		return nil
	}

	fmt.Printf("Found %d namespace(s) matching prefix \"%s\":\n", len(matching), prefix)
	for _, id := range matching {
		fmt.Printf("  - %s\n", id)
	}

	fmt.Println("\nWARNING: This will permanently delete these namespaces!")
	fmt.Printf("To confirm, please type the prefix: %s\n\n", prefix)
	fmt.Print("> ")
	var answer string
	_, _ = fmt.Scanln(&answer)

	if !strings.EqualFold(answer, prefix) {
		fmt.Println("\nDeletion cancelled.")
		return nil
	}

	fmt.Println("\nStarting deletion...")
	fmt.Println()

	successCount := 0
	failCount := 0
	for _, id := range matching {
		debug.Log("Deleting namespace", map[string]any{"namespace": id})
		ns := c.Namespace(id)
		_, err := deleteNamespace(ctx, &ns)
		if err != nil {
			fmt.Printf("  Failed to delete: %s (%s)\n", id, err)
			failCount++
		} else {
			fmt.Printf("  Deleted: %s\n", id)
			successCount++
		}
	}

	fmt.Println("\nDeletion complete!")
	fmt.Printf("Successfully deleted: %d\n", successCount)
	if failCount > 0 {
		fmt.Printf("Failed: %d\n", failCount)
	}
	return nil
}

func listAllNamespaceIDs(ctx context.Context, c *turbopuffer.Client) ([]string, error) {
	pager := c.NamespacesAutoPaging(ctx, turbopuffer.NamespacesParams{})
	var ids []string
	for pager.Next() {
		ids = append(ids, pager.Current().ID)
	}
	if err := pager.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func deleteNamespace(ctx context.Context, ns *turbopuffer.Namespace) (*turbopuffer.NamespaceDeleteAllResponse, error) {
	return ns.DeleteAll(ctx, turbopuffer.NamespaceDeleteAllParams{})
}

func promptYN(message string) bool {
	fmt.Printf("%s ", message)
	var answer string
	_, _ = fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
