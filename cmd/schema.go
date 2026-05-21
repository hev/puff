package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hev/tpuff/internal/client"
	"github.com/hev/tpuff/internal/output"
	"github.com/hev/tpuff/internal/schema"
	"github.com/spf13/cobra"
	"github.com/turbopuffer/turbopuffer-go"
	"github.com/turbopuffer/turbopuffer-go/packages/param"
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Manage namespace schemas",
}

var schemaGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Display the schema for a namespace",
	RunE:  runSchemaGet,
}

var schemaApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a schema from a JSON file to namespace(s)",
	RunE:  runSchemaApply,
}

var schemaCopyCmd = &cobra.Command{
	Use:   "copy",
	Short: "Copy schema from a source namespace to a new target namespace",
	RunE:  runSchemaCopy,
}

func init() {
	schemaGetCmd.Flags().StringP("namespace", "n", "", "Namespace to get schema from")
	schemaGetCmd.Flags().StringP("region", "r", "", "Override the region")
	schemaGetCmd.Flags().Bool("raw", false, "Output raw JSON without formatting")
	_ = schemaGetCmd.MarkFlagRequired("namespace")

	schemaApplyCmd.Flags().StringP("namespace", "n", "", "Target namespace to apply schema to")
	schemaApplyCmd.Flags().String("prefix", "", "Apply to all namespaces matching this prefix")
	schemaApplyCmd.Flags().Bool("all", false, "Apply to all namespaces")
	schemaApplyCmd.Flags().StringP("file", "f", "", "JSON file containing schema definition")
	schemaApplyCmd.Flags().StringP("region", "r", "", "Override the region")
	schemaApplyCmd.Flags().Bool("dry-run", false, "Show diff only, don't apply changes")
	schemaApplyCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	schemaApplyCmd.Flags().Bool("continue-on-error", false, "Continue applying when conflicts occur (batch mode)")
	_ = schemaApplyCmd.MarkFlagRequired("file")

	schemaCopyCmd.Flags().StringP("namespace", "n", "", "Source namespace to copy schema from")
	schemaCopyCmd.Flags().String("to", "", "Target namespace name")
	schemaCopyCmd.Flags().StringP("region", "r", "", "Override the region")
	schemaCopyCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	_ = schemaCopyCmd.MarkFlagRequired("namespace")
	_ = schemaCopyCmd.MarkFlagRequired("to")

	schemaCmd.AddCommand(schemaGetCmd)
	schemaCmd.AddCommand(schemaApplyCmd)
	schemaCmd.AddCommand(schemaCopyCmd)
	rootCmd.AddCommand(schemaCmd)
}

