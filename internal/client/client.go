package client

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/hev/puff/internal/config"
	"github.com/turbopuffer/turbopuffer-go"
	"github.com/turbopuffer/turbopuffer-go/option"
)

var (
	clientCache = make(map[string]*turbopuffer.Client)
	cacheMu     sync.Mutex
)

// GetClient returns a turbopuffer client for the given region.
// Priority: env vars > config file > defaults.
func GetClient(regionOverride string) (*turbopuffer.Client, error) {
	apiKey := os.Getenv("TURBOPUFFER_API_KEY")
	baseURL := os.Getenv("TURBOPUFFER_BASE_URL")
	region := regionOverride
	if region == "" {
		region = os.Getenv("TURBOPUFFER_REGION")
	}

	// Fall back to config file if env vars not set
	if apiKey == "" || (region == "" && baseURL == "") {
		if _, env, ok := config.GetActiveEnv(); ok {
			if apiKey == "" {
				apiKey = env.APIKey
			}
			if region == "" && baseURL == "" {
				region = env.Region
				if env.BaseURL != "" {
					baseURL = env.BaseURL
				}
			}
		}
	}

	if apiKey == "" {
		return nil, fmt.Errorf("TURBOPUFFER_API_KEY not set. Set the env var or run 'puff env add <name>'")
	}

	if region == "" {
		region = "aws-us-east-1"
	}

	cacheKey := baseURL
	if cacheKey == "" {
		cacheKey = region
	}

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if c, ok := clientCache[cacheKey]; ok {
		return c, nil
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
		// The SDK also reads TURBOPUFFER_REGION itself. A concrete endpoint
		// rejects that region unless its URL contains the REGION placeholder.
		if strings.Contains(baseURL, "REGION") {
			opts = append(opts, option.WithRegion(region))
		} else {
			opts = append(opts, option.WithRegion(""))
		}
	} else {
		opts = append(opts, option.WithRegion(region))
	}

	c := turbopuffer.NewClient(opts...)
	clientCache[cacheKey] = &c
	return &c, nil
}

// GetNamespace returns a namespace reference.
func GetNamespace(name, regionOverride string) (*turbopuffer.Namespace, error) {
	c, err := GetClient(regionOverride)
	if err != nil {
		return nil, err
	}
	ns := c.Namespace(name)
	return &ns, nil
}

// ClearCache clears the client cache.
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	clientCache = make(map[string]*turbopuffer.Client)
}

// ListNamespaces lists all namespaces, optionally filtered by prefix.
func ListNamespaces(ctx context.Context, regionOverride, prefix string) ([]string, error) {
	c, err := GetClient(regionOverride)
	if err != nil {
		return nil, err
	}

	params := turbopuffer.NamespacesParams{}
	if prefix != "" {
		params.Prefix = turbopuffer.String(prefix)
	}

	pager := c.NamespacesAutoPaging(ctx, params)
	var ids []string
	for pager.Next() {
		ns := pager.Current()
		ids = append(ids, ns.ID)
	}
	if err := pager.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}
