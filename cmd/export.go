package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/hev/puff/internal/debug"
	"github.com/hev/puff/internal/metadata"
	"github.com/hev/puff/internal/metrics"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:     "export",
	Aliases: []string{"metrics"},
	Short:   "Run Prometheus exporter for Turbopuffer namespace metrics",
	RunE:    runExport,
}

func init() {
	exportCmd.Flags().IntP("port", "p", 9876, "HTTP server port")
	exportCmd.Flags().StringP("region", "r", "", "Query specific region")
	exportCmd.Flags().BoolP("all-regions", "A", false, "Query all Turbopuffer regions")
	exportCmd.Flags().IntP("interval", "i", 60, "Metric refresh interval in seconds")
	exportCmd.Flags().IntP("timeout", "t", 30, "API request timeout per region in seconds")
	exportCmd.Flags().Bool("include-recall", false, "Include recall estimation metrics")
	exportCmd.Flags().Int("recall-interval", 3600, "Recall estimation refresh interval in seconds")
	rootCmd.AddCommand(exportCmd)
}

type metricsState struct {
	mu               sync.RWMutex
	data             string
	lastUpdate       time.Time
	err              string
	recallData       map[string]*metadata.RecallData
	recallLastUpdate time.Time
}

var state = &metricsState{
	data:       "# Waiting for first scrape...\n",
	lastUpdate: time.Now(),
	recallData: make(map[string]*metadata.RecallData),
}

func runExport(cmd *cobra.Command, args []string) error {
	port, _ := cmd.Flags().GetInt("port")
	region, _ := cmd.Flags().GetString("region")
	allRegions, _ := cmd.Flags().GetBool("all-regions")
	interval, _ := cmd.Flags().GetInt("interval")
	includeRecall, _ := cmd.Flags().GetBool("include-recall")
	recallInterval, _ := cmd.Flags().GetInt("recall-interval")

	if allRegions && region != "" {
		fmt.Fprintln(os.Stderr, "Error: Cannot use both --all-regions and --region flags together")
		os.Exit(1)
	}

	// Initial fetch
	fmt.Println("Performing initial metrics fetch...")
	refreshMetrics(allRegions, region, includeRecall, recallInterval)

	// Background refresh
	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				refreshMetrics(allRegions, region, includeRecall, recallInterval)
			case <-stopCh:
				return
			}
		}
	}()

	// HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", handleMetrics)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/", handleRoot(port, interval, allRegions, region, includeRecall, recallInterval))

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down gracefully...")
		close(stopCh)
		_ = server.Close()
	}()

	fmt.Println("Turbopuffer Prometheus exporter running")
	fmt.Printf("  Port:           %d\n", port)
	fmt.Printf("  Refresh:        %ds\n", interval)
	if allRegions {
		fmt.Println("  Region mode:    All regions")
	} else if region != "" {
		fmt.Printf("  Region mode:    %s\n", region)
	} else {
		fmt.Println("  Region mode:    Default")
	}
	if includeRecall {
		fmt.Printf("  Recall:         enabled (refresh: %ds)\n", recallInterval)
	} else {
		fmt.Println("  Recall:         disabled (use --include-recall to enable)")
	}
	fmt.Printf("\n  Endpoints:\n")
	fmt.Printf("    http://localhost:%d/metrics\n", port)
	fmt.Printf("    http://localhost:%d/health\n", port)
	fmt.Printf("    http://localhost:%d/\n", port)
	fmt.Println("\n  Press Ctrl+C to stop")
	fmt.Println()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "Server error: %s\n", err)
		os.Exit(1)
	}
	return nil
}

func refreshMetrics(allRegions bool, region string, includeRecall bool, recallInterval int) {
	ctx := context.Background()
	startTime := time.Now()

	debug.Log("Starting metrics refresh", map[string]any{
		"all_regions":    allRegions,
		"region":         region,
		"include_recall": includeRecall,
	})

	// Check if we should refresh recall
	state.mu.RLock()
	timeSinceRecall := time.Since(state.recallLastUpdate)
	state.mu.RUnlock()
	shouldRefreshRecall := includeRecall && timeSinceRecall >= time.Duration(recallInterval)*time.Second

	namespaces := metadata.FetchNamespacesWithMetadata(ctx, allRegions, region, shouldRefreshRecall)

	debug.Log("Fetched namespaces", map[string]any{
		"count":            len(namespaces),
		"refreshed_recall": shouldRefreshRecall,
	})

	// Update recall cache
	state.mu.Lock()
	if shouldRefreshRecall {
		state.recallData = make(map[string]*metadata.RecallData)
		for _, ns := range namespaces {
			if ns.Recall != nil {
				state.recallData[ns.NamespaceID] = ns.Recall
			}
		}
		state.recallLastUpdate = time.Now()
	} else if includeRecall {
		// Merge cached recall
		for i, ns := range namespaces {
			if rd, ok := state.recallData[ns.NamespaceID]; ok {
				namespaces[i].Recall = rd
			}
		}
	}
	state.mu.Unlock()

	// Generate metrics
	promMetrics := generatePrometheusMetrics(namespaces)

	scrapeDuration := time.Since(startTime).Seconds()
	promMetrics = append(promMetrics,
		metrics.CreateSimpleGauge("turbopuffer_exporter_scrape_duration_seconds", "Time taken to fetch metrics", scrapeDuration),
		metrics.CreateSimpleGauge("turbopuffer_exporter_last_scrape_timestamp_seconds", "Unix timestamp of last scrape", float64(metrics.CurrentTimestamp())),
	)

	metricsText := metrics.FormatMetrics(promMetrics)

	state.mu.Lock()
	state.data = metricsText
	state.lastUpdate = time.Now()
	state.err = ""
	state.mu.Unlock()
}

