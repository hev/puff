package searchapp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tp "github.com/turbopuffer/turbopuffer-go"
	"github.com/turbopuffer/turbopuffer-go/option"
)

type Backend interface {
	Metadata(context.Context) (string, error)
	Query(context.Context, tp.NamespaceQueryParams) (string, error)
}

type SDKBackend struct{ Namespace *tp.Namespace }

func (b SDKBackend) Metadata(ctx context.Context) (string, error) {
	v, err := b.Namespace.Metadata(ctx, tp.NamespaceMetadataParams{}, option.WithMaxRetries(0))
	if err != nil {
		return "", err
	}
	return v.RawJSON(), nil
}
func (b SDKBackend) Query(ctx context.Context, q tp.NamespaceQueryParams) (string, error) {
	v, err := b.Namespace.Query(ctx, q, option.WithMaxRetries(0))
	if err != nil {
		return "", err
	}
	return v.RawJSON(), nil
}

type Server struct {
	backend                             Backend
	definition                          Definition
	preferred, namespace, origin, token string
	assets                              fs.FS
	slots                               chan struct{}
	mu                                  sync.Mutex
	cache                               map[string]cachedValues
}
type cachedValues struct {
	at   time.Time
	data any
}

func New(backend Backend, assets fs.FS, namespace string, definition Definition, preferred string) (*Server, error) {
	if namespace == "" {
		return nil, errors.New("namespace is required")
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil, errors.New("search UI bundle is missing; supply --ui-dir with a built search-ui runtime")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Server{backend: backend, assets: assets, namespace: namespace, definition: definition, preferred: preferred, token: hex.EncodeToString(b), slots: make(chan struct{}, 4), cache: map[string]cachedValues{}}, nil
}

// Bind is called once after listening, before serving requests.
func (s *Server) Bind(address string) string {
	s.origin = "http://" + address
	return s.origin + "/#session=" + s.token
}

func (s *Server) schema(ctx context.Context) ([]Field, Definition, error) {
	raw, err := s.backend.Metadata(ctx)
	if err != nil {
		return nil, Definition{}, upstreamError(err)
	}
	fields, err := parseSchema(raw)
	if err != nil {
		return nil, Definition{}, errors.New("upstream returned invalid schema")
	}
	d := s.definition
	fields = d.visibleFields(fields)
	if err = d.resolve(fields, s.preferred); err != nil {
		return nil, d, err
	}
	return fields, d, nil
}

// Check validates metadata before the CLI opens a browser.
func (s *Server) Check(ctx context.Context) error { _, _, err := s.schema(ctx); return err }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	if s.origin == "" || "http://"+r.Host != s.origin || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.origin) {
		writeError(w, 403, "local origin required")
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, 405, "method not allowed")
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		body, err := fs.ReadFile(s.assets, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(name, ".mjs") {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
		return
	}
	if !hmac.Equal([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) {
		writeError(w, 401, "reopen the URL printed by tpuff to authorize this tab")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "use POST")
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		writeError(w, 415, "expected application/json")
		return
	}
	if !slices.Contains([]string{"/api/bootstrap", "/api/query", "/api/values"}, r.URL.Path) {
		writeError(w, 404, "not found")
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		writeError(w, 429, "too many active requests; retry shortly")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var q Query
	if err := decode(http.MaxBytesReader(w, r.Body, 64<<10), &q); err != nil {
		writeError(w, 400, "invalid request JSON")
		return
	}
	fields, d, err := s.schema(ctx)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if r.URL.Path == "/api/bootstrap" {
		writeJSON(w, map[string]any{"protocol": Protocol, "namespace": s.namespace, "fields": fields, "app": d, "capabilities": []string{"BM25", "browse", "values"}})
		return
	}
	if q.Limit < 1 || q.Limit > maxResults || len(q.Query) > 8192 || len(q.After) > 8192 {
		writeError(w, 400, "invalid result limit, query, or continuation")
		return
	}
	if r.URL.Path == "/api/values" {
		s.values(w, ctx, q, fields, d)
		return
	}
	if q.Facet != "" {
		writeError(w, 400, "facet is only valid for values requests")
		return
	}
	filters, err := compileFilters(q.Filters, fields, d.FilterFields, "")
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	params := tp.NamespaceQueryParams{TopK: tp.Int(int64(q.Limit)), Filters: filters, IncludeAttributes: tp.IncludeAttributesParam{StringArray: d.Fields}}
	if len(d.Fields) == 0 {
		params.IncludeAttributes = tp.IncludeAttributesParam{StringArray: []string{"id"}}
	}
	if strings.TrimSpace(q.Query) == "" {
		params.RankBy = tp.NewRankByAttribute("id", tp.RankByAttributeOrderAsc)
		if q.After != "" {
			id, err := s.readCursor(q)
			if err != nil {
				writeError(w, 400, "continuation does not match this request")
				return
			}
			params.Filters = and([]tp.Filter{filters, tp.NewFilterGt("id", id)})
		}
	} else {
		if q.After != "" {
			writeError(w, 400, "ranked searches do not support browse continuation")
			return
		}
		found := false
		for _, f := range fields {
			if f.Name == q.Field && f.Searchable {
				found = true
			}
		}
		if !found {
			writeError(w, 400, "choose a field with a full-text index")
			return
		}
		params.RankBy = tp.NewRankByTextBM25(q.Field, q.Query)
	}
	raw, err := s.backend.Query(ctx, params)
	if err != nil {
		writeError(w, 502, upstreamError(err).Error())
		return
	}
	var response struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decodeLoose(raw, &response); err != nil {
		writeError(w, 502, "invalid upstream response")
		return
	}
	if len(response.Rows) > q.Limit {
		writeError(w, 502, "upstream exceeded the result limit")
		return
	}
	if response.Rows == nil {
		response.Rows = []map[string]any{}
	}
	next := ""
	if strings.TrimSpace(q.Query) == "" && len(response.Rows) == q.Limit {
		if id, ok := response.Rows[len(response.Rows)-1]["id"]; ok {
			next = s.cursor(q, id)
		}
	}
	for _, row := range response.Rows {
		for key := range row {
			if key != "id" && key != "$dist" && !slices.Contains(d.Fields, key) {
				delete(row, key)
			}
		}
		browserValue(row)
	}
	writeJSON(w, map[string]any{"rows": response.Rows, "after": next, "ranked": strings.TrimSpace(q.Query) != "", "limit": q.Limit})
}

func (s *Server) values(w http.ResponseWriter, ctx context.Context, q Query, fields []Field, d Definition) {
	found := false
	for _, f := range fields {
		if f.Name == q.Facet && f.Type == "string" && f.Filterable && slices.Contains(d.FilterFields, f.Name) {
			found = true
		}
	}
	if !found || q.After != "" {
		writeError(w, 400, "choose a filterable string facet")
		return
	}
	filters, err := compileFilters(q.Filters, fields, d.FilterFields, q.Facet)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	// No ranking predicate: these are counts within the attribute-filter scope.
	params := tp.NamespaceQueryParams{TopK: tp.Int(int64(q.Limit)), Filters: filters, AggregateBy: map[string]tp.AggregateBy{"count": tp.NewAggregateByCount()}, GroupBy: []string{q.Facet}}
	keyBytes, _ := json.Marshal([]any{fields, params})
	key := string(keyBytes)
	s.mu.Lock()
	cached, ok := s.cache[key]
	s.mu.Unlock()
	if ok && time.Since(cached.at) < 30*time.Second {
		writeJSON(w, cached.data)
		return
	}
	raw, err := s.backend.Query(ctx, params)
	if err != nil {
		writeError(w, 502, upstreamError(err).Error())
		return
	}
	var response struct {
		Groups []map[string]any `json:"aggregation_groups"`
	}
	if err := decodeLoose(raw, &response); err != nil || len(response.Groups) > q.Limit {
		writeError(w, 502, "invalid upstream values response")
		return
	}
	values := []map[string]any{}
	for _, group := range response.Groups {
		v, ok := group[q.Facet].(string)
		if ok {
			values = append(values, map[string]any{"v": v, "n": browserValue(group["count"])})
		}
	}
	data := map[string]any{"values": values, "total": len(values), "truncated": len(response.Groups) == q.Limit, "scope": "Attribute filters, excluding this facet; not ranked search matches"}
	s.mu.Lock()
	if len(s.cache) >= 32 {
		s.cache = map[string]cachedValues{}
	}
	s.cache[key] = cachedValues{at: time.Now(), data: data}
	s.mu.Unlock()
	writeJSON(w, data)
}

type continuation struct {
	ID    json.RawMessage `json:"id"`
	Scope string          `json:"scope"`
}

func scope(q Query) string {
	q.After = ""
	raw, _ := json.Marshal(q)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func (s *Server) cursor(q Query, id any) string {
	raw, _ := json.Marshal(id)
	body, _ := json.Marshal(continuation{ID: raw, Scope: scope(q)})
	mac := hmac.New(sha256.New, []byte(s.token))
	_, _ = mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Server) readCursor(q Query) (any, error) {
	parts := strings.Split(q.After, ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid cursor")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, []byte(s.token))
	_, _ = mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, errors.New("invalid cursor")
	}
	var c continuation
	if json.Unmarshal(body, &c) != nil || c.Scope != scope(q) {
		return nil, errors.New("invalid cursor")
	}
	var id any
	if err := decode(bytes.NewReader(c.ID), &id); err != nil {
		return nil, err
	}
	switch v := id.(type) {
	case string:
		return v, nil
	case json.Number:
		n, err := strconv.ParseUint(v.String(), 10, 64)
		return n, err
	default:
		return nil, errors.New("invalid ID")
	}
}

func decodeLoose(raw string, v any) error {
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	return d.Decode(v)
}
func upstreamError(err error) error {
	if errors.Is(err, context.Canceled) {
		return errors.New("request cancelled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("upstream timed out; retry the request")
	}
	var api *tp.Error
	if errors.As(err, &api) {
		return fmt.Errorf("turbopuffer returned HTTP %d; check namespace access, indexes, and configuration", api.StatusCode)
	}
	return errors.New("could not reach Turbopuffer; check the selected environment")
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
