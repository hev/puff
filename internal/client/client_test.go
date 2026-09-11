package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	tp "github.com/turbopuffer/turbopuffer-go"
)

func TestCustomEndpointOverridesSDKRegionEnvironment(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/namespaces/notes/metadata" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"schema":{"body":{"type":"string"}}}`)
	}))
	defer upstream.Close()
	t.Setenv("TURBOPUFFER_API_KEY", "fixture-key")
	t.Setenv("TURBOPUFFER_BASE_URL", upstream.URL)
	t.Setenv("TURBOPUFFER_REGION", "aws-us-east-1")
	ClearCache()
	t.Cleanup(ClearCache)
	ns, err := GetNamespace("notes", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ns.Metadata(context.Background(), tp.NamespaceMetadataParams{}); err != nil {
		t.Fatal(err)
	}
}
