package searchapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	tp "github.com/turbopuffer/turbopuffer-go"
	"github.com/turbopuffer/turbopuffer-go/option"
)

const nativeSchema = `{"schema":{"body":{"type":"string","full_text_search":true,"embed":{"model":"voyage/voyage-4","attribute":"vector"}},"vector":{"type":"[1024]f32","ann":true},"active":{"type":"bool"}}}`

func TestNativeEmbedReachesRealSDKTransport(t *testing.T) {
	for _, tc := range []struct {
		name     string
		semantic *Semantic
		want     string
	}{
		{"schema model", nil, `["body","ANN",["Embed","a useful idea"]]`},
		{"explicit vector model", &Semantic{Field: "vector", Model: "voyage/voyage-4"}, `["vector","ANN",["Embed","a useful idea",{"model":"voyage/voyage-4"}]]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rank string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("missing host credential")
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/metadata") {
					_, _ = fmt.Fprint(w, nativeSchema)
					return
				}
				if r.URL.Path != "/v2/namespaces/notes/query" {
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
				raw, _ := io.ReadAll(r.Body)
				var q map[string]json.RawMessage
				if err := json.Unmarshal(raw, &q); err != nil {
					t.Error(err)
				}
				rank = string(q["rank_by"])
				if string(q["filters"]) != `["active","Eq",false]` {
					t.Errorf("lost filter: %s", q["filters"])
				}
				if strings.Contains(string(q["include_attributes"]), "vector") {
					t.Error("vector projected")
				}
				_, _ = fmt.Fprint(w, `{"rows":[{"id":"a","body":"hello"}]}`)
			}))
			defer upstream.Close()
			client := tp.NewClient(option.WithAPIKey("test-key"), option.WithBaseURL(upstream.URL))
			ns := client.Namespace("notes")
			s := fixtureHost(t, SDKBackend{Namespace: &ns})
			s.definition.Semantic = tc.semantic
			boot := request(t, s, "/api/bootstrap", `{}`)
			mustOK(t, boot)
			if !strings.Contains(boot.Body.String(), `"nativeEmbedding":true`) {
				t.Fatal("capability missing")
			}
			body := `{"query":"a useful idea","mode":"ANN","field":"body","limit":10,"filters":[{"field":"active","op":"Eq","value":false}]}`
			mustOK(t, request(t, s, "/api/query", body))
			if rank != tc.want {
				t.Fatalf("rank %s, want %s", rank, tc.want)
			}
			mustOK(t, request(t, s, "/api/query", strings.Replace(body, `"ANN"`, `"BM25"`, 1)))
			if rank != `["body","BM25","a useful idea"]` {
				t.Fatalf("BM25 changed: %s", rank)
			}
		})
	}
}

func TestSemanticBindingRejectsInvalidConfiguration(t *testing.T) {
	fields, err := parseSchema(nativeSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, semantic := range []*Semantic{{Field: "missing"}, {Field: "vector"}, {Field: "active", Model: "voyage/voyage-4"}, {Field: "body", Model: "wrong/model"}, {Field: "vector", Model: "https://external/model"}} {
		d := Definition{Version: 1, Semantic: semantic}
		if err := d.resolve(fields, ""); err == nil {
			t.Fatalf("accepted %+v", semantic)
		}
	}
	noIndex, err := parseSchema(strings.Replace(nativeSchema, `"ann":true`, `"ann":false`, 1))
	if err != nil {
		t.Fatal(err)
	}
	d := Definition{Version: 1}
	if err = d.resolve(noIndex, ""); err == nil {
		t.Fatal("accepted disabled ANN target")
	}
	d = Definition{Version: 1, Semantic: &Semantic{Field: ""}}
	if err = d.resolve(fields, ""); err != nil {
		t.Fatal(err)
	}
	if d.Semantic.Field != "" {
		t.Fatal("explicit disabled semantic binding overridden")
	}
}

func TestNativeEmbeddingWithoutFullTextAndNoClientModelOverride(t *testing.T) {
	b := &mockBackend{metadata: strings.Replace(nativeSchema, `"full_text_search":true,`, "", 1), reply: `{"rows":[]}`}
	s := fixtureHost(t, b)
	mustOK(t, request(t, s, "/api/query", `{"query":"hello","mode":"ANN","limit":10,"filters":[]}`))
	for _, body := range []string{
		`{"query":"hello","mode":"Auto","limit":10,"filters":[]}`,
		`{"query":"hello","mode":"BM25","field":"body","limit":10,"filters":[]}`,
		`{"query":"hello","mode":"ANN","model":"other/model","limit":10,"filters":[]}`,
		`{"query":"hello","mode":"ANN","after":"cursor","limit":10,"filters":[]}`,
	} {
		if w := request(t, s, "/api/query", body); w.Code == 200 {
			t.Fatalf("accepted %s", body)
		}
	}
	if len(b.calls) != 1 {
		t.Fatal("invalid requests reached upstream")
	}
	mustOK(t, request(t, s, "/api/query", `{"query":"","mode":"ANN","limit":10,"filters":[]}`))
	if !strings.Contains(b.calls[1], `"rank_by":["id","asc"]`) {
		t.Fatal("empty query embedded instead of browsing")
	}
}

func TestSemanticSchemaChangeRevalidatesBeforeQuery(t *testing.T) {
	b := &mockBackend{metadata: nativeSchema, reply: `{"rows":[]}`}
	s := fixtureHost(t, b)
	s.definition.Semantic = &Semantic{Field: "vector", Model: "voyage/voyage-4"}
	mustOK(t, request(t, s, "/api/bootstrap", `{}`))
	b.metadata = strings.Replace(nativeSchema, `"ann":true`, `"ann":false`, 1)
	if w := request(t, s, "/api/query", `{"query":"hello","mode":"ANN","limit":10,"filters":[]}`); w.Code == 200 {
		t.Fatal("stale binding accepted")
	}
	if len(b.calls) != 0 {
		t.Fatal("stale binding reached upstream")
	}
}

func TestNativeQueryCancellationKeepsContext(t *testing.T) {
	b := &mockBackend{metadata: nativeSchema, query: func(ctx context.Context, _ tp.NamespaceQueryParams) (string, error) { return "", ctx.Err() }}
	s := fixtureHost(t, b)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", s.origin+"/api/query", strings.NewReader(`{"query":"hello","mode":"ANN","limit":10,"filters":[]}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+s.token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("cancelled request succeeded")
	}
}

func TestEmbeddingMetadataNormalizesStringAndObject(t *testing.T) {
	a, _ := parseEmbedding(json.RawMessage(`"voyage/voyage-4"`), "string")
	b, _ := parseEmbedding(json.RawMessage(`{"model":"voyage/voyage-4"}`), "string")
	if !reflect.DeepEqual(a, b) {
		t.Fatal("declaration forms diverged")
	}
	for _, raw := range []string{`false`, `{}`, `42`, `""`} {
		if _, err := parseEmbedding(json.RawMessage(raw), "string"); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	// Unrelated multi-vector indexes do not prevent supported controls from loading.
	if _, err := parseSchema(`{"schema":{"multi":{"type":"[][128]f32","ann":{"late_interaction":true}}}}`); err != nil {
		t.Fatal(err)
	}
}
