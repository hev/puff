package metadata

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hev/tpuff/internal/client"
	"github.com/hev/tpuff/internal/debug"
	"github.com/hev/tpuff/internal/regions"
	"github.com/turbopuffer/turbopuffer-go"
)

// RecallData holds recall metrics for a namespace.
type RecallData struct {
	AvgRecall           float64
	AvgAnnCount         float64
	AvgExhaustiveCount  float64
}

// NamespaceMetadata holds metadata for a namespace.
type NamespaceMetadata struct {
	ApproxRowCount      int64
	ApproxLogicalBytes  int64
	Index               map[string]any
	UpdatedAt           time.Time
	CreatedAt           time.Time
	Schema              map[string]any
	Encryption          map[string]any
}

// NamespaceWithMetadata combines namespace ID with its metadata.
type NamespaceWithMetadata struct {
	NamespaceID string
	Metadata    *NamespaceMetadata
	Region      string
	Recall      *RecallData
}

// GetIndexStatus extracts index status from metadata.
func GetIndexStatus(m *NamespaceMetadata) string {
	if m == nil || m.Index == nil {
		return "up-to-date"
	}
	if s, ok := m.Index["status"].(string); ok {
		return s
	}
	return "up-to-date"
}

// GetUnindexedBytes gets unindexed bytes from metadata.
func GetUnindexedBytes(m *NamespaceMetadata) int64 {
	if m == nil || m.Index == nil {
		return 0
	}
	if s, ok := m.Index["status"].(string); ok && s == "up-to-date" {
		return 0
	}
	if b, ok := m.Index["unindexed_bytes"].(float64); ok {
		return int64(b)
	}
	if b, ok := m.Index["unindexed_bytes"].(int64); ok {
		return b
	}
	return 0
}

// GetEncryptionType extracts encryption type from metadata.
func GetEncryptionType(m *NamespaceMetadata) string {
	if m == nil || m.Encryption == nil {
		return "sse"
	}
	if _, ok := m.Encryption["cmek"]; ok {
		return "cmek"
	}
	return "sse"
}

// FetchRecallData fetches recall metrics for a namespace.
func FetchRecallData(ctx context.Context, namespaceID, region string) *RecallData {
	ns, err := client.GetNamespace(namespaceID, region)
	if err != nil {
		debug.Log(fmt.Sprintf("Failed to get namespace for recall: %s", namespaceID), err.Error())
		return nil
	}

	resp, err := ns.Recall(ctx, turbopuffer.NamespaceRecallParams{
		Num:  turbopuffer.Int(25),
		TopK: turbopuffer.Int(10),
	})
	if err != nil {
		debug.Log(fmt.Sprintf("Failed to fetch recall for %s", namespaceID), err.Error())
		return nil
	}

	return &RecallData{
		AvgRecall:          resp.AvgRecall,
		AvgAnnCount:        resp.AvgAnnCount,
		AvgExhaustiveCount: resp.AvgExhaustiveCount,
	}
}

// FetchNamespaceMetadata fetches metadata for a single namespace.
func FetchNamespaceMetadata(ctx context.Context, namespaceID, region string) *NamespaceMetadata {
	ns, err := client.GetNamespace(namespaceID, region)
	if err != nil {
		debug.Log(fmt.Sprintf("Failed to get namespace: %s", namespaceID), err.Error())
		return nil
	}

	md, err := ns.Metadata(ctx, turbopuffer.NamespaceMetadataParams{})
	if err != nil {
		debug.Log(fmt.Sprintf("Failed to fetch metadata for %s", namespaceID), err.Error())
		return nil
	}

	indexData := map[string]any{"status": "up-to-date"}
	if md.Index.Status != "" {
		indexData["status"] = md.Index.Status
		if md.Index.UnindexedBytes > 0 {
			indexData["unindexed_bytes"] = md.Index.UnindexedBytes
		}
	}

	schemaData := make(map[string]any)
	for k, v := range md.Schema {
		schemaData[k] = v
	}

	return &NamespaceMetadata{
		ApproxRowCount:     md.ApproxRowCount,
		ApproxLogicalBytes: md.ApproxLogicalBytes,
		UpdatedAt:          md.UpdatedAt,
		CreatedAt:          md.CreatedAt,
		Schema:             schemaData,
		Index:              indexData,
	}
}

// FetchNamespacesWithMetadata fetches all namespaces with their metadata.
func FetchNamespacesWithMetadata(ctx context.Context, allRegions bool, region string, includeRecall bool) []NamespaceWithMetadata {
	result := []NamespaceWithMetadata{}

	if allRegions {
		for _, r := range regions.TurbopufferRegions {
			client.ClearCache()
			nsIDs, err := client.ListNamespaces(ctx, r, "")
			if err != nil {
				debug.Log(fmt.Sprintf("Failed to query region %s", r), err.Error())
				continue
			}
			if len(nsIDs) > 0 {
				items := fetchMetadataParallel(ctx, nsIDs, r, includeRecall)
				result = append(result, items...)
			}
		}
	} else {
		nsIDs, err := client.ListNamespaces(ctx, region, "")
		if err != nil {
			return nil
		}
		if len(nsIDs) == 0 {
			return []NamespaceWithMetadata{}
		}
		result = fetchMetadataParallel(ctx, nsIDs, region, includeRecall)
	}

	return result
}

func fetchMetadataParallel(ctx context.Context, nsIDs []string, region string, includeRecall bool) []NamespaceWithMetadata {
	type metaResult struct {
		id       string
		metadata *NamespaceMetadata
	}
	type recallResult struct {
		id     string
		recall *RecallData
	}

	// Fetch metadata in parallel
	metaCh := make(chan metaResult, len(nsIDs))
	var wg sync.WaitGroup
	for _, id := range nsIDs {
		wg.Add(1)
		go func(nsID string) {
			defer wg.Done()
			md := FetchNamespaceMetadata(ctx, nsID, region)
			metaCh <- metaResult{id: nsID, metadata: md}
		}(id)
	}
	wg.Wait()
	close(metaCh)

	metaMap := make(map[string]*NamespaceMetadata)
	for r := range metaCh {
		metaMap[r.id] = r.metadata
	}

	// Fetch recall in parallel if requested
	recallMap := make(map[string]*RecallData)
	if includeRecall {
		recallCh := make(chan recallResult, len(nsIDs))
		for _, id := range nsIDs {
			wg.Add(1)
			go func(nsID string) {
				defer wg.Done()
				rd := FetchRecallData(ctx, nsID, region)
				recallCh <- recallResult{id: nsID, recall: rd}
			}(id)
		}
		wg.Wait()
		close(recallCh)
		for r := range recallCh {
			recallMap[r.id] = r.recall
		}
	}

	// Combine
	var items []NamespaceWithMetadata
	for _, id := range nsIDs {
		items = append(items, NamespaceWithMetadata{
			NamespaceID: id,
			Metadata:    metaMap[id],
			Region:      region,
			Recall:      recallMap[id],
		})
	}
	return items
}