func runSchemaGet(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	region, _ := cmd.Flags().GetString("region")
	raw, _ := cmd.Flags().GetBool("raw")
	ctx := context.Background()

	useRaw := raw || output.IsPlain()

	ns, err := client.GetNamespace(namespace, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	md, err := ns.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
	if err != nil {
		if useRaw {
			fmt.Fprintf(os.Stderr, `{"error": "%s"}`+"\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		}
		os.Exit(1)
	}

	if len(md.Schema) == 0 {
		if useRaw {
			fmt.Println("{}")
		} else {
			fmt.Printf("No schema found for namespace: %s\n", namespace)
		}
		return nil
	}

	// Convert schema to serializable format
	schemaDict := make(map[string]any)
	for k, v := range md.Schema {
		schemaDict[k] = v
	}

	if useRaw {
		b, _ := json.Marshal(schemaDict)
		fmt.Println(string(b))
	} else {
		fmt.Printf("\nSchema for namespace: %s\n\n", namespace)
		b, _ := json.MarshalIndent(schemaDict, "", "  ")
		fmt.Println(string(b))
	}
	return nil
}

func runSchemaApply(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	prefix, _ := cmd.Flags().GetString("prefix")
	applyAll, _ := cmd.Flags().GetBool("all")
	schemaFile, _ := cmd.Flags().GetString("file")
	region, _ := cmd.Flags().GetString("region")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	continueOnError, _ := cmd.Flags().GetBool("continue-on-error")
	ctx := context.Background()

	modeCount := 0
	if namespace != "" {
		modeCount++
	}
	if prefix != "" {
		modeCount++
	}
	if applyAll {
		modeCount++
	}

	if modeCount > 1 {
		fmt.Fprintln(os.Stderr, "Error: Cannot use more than one of --namespace, --prefix, and --all")
		os.Exit(1)
	}
	if modeCount == 0 {
		fmt.Fprintln(os.Stderr, "Error: Must specify one of --namespace, --prefix, or --all")
		os.Exit(1)
	}

	newSchema, err := schema.LoadSchemaFile(schemaFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	if len(newSchema) == 0 {
		fmt.Println("Schema file is empty, nothing to apply")
		return nil
	}

	if namespace != "" {
		applySchemaToSingle(ctx, namespace, newSchema, region, dryRun, yes)
	} else {
		var namespaces []string
		if prefix != "" {
			namespaces, err = client.ListNamespaces(ctx, region, prefix)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				os.Exit(1)
			}
			if len(namespaces) == 0 {
				fmt.Printf("No namespaces found matching prefix: %s\n", prefix)
				return nil
			}
			output.StatusPrint(fmt.Sprintf("Found %d namespace(s) matching prefix '%s'", len(namespaces), prefix))
		} else {
			namespaces, err = client.ListNamespaces(ctx, region, "")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				os.Exit(1)
			}
			if len(namespaces) == 0 {
				fmt.Println("No namespaces found")
				return nil
			}
			output.StatusPrint(fmt.Sprintf("Found %d namespace(s)", len(namespaces)))
		}
		applySchemaToMultiple(ctx, namespaces, newSchema, region, dryRun, yes, continueOnError)
	}
	return nil
}

func getCurrentSchema(ctx context.Context, ns *turbopuffer.Namespace) map[string]any {
	md, err := ns.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
	if err != nil {
		return nil
	}
	if len(md.Schema) == 0 {
		return nil
	}
	// Marshal SDK structs to JSON, then unmarshal back to map[string]any
	// so they're comparable to schemas loaded from JSON files.
	b, err := json.Marshal(md.Schema)
	if err != nil {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		return nil
	}
	return result
}

func displaySchemaDiff(diff schema.SchemaDiff, namespace string) {
	fmt.Printf("\nSchema changes for namespace: %s\n\n", namespace)

	if !diff.HasChanges() && len(diff.Unchanged) == 0 {
		fmt.Println("No schema attributes")
		return
	}

	allAttrs := make(map[string]bool)
	for k := range diff.Unchanged {
		allAttrs[k] = true
	}
	for k := range diff.Additions {
		allAttrs[k] = true
	}
	for k := range diff.Modifications {
		allAttrs[k] = true
	}
	for k := range diff.Conflicts {
		allAttrs[k] = true
	}

	sorted := make([]string, 0, len(allAttrs))
	for k := range allAttrs {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	for _, attr := range sorted {
		if _, ok := diff.Unchanged[attr]; ok {
			fmt.Printf("  %s\n", attr)
		} else if v, ok := diff.Additions[attr]; ok {
			fmt.Printf("+ %s: %s  (new)\n", attr, v)
		} else if m, ok := diff.Modifications[attr]; ok {
			fmt.Printf("~ %s (%s):\n", attr, m.BaseType)
			for _, c := range m.Changes {
				fmt.Printf("    %s: %s -> %s\n", c.Key, c.OldValue, c.NewValue)
			}
		} else if c, ok := diff.Conflicts[attr]; ok {
			fmt.Printf("! %s: %s -> %s  (type change not allowed)\n", attr, c[0], c[1])
		}
	}
	fmt.Println()
}

func applySchemaToSingle(ctx context.Context, namespace string, newSchema map[string]any, region string, dryRun, yes bool) {
	ns, err := client.GetNamespace(namespace, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	currentSchema := getCurrentSchema(ctx, ns)
	diff := schema.ComputeSchemaDiff(currentSchema, newSchema)
	displaySchemaDiff(diff, namespace)

	if diff.HasConflicts() {
		fmt.Fprintln(os.Stderr, "Error: Cannot apply schema with type conflicts.")
		fmt.Fprintln(os.Stderr, "Changing an existing attribute's type is not allowed.")
		os.Exit(1)
	}

	if !diff.HasChanges() {
		fmt.Println("Schema is already up to date, no changes needed.")
		return
	}

	if dryRun {
		fmt.Println("Dry run mode - no changes applied")
		return
	}

	if !yes {
		if !confirmPrompt("Apply these schema changes?") {
			fmt.Println("Aborted")
			return
		}
	}

	fmt.Printf("Applying schema to %s...\n", namespace)
	writeSchema(ctx, ns, newSchema)
	fmt.Printf("Successfully applied schema to %s\n", namespace)
}

type batchApplyResult struct {
	namespace     string
	success       bool
	additions     int
	modifications int
	conflicts     int
	err           string
}

func applySchemaToMultiple(ctx context.Context, namespaces []string, newSchema map[string]any, region string, dryRun, yes, continueOnError bool) {
	var results []batchApplyResult
	hasAnyConflicts := false
	hasAnyChanges := false

	fmt.Printf("\nAnalyzing schema for %d namespace(s)...\n\n", len(namespaces))

	for _, nsName := range namespaces {
		ns, err := client.GetNamespace(nsName, region)
		if err != nil {
			results = append(results, batchApplyResult{namespace: nsName, err: err.Error()})
			continue
		}

		currentSchema := getCurrentSchema(ctx, ns)
		diff := schema.ComputeSchemaDiff(currentSchema, newSchema)

		r := batchApplyResult{
			namespace:     nsName,
			additions:     len(diff.Additions),
			modifications: len(diff.Modifications),
			conflicts:     len(diff.Conflicts),
		}

		if diff.HasConflicts() {
			hasAnyConflicts = true
		}
		if diff.HasChanges() {
			hasAnyChanges = true
		}
		results = append(results, r)
	}

	// Display summary
	fmt.Printf("Schema changes for %d namespace(s):\n\n", len(namespaces))
	displayBatchSummary(results, true)

	if hasAnyConflicts {
		if continueOnError {
			fmt.Println("\nWarning: Some namespaces have type conflicts and will be skipped.")
		} else {
			fmt.Fprintln(os.Stderr, "\nError: Some namespaces have type conflicts.")
			fmt.Fprintln(os.Stderr, "Changing an existing attribute's type is not allowed.")
			fmt.Fprintln(os.Stderr, "Fix conflicts or use --continue-on-error to skip them.")
			os.Exit(1)
		}
	}

	if !hasAnyChanges {
		fmt.Println("\nAll namespaces are already up to date, no changes needed.")
		return
	}

	if dryRun {
		fmt.Println("\nDry run mode - no changes applied")
		return
	}

	// Find namespaces to update
	var toUpdate []int
	for i, r := range results {
		if (r.additions > 0 || r.modifications > 0) && r.conflicts == 0 && r.err == "" {
			toUpdate = append(toUpdate, i)
		}
	}

	if len(toUpdate) == 0 {
		fmt.Println("\nNo namespaces need updates.")
		return
	}

	if !yes {
		if !confirmPrompt(fmt.Sprintf("\nApply schema to %d namespace(s)?", len(toUpdate))) {
			fmt.Println("Aborted")
			return
		}
	}

	fmt.Printf("\nApplying schema to %d namespace(s)...\n\n", len(toUpdate))

	successCount := 0
	failCount := 0

	for _, i := range toUpdate {
		ns, err := client.GetNamespace(results[i].namespace, region)
		if err != nil {
			results[i].err = err.Error()
			failCount++
			continue
		}

		err = writeSchemaErr(ctx, ns, newSchema)
		if err != nil {
			results[i].err = err.Error()
			failCount++
		} else {
			results[i].success = true
			successCount++
		}
	}

	fmt.Println("Results:")
	fmt.Println()
	displayBatchSummary(results, false)

	if failCount == 0 {
		fmt.Printf("\nSuccessfully applied schema to %d namespace(s)\n", successCount)
	} else {
		fmt.Printf("\nApplied schema to %d namespace(s), %d failed\n", successCount, failCount)
		os.Exit(1)
	}
}

func displayBatchSummary(results []batchApplyResult, dryRun bool) {
	headers := []string{"Namespace", "Changes", "Status"}
	var rows [][]string

	for _, r := range results {
		var changes, status string
		if r.conflicts > 0 {
			changes = fmt.Sprintf("+%d attributes (%d conflict(s))", r.additions, r.conflicts)
			status = "blocked"
		} else if r.err != "" {
			changes = "N/A"
			status = fmt.Sprintf("error: %s", r.err)
		} else if r.additions == 0 && r.modifications == 0 {
			changes = "no changes"
			if dryRun {
				status = "would skip"
			} else {
				status = "up-to-date"
			}
		} else {
			var parts []string
			if r.additions > 0 {
				parts = append(parts, fmt.Sprintf("+%d new", r.additions))
			}
			if r.modifications > 0 {
				parts = append(parts, fmt.Sprintf("~%d modified", r.modifications))
			}
			changes = strings.Join(parts, ", ")
			if dryRun {
				status = "would apply"
			} else if r.success {
				status = "applied"
			} else {
				status = "failed"
			}
		}
		rows = append(rows, []string{r.namespace, changes, status})
	}

	output.PrintTable(headers, rows)
}

func writeSchema(ctx context.Context, ns *turbopuffer.Namespace, s map[string]any) {
	err := writeSchemaErr(ctx, ns, s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error applying schema: %s\n", err)
		os.Exit(1)
	}
}

func writeSchemaErr(ctx context.Context, ns *turbopuffer.Namespace, s map[string]any) error {
	schemaParam := make(map[string]turbopuffer.AttributeSchemaConfigParam)
	for k, v := range s {
		schemaParam[k] = toAttributeSchemaParam(v)
	}

	row := turbopuffer.RowParam{"id": "__schema_placeholder__"}

	// If schema has a vector field, include a zero vector to satisfy the constraint.
	for attrName, attrType := range s {
		baseType := schema.ExtractBaseType(attrType)
		if schema.VectorTypePattern.MatchString(baseType) {
			dims := schema.ParseVectorDims(baseType)
			if dims > 0 {
				vec := make([]float32, dims)
				row[attrName] = vec
			}
		}
	}

	_, err := ns.Write(ctx, turbopuffer.NamespaceWriteParams{
		UpsertRows: []turbopuffer.RowParam{row},
		Schema:     schemaParam,
	})
	return err
}

func toAttributeSchemaParam(v any) turbopuffer.AttributeSchemaConfigParam {
	switch val := v.(type) {
	case string:
		return turbopuffer.AttributeSchemaConfigParam{
			Type: val,
		}
	case map[string]any:
		p := turbopuffer.AttributeSchemaConfigParam{}
		if t, ok := val["type"].(string); ok {
			p.Type = t
		}
		if fts, ok := val["full_text_search"]; ok {
			switch ftsVal := fts.(type) {
			case bool:
				if ftsVal {
					p.FullTextSearch = &turbopuffer.FullTextSearchConfigParam{}
				}
			case map[string]any:
				ftsParam := &turbopuffer.FullTextSearchConfigParam{}
				if v, ok := ftsVal["ascii_folding"].(bool); ok {
					ftsParam.AsciiFolding = turbopuffer.Bool(v)
				}
				if v, ok := ftsVal["b"].(float64); ok && v != 0 {
					ftsParam.B = turbopuffer.Float(v)
				}
				if v, ok := ftsVal["case_sensitive"].(bool); ok {
					ftsParam.CaseSensitive = turbopuffer.Bool(v)
				}
				if v, ok := ftsVal["k1"].(float64); ok && v != 0 {
					ftsParam.K1 = turbopuffer.Float(v)
				}
				if v, ok := ftsVal["remove_stopwords"].(bool); ok {
					ftsParam.RemoveStopwords = turbopuffer.Bool(v)
				}
				if v, ok := ftsVal["stemming"].(bool); ok {
					ftsParam.Stemming = turbopuffer.Bool(v)
				}
				if v, ok := ftsVal["language"].(string); ok && v != "" {
					ftsParam.Language = turbopuffer.Language(v)
				}
				if v, ok := ftsVal["max_token_length"].(float64); ok && v != 0 {
					ftsParam.MaxTokenLength = turbopuffer.Int(int64(v))
				}
				if v, ok := ftsVal["tokenizer"].(string); ok && v != "" {
					ftsParam.Tokenizer = turbopuffer.Tokenizer(v)
				}
				// Only set if there's actual config (not all zeroes)
				hasConfig := ftsParam.B.Valid() || ftsParam.K1.Valid() ||
					ftsParam.Language != "" || ftsParam.Tokenizer != "" ||
					ftsParam.MaxTokenLength.Valid()
				if hasConfig {
					p.FullTextSearch = ftsParam
				}
			}
		}
		if f, ok := val["filterable"].(bool); ok {
			p.Filterable = turbopuffer.Bool(f)
		}
		if ann, ok := val["ann"]; ok {
			switch a := ann.(type) {
			case bool:
				if a {
					p.Ann = param.Override[turbopuffer.AttributeSchemaConfigAnnParam](true)
				}
			case map[string]any:
				if dm, ok := a["distance_metric"].(string); ok && dm != "" {
					p.Ann = turbopuffer.AttributeSchemaConfigAnnParam{
						DistanceMetric: turbopuffer.DistanceMetric(dm),
					}
				}
			}
		}
		if g, ok := val["glob"].(bool); ok {
			p.Glob = turbopuffer.Bool(g)
		}
		if r, ok := val["regex"].(bool); ok {
			p.Regex = turbopuffer.Bool(r)
		}
		return p
	default:
		return turbopuffer.AttributeSchemaConfigParam{
			Type: fmt.Sprintf("%v", v),
		}
	}
}

func runSchemaCopy(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	target, _ := cmd.Flags().GetString("to")
	region, _ := cmd.Flags().GetString("region")
	yes, _ := cmd.Flags().GetBool("yes")
	ctx := context.Background()

	// Get source schema
	sourceNs, err := client.GetNamespace(namespace, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	sourceSchema := getCurrentSchema(ctx, sourceNs)
	if sourceSchema == nil {
		fmt.Fprintf(os.Stderr, "Error: Source namespace '%s' has no schema or does not exist\n", namespace)
		os.Exit(1)
	}

	// Check target
	targetNs, err := client.GetNamespace(target, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	targetMd, err := targetNs.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
	if err == nil && targetMd.ApproxRowCount > 0 {
		fmt.Fprintf(os.Stderr, "Error: Target namespace '%s' already has %d row(s)\n", target, targetMd.ApproxRowCount)
		fmt.Fprintln(os.Stderr, "Target namespace must be empty or non-existent")
		os.Exit(1)
	}

	// Display
	fmt.Printf("\nCopying schema from: %s\n", namespace)
	fmt.Printf("Creating namespace:  %s\n", target)
	fmt.Println("\nSchema:")
	keys := make([]string, 0, len(sourceSchema))
	for k := range sourceSchema {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s: %s\n", k, schema.SchemaTypeForDisplay(sourceSchema[k]))
	}
	fmt.Println("\nNote: A placeholder row will be created to initialize the namespace.")

	if !yes {
		if !confirmPrompt("\nCopy schema to target namespace?") {
			fmt.Println("Aborted")
			return nil
		}
	}

	fmt.Printf("\nCreating namespace %s with schema...\n", target)
	writeSchema(ctx, targetNs, sourceSchema)
	fmt.Printf("Successfully created namespace '%s' with schema from '%s'\n", target, namespace)
	return nil
}

func confirmPrompt(message string) bool {
	fmt.Printf("%s [y/N]: ", message)
	var answer string
	_, _ = fmt.Scanln(&answer)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
}
