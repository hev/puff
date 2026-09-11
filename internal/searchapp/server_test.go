package searchapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	tp "github.com/turbopuffer/turbopuffer-go"
	"github.com/turbopuffer/turbopuffer-go/option"
)

const schemaFixture = `{"schema":{"body":{"type":"string","full_text_search":true},"category":{"type":"string"},"active":{"type":"bool"},"size":{"type":"uint"},"date":{"type":"datetime"},"vector":{"type":"[3]f32","ann":true}}}`

type mockBackend struct {
	metadata string
	reply    string
	query    func(context.Context, tp.NamespaceQueryParams) (string, error)
	calls    []string
	mu       sync.Mutex
}

func (b *mockBackend) Metadata(context.Context) (string, error) { return b.metadata, nil }
func (b *mockBackend) Query(ctx context.Context, q tp.NamespaceQueryParams) (string, error) {
	raw, _ := json.Marshal(q)
	b.mu.Lock()
	b.calls = append(b.calls, string(raw))
	b.mu.Unlock()
	if b.query != nil {
		return b.query(ctx, q)
	}
	return b.reply, nil
}
func fixtureHost(t *testing.T, b Backend) *Server {
	t.Helper()
	s, err := New(b, fstest.MapFS{"index.html": {Data: []byte("<p>app</p>")}}, "notes", Definition{Version: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	s.Bind("127.0.0.1:8383")
	return s
}
func request(t *testing.T, s *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", s.origin+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+s.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", s.origin)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func mustOK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
}

func TestTypedFiltersReachSDKAndCredentialsStayServerSide(t *testing.T) {
	var requests []map[string]any
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("SDK credential missing")
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/metadata") {
			_, _ = fmt.Fprint(w, schemaFixture)
			return
		}
		if r.URL.Path != "/v2/namespaces/notes/query" {
			t.Errorf("wrong namespace path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var q map[string]any
		if err := decodeLoose(string(raw), &q); err != nil {
			t.Error(err)
		}
		mu.Lock()
		requests = append(requests, q)
		mu.Unlock()
		_, _ = fmt.Fprint(w, `{"rows":[{"id":18446744073709551615,"body":"hello","size":18446744073709551615,"vector":[1,2,3],"unrequested":"hidden"}]}`)
	}))
	defer upstream.Close()
	client := tp.NewClient(option.WithAPIKey("fixture-key"), option.WithBaseURL(upstream.URL))
	ns := client.Namespace("notes")
	s := fixtureHost(t, SDKBackend{Namespace: &ns})
	body := `{"query":"hello","field":"body","limit":10,"filters":[{"field":"active","op":"Eq","value":false},{"field":"size","op":"Gte","value":"18446744073709551614"},{"field":"category","op":"In","value":[""," a "]},{"field":"date","op":"Lt","value":"2026-09-11T00:00:00Z"}]}`
	w := request(t, s, "/api/query", body)
	mustOK(t, w)
	if strings.Contains(w.Body.String(), "fixture-key") || strings.Contains(w.Body.String(), "vector") || strings.Contains(w.Body.String(), "unrequested") {
		t.Fatal("response leaked excluded data")
	}
	if !strings.Contains(w.Body.String(), `"id":"18446744073709551615"`) {
		t.Fatal("uint64 precision lost", w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("queries = %d", len(requests))
	}
	raw, _ := json.Marshal(requests[0])
	for _, want := range []string{`"rank_by":["body","BM25","hello"]`, `["active","Eq",false]`, `18446744073709551614`, `["category","In",[""," a "]]`, `["date","NotEq",null]`} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
}