func generatePrometheusMetrics(namespaces []metadata.NamespaceWithMetadata) []metrics.PrometheusMetric {
	rowsMetric := metrics.PrometheusMetric{Name: "turbopuffer_namespace_rows", Type: "gauge", Help: "Approximate number of rows in namespace"}
	bytesMetric := metrics.PrometheusMetric{Name: "turbopuffer_namespace_logical_bytes", Type: "gauge", Help: "Approximate logical storage size in bytes"}
	unindexedMetric := metrics.PrometheusMetric{Name: "turbopuffer_namespace_unindexed_bytes", Type: "gauge", Help: "Number of unindexed bytes"}
	recallMetric := metrics.PrometheusMetric{Name: "turbopuffer_namespace_recall", Type: "gauge", Help: "Average vector recall estimation (0-1 scale)"}
	infoMetric := metrics.PrometheusMetric{Name: "turbopuffer_namespace_info", Type: "gauge", Help: "Namespace information with labels"}

	for _, ns := range namespaces {
		if ns.Metadata == nil {
			continue
		}

		labels := map[string]string{
			"namespace":    ns.NamespaceID,
			"region":       ns.Region,
			"encryption":   metadata.GetEncryptionType(ns.Metadata),
			"index_status": metadata.GetIndexStatus(ns.Metadata),
		}

		rowsMetric.Values = append(rowsMetric.Values, metrics.MetricValue{Labels: labels, Value: float64(ns.Metadata.ApproxRowCount)})
		bytesMetric.Values = append(bytesMetric.Values, metrics.MetricValue{Labels: labels, Value: float64(ns.Metadata.ApproxLogicalBytes)})
		unindexedMetric.Values = append(unindexedMetric.Values, metrics.MetricValue{Labels: labels, Value: float64(metadata.GetUnindexedBytes(ns.Metadata))})

		if ns.Recall != nil {
			recallMetric.Values = append(recallMetric.Values, metrics.MetricValue{Labels: labels, Value: ns.Recall.AvgRecall})
		}

		infoLabels := make(map[string]string)
		for k, v := range labels {
			infoLabels[k] = v
		}
		infoLabels["updated_at"] = ns.Metadata.UpdatedAt.String()
		infoMetric.Values = append(infoMetric.Values, metrics.MetricValue{Labels: infoLabels, Value: 1})
	}

	var result []metrics.PrometheusMetric
	if len(rowsMetric.Values) > 0 {
		result = append(result, rowsMetric)
	}
	if len(bytesMetric.Values) > 0 {
		result = append(result, bytesMetric)
	}
	if len(unindexedMetric.Values) > 0 {
		result = append(result, unindexedMetric)
	}
	if len(recallMetric.Values) > 0 {
		result = append(result, recallMetric)
	}
	if len(infoMetric.Values) > 0 {
		result = append(result, infoMetric)
	}
	return result
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	data := state.data
	state.mu.RUnlock()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(data))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	health := map[string]any{
		"status":     "ok",
		"lastUpdate": state.lastUpdate.Format(time.RFC3339),
		"error":      state.err,
	}
	state.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(health)
}

func handleRoot(port, interval int, allRegions bool, region string, includeRecall bool, recallInterval int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		regionMode := "Default"
		if allRegions {
			regionMode = "All regions"
		} else if region != "" {
			regionMode = region
		}

		state.mu.RLock()
		lastUpdate := state.lastUpdate.Format(time.RFC3339)
		state.mu.RUnlock()

		recallInfo := "disabled"
		if includeRecall {
			recallInfo = fmt.Sprintf("enabled (refresh: %ds)", recallInterval)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Turbopuffer Prometheus Exporter</title>
<style>body{font-family:sans-serif;max-width:800px;margin:50px auto;padding:20px}
.info{background:#f5f5f5;padding:15px;border-radius:5px;margin:20px 0}
a{color:#0066cc}</style></head>
<body>
<h1>Turbopuffer Prometheus Exporter</h1>
<div class="info">
<p><strong>Status:</strong> Running</p>
<p><strong>Last Update:</strong> %s</p>
<p><strong>Refresh Interval:</strong> %ds</p>
<p><strong>Region Mode:</strong> %s</p>
<p><strong>Recall Metrics:</strong> %s</p>
</div>
<h2>Endpoints</h2>
<ul><li><a href="/metrics">/metrics</a></li><li><a href="/health">/health</a></li></ul>
<h2>Example Prometheus Configuration</h2>
<pre>scrape_configs:
  - job_name: 'turbopuffer'
    scrape_interval: %ds
    static_configs:
      - targets: ['localhost:%d']</pre>
</body></html>`, lastUpdate, interval, regionMode, recallInfo, interval, port)
	}
}