func TestBrowseCursorPreservesIDAndBindsRequest(t *testing.T) {
	for _, id := range []string{`18446744073709551615`, `"opaque/id"`} {
		t.Run(id, func(t *testing.T) {
			b := &mockBackend{metadata: schemaFixture, reply: `{"rows":[{"id":` + id + `,"body":"result"}]}`}
			s := fixtureHost(t, b)
			w := request(t, s, "/api/query", `{"query":"","field":"body","limit":1,"filters":[]}`)
			mustOK(t, w)
			var result struct {
				After string `json:"after"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.After == "" {
				t.Fatal("no browse continuation")
			}
			body := fmt.Sprintf(`{"query":"","field":"body","limit":1,"filters":[],"after":%q}`, result.After)
			mustOK(t, request(t, s, "/api/query", body))
			if !strings.Contains(b.calls[1], `["id","Gt",`+id+`]`) {
				t.Fatalf("ID changed: %s", b.calls[1])
			}
			bad := strings.Replace(body, `"limit":1`, `"limit":2`, 1)
			if request(t, s, "/api/query", bad).Code != 400 {
				t.Fatal("accepted cursor for changed query")
			}
			if len(b.calls) != 2 {
				t.Fatal("invalid cursor reached upstream")
			}
		})
	}
}

func TestValuesAreBoundedScopedAndCached(t *testing.T) {
	b := &mockBackend{metadata: schemaFixture, reply: `{"aggregation_groups":[{"category":"a","count":2},{"category":"b","count":3}]}`}
	s := fixtureHost(t, b)
	body := `{"query":"not part of facet scope","field":"body","facet":"category","limit":2,"filters":[{"field":"category","op":"In","value":["a"]},{"field":"active","op":"Eq","value":false}]}`
	w := request(t, s, "/api/values", body)
	mustOK(t, w)
	mustOK(t, request(t, s, "/api/values", body))
	if !strings.Contains(w.Body.String(), `"truncated":true`) {
		t.Fatal("capped listing not marked")
	}
	if len(b.calls) != 1 {
		t.Fatal("identical facet query not cached")
	}
	raw := b.calls[0]
	for _, want := range []string{`"group_by":["category"]`, `"aggregate_by":{"count":["Count"]}`, `"filters":["active","Eq",false]`} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
	if strings.Contains(raw, "rank_by") || strings.Contains(raw, `"In"`) {
		t.Fatal("facet inherited ranking or own filter")
	}
}

func TestOriginSessionAndRequestValidation(t *testing.T) {
	b := &mockBackend{metadata: schemaFixture, reply: `{"rows":[]}`}
	s := fixtureHost(t, b)
	for _, tc := range []struct {
		name, host, origin, token string
		want                      int
	}{
		{"wrong host", "evil.example", "", s.token, 403}, {"cross origin", "127.0.0.1:8383", "https://evil.example", s.token, 403}, {"no session", "127.0.0.1:8383", s.origin, "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", s.origin+"/api/bootstrap", strings.NewReader(`{}`))
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Authorization", "Bearer "+tc.token)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("code %d", w.Code)
			}
		})
	}
	for _, body := range []string{
		`{"namespace":"other"}`, `{} {}`, `{"limit":101}`, `{"query":"x","field":"category","limit":10}`,
		`{"limit":10,"filters":[{"field":"vector","op":"Eq","value":0}]}`,
		`{"limit":10,"filters":[{"field":"active","op":"Eq","value":"false"}]}`,
		`{"limit":10,"filters":[{"field":"size","op":"Gte","value":"18446744073709551616"}]}`,
	} {
		if w := request(t, s, "/api/query", body); w.Code != 400 {
			t.Errorf("accepted %s: %d", body, w.Code)
		}
	}
	if len(b.calls) != 0 {
		t.Fatal("invalid request reached upstream")
	}
	r := httptest.NewRequest("GET", s.origin+"/", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	mustOK(t, w)
	if strings.Contains(w.Body.String(), s.token) {
		t.Fatal("unauthenticated HTML leaked session")
	}
	r = httptest.NewRequest("GET", s.origin+"/.env", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("served arbitrary file")
	}
}

func TestCancellationAndRedactedUpstreamErrors(t *testing.T) {
	entered := make(chan struct{})
	b := &mockBackend{metadata: schemaFixture, query: func(ctx context.Context, _ tp.NamespaceQueryParams) (string, error) {
		close(entered)
		<-ctx.Done()
		return "", ctx.Err()
	}}
	s := fixtureHost(t, b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("POST", s.origin+"/api/query", strings.NewReader(`{"limit":10}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+s.token)
	done := make(chan struct{})
	go func() { s.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	<-entered
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not reach backend")
	}
	b.query = func(context.Context, tp.NamespaceQueryParams) (string, error) {
		return "", fmt.Errorf("fixture-secret upstream-url response-body")
	}
	w := request(t, s, "/api/query", `{"limit":10}`)
	if w.Code != 502 || strings.Contains(w.Body.String(), "fixture-secret") {
		t.Fatal("upstream error leaked")
	}
}

func TestAssetManifestAndEmptySchema(t *testing.T) {
	data := []byte("<p>app</p>")
	sum := sha256.Sum256(data)
	manifest, _ := json.Marshal(Manifest{Protocol: 1, Version: "test", Files: map[string]string{"index.html": hex.EncodeToString(sum[:])}})
	source := fstest.MapFS{"manifest.json": {Data: manifest}, "index.html": {Data: data}, ".env": {Data: []byte("secret")}}
	assets, err := VerifyAssets(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = assets.Open(".env"); err == nil {
		t.Fatal("extra file exposed")
	}
	source["index.html"].Data = []byte("changed")
	if _, err = VerifyAssets(source); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	b := &mockBackend{metadata: `{"schema":{}}`, reply: `{"rows":[]}`}
	s := fixtureHost(t, b)
	w := request(t, s, "/api/bootstrap", `{}`)
	mustOK(t, w)
	if !strings.Contains(w.Body.String(), `"fields":[]`) || !strings.Contains(w.Body.String(), `"filterFields":[]`) {
		t.Fatal("empty schema not represented as lists", w.Body.String())
	}
}

func TestPresentationDefaultsAndQueryFieldSelection(t *testing.T) {
	fields, err := parseSchema(`{"schema":{"summary":{"type":"string","full_text_search":true},"body":{"type":"string","full_text_search":true},"active":{"type":"bool"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, preferred, explicit, want string }{
		{"automatic", "", "", "body"},
		{"configured preference", "summary", "", "summary"},
		{"explicit override", "body", "summary", "summary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Definition{Version: 1, QueryField: tc.explicit}
			if err := d.resolve(fields, tc.preferred); err != nil {
				t.Fatal(err)
			}
			if d.QueryField != tc.want || d.Limit != 25 || d.Layout != "list" || d.Palette != "turbopuffer" || d.Appearance != "dark" {
				t.Fatalf("unexpected defaults: %+v", d)
			}
			if len(d.FilterFields) != 1 || d.FilterFields[0] != "active" {
				t.Fatalf("supported filters missing: %+v", d.FilterFields)
			}
		})
	}
	for _, limit := range []int{-1, 101} {
		d := Definition{Version: 1, Limit: limit}
		if err := d.resolve(fields, ""); err == nil {
			t.Fatalf("accepted limit %d", limit)
		}
	}
	d := Definition{Version: 1, Limit: 7, Palette: "hev", Appearance: "light", Layout: "table"}
	if err := d.resolve(fields, ""); err != nil {
		t.Fatal(err)
	}
	if d.Limit != 7 || d.Palette != "hev" || d.Appearance != "light" || d.Layout != "table" {
		t.Fatalf("overrides lost: %+v", d)
	}
}

func TestTurbopufferHidesLayerMetadataByDefault(t *testing.T) {
	metadata := `{"schema":{"_hevlayer_source":{"type":"string"},"_hevlayer_text":{"type":"string","full_text_search":true},"title":{"type":"string"},"my_hevlayer_note":{"type":"string"}}}`
	for _, tc := range []struct {
		name         string
		definition   Definition
		wantMetadata bool
	}{
		{"default palette", Definition{Version: 1}, false},
		{"turbopuffer", Definition{Version: 1, Palette: "turbopuffer"}, false},
		{"hev", Definition{Version: 1, Palette: "hev"}, true},
		{"grayscale", Definition{Version: 1, Palette: "grayscale"}, true},
		{"explicit bindings", Definition{Version: 1, Fields: []string{"title", "_hevlayer_source", "_hevlayer_text"}, FilterFields: []string{"_hevlayer_source"}, QueryField: "_hevlayer_text"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &mockBackend{metadata: metadata, reply: `{"rows":[{"id":1,"title":"Document","_hevlayer_source":"internal","_hevlayer_text":"indexed metadata"}]}`}
			s := fixtureHost(t, b)
			s.definition = tc.definition
			s.preferred = "_hevlayer_text"
			w := request(t, s, "/api/bootstrap", `{}`)
			mustOK(t, w)
			var bootstrap struct {
				Fields []Field    `json:"fields"`
				App    Definition `json:"app"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &bootstrap); err != nil {
				t.Fatal(err)
			}
			hasMetadata, hasUserField := false, false
			for _, f := range bootstrap.Fields {
				hasMetadata = hasMetadata || strings.HasPrefix(f.Name, "_hevlayer")
				hasUserField = hasUserField || f.Name == "my_hevlayer_note"
			}
			if hasMetadata != tc.wantMetadata || !hasUserField {
				t.Fatalf("unexpected visible schema: %+v", bootstrap.Fields)
			}
			if (bootstrap.App.QueryField == "_hevlayer_text") != tc.wantMetadata {
				t.Fatalf("unexpected search default: %q", bootstrap.App.QueryField)
			}
			w = request(t, s, "/api/query", `{"limit":10}`)
			mustOK(t, w)
			if strings.Contains(w.Body.String(), `"_hevlayer_source"`) != tc.wantMetadata || strings.Contains(b.calls[0], `"_hevlayer_source"`) != tc.wantMetadata {
				t.Fatalf("metadata projection mismatch: request %s, response %s", b.calls[0], w.Body.String())
			}
			w = request(t, s, "/api/values", `{"limit":10,"facet":"_hevlayer_source"}`)
			if !tc.wantMetadata && w.Code != 400 {
				t.Fatalf("hidden facet was accepted: %d", w.Code)
			}
		})
	}
}

func TestBundleDocumentationServedAsPlainText(t *testing.T) {
	data := []byte("<script>not executable</script>")
	sum := sha256.Sum256(data)
	manifest, _ := json.Marshal(Manifest{Protocol: 1, Version: "test", Files: map[string]string{"index.html": hex.EncodeToString(sum[:]), "docs/runtime.md": hex.EncodeToString(sum[:])}})
	assets, err := VerifyAssets(fstest.MapFS{"manifest.json": {Data: manifest}, "index.html": {Data: data}, "docs/runtime.md": {Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	s := fixtureHost(t, &mockBackend{})
	s.assets = assets
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", s.origin+"/docs/runtime.md", nil))
	mustOK(t, w)
	if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Body.String() != string(data) {
		t.Fatal("documentation was not served as plain text")
	}
}

func TestNumericValueSetsPreserveUint64Precision(t *testing.T) {
	b := &mockBackend{metadata: schemaFixture, reply: `{"rows":[]}`}
	s := fixtureHost(t, b)
	w := request(t, s, "/api/query", `{"limit":10,"filters":[{"field":"size","op":"In","value":["18446744073709551614","18446744073709551615"]}]}`)
	mustOK(t, w)
	if !strings.Contains(b.calls[0], `["size","In",[18446744073709551614,18446744073709551615]]`) {
		t.Fatal("numeric set lost precision", b.calls[0])
	}
	for _, values := range []string{`["18446744073709551616"]`, `[null]`, `[true]`, `[]`} {
		w := request(t, s, "/api/query", `{"limit":10,"filters":[{"field":"size","op":"In","value":`+values+`}]}`)
		if w.Code != 400 {
			t.Fatalf("accepted invalid set %s", values)
		}
	}
	if len(b.calls) != 1 {
		t.Fatal("invalid numeric set reached upstream")
	}
}
