// Package searchapp hosts a schema-driven, read-only search application.
package searchapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	tp "github.com/turbopuffer/turbopuffer-go"
)

const Protocol = 1
const maxResults = 100

type Definition struct {
	Semantic     *Semantic `json:"semantic,omitempty"`
	Version      int       `json:"version"`
	QueryField   string    `json:"queryField,omitempty"`
	Fields       []string  `json:"fields"`
	FilterFields []string  `json:"filterFields"`
	TitleField   string    `json:"titleField,omitempty"`
	ImageField   string    `json:"imageField,omitempty"`
	SourceField  string    `json:"sourceField,omitempty"`
	Layout       string    `json:"layout,omitempty"`
	Palette      string    `json:"palette,omitempty"`
	Appearance   string    `json:"appearance,omitempty"`
	Limit        int       `json:"limit,omitempty"`
}

func LoadDefinition(path string) (Definition, error) {
	d := Definition{Version: Protocol}
	if path == "" {
		return d, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return d, err
	}
	defer func() { _ = f.Close() }()
	err = decode(io.LimitReader(f, 64<<10), &d)
	if err != nil {
		return d, fmt.Errorf("invalid app definition: %w", err)
	}
	return d, nil
}

type Field struct {
	Embed      *Embedding `json:"embed,omitempty"`
	Vector     bool       `json:"vector,omitempty"`
	ANN        bool       `json:"ann,omitempty"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Filterable bool       `json:"filterable"`
	Searchable bool       `json:"searchable"`
}

func parseSchema(raw string) ([]Field, error) {
	var metadata struct {
		Schema map[string]json.RawMessage `json:"schema"`
	}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil, err
	}
	fields := make([]Field, 0, len(metadata.Schema))
	for name, raw := range metadata.Schema {
		var f struct {
			Type       string          `json:"type"`
			Embed      json.RawMessage `json:"embed"`
			ANN        json.RawMessage `json:"ann"`
			Filterable *bool           `json:"filterable"`
			FullText   json.RawMessage `json:"full_text_search"`
			Regex      bool            `json:"regex"`
			Glob       bool            `json:"glob"`
			Fuzzy      bool            `json:"fuzzy"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			if err := json.Unmarshal(raw, &f.Type); err != nil {
				return nil, fmt.Errorf("invalid schema field %q", name)
			}
		}
		fts := bytes.Equal(f.FullText, []byte("true")) || bytes.HasPrefix(bytes.TrimSpace(f.FullText), []byte("{"))
		filterable := !fts && !f.Regex && !f.Glob && !f.Fuzzy
		if f.Filterable != nil {
			filterable = *f.Filterable
		}
		embed, err := parseEmbedding(f.Embed, f.Type)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		vector := denseVector.MatchString(f.Type)
		fields = append(fields, Field{Name: name, Type: f.Type, Filterable: filterable && supportedScalar(f.Type), Searchable: fts && (f.Type == "string" || f.Type == "[]string"), Embed: embed, Vector: vector, ANN: vector && (len(f.ANN) == 0 || bytes.Equal(bytes.TrimSpace(f.ANN), []byte("true")))})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields, nil
}

func supportedScalar(t string) bool {
	return slices.Contains([]string{"string", "uint", "int", "float", "bool", "datetime"}, t)
}
func numeric(t string) bool { return t == "uint" || t == "int" || t == "float" }
func displayable(t string) bool {
	return supportedScalar(t) || t == "uuid" || slices.Contains([]string{"[]string", "[]uint", "[]int", "[]float", "[]bool", "[]uuid", "[]datetime"}, t)
}

// Turbopuffer apps omit Layer metadata unless a binding explicitly requests it.
// Apply this before resolving defaults so hidden fields cannot become filters,
// displayed attributes, or the automatically selected full-text field.
func (d Definition) visibleFields(fields []Field) []Field {
	if d.Palette != "" && d.Palette != "turbopuffer" {
		return fields
	}
	visible := make([]Field, 0, len(fields))
	for _, f := range fields {
		explicit := slices.Contains(d.Fields, f.Name) || slices.Contains(d.FilterFields, f.Name) ||
			slices.Contains([]string{d.QueryField, d.TitleField, d.ImageField, d.SourceField}, f.Name) || (d.Semantic != nil && d.Semantic.Field == f.Name)
		if !strings.HasPrefix(f.Name, "_hevlayer") || explicit {
			visible = append(visible, f)
		}
	}
	return visible
}

func (d *Definition) resolve(fields []Field, preferred string) error {
	if d.Version != Protocol {
		return fmt.Errorf("app version must be %d", Protocol)
	}
	if d.Layout == "" {
		d.Layout = "list"
	}
	if d.Palette == "" {
		d.Palette = "turbopuffer"
	}
	if d.Appearance == "" {
		d.Appearance = "dark"
	}
	if d.Limit == 0 {
		d.Limit = 25
	}
	if d.Limit < 1 || d.Limit > maxResults {
		return errors.New("app limit must be between 1 and 100")
	}
	if !slices.Contains([]string{"list", "table", "cards"}, d.Layout) || !slices.Contains([]string{"hev", "grayscale", "turbopuffer"}, d.Palette) || !slices.Contains([]string{"light", "dark"}, d.Appearance) {
		return errors.New("invalid layout, palette, or appearance")
	}
	byName := map[string]Field{}
	searchable := []string{}
	for _, f := range fields {
		byName[f.Name] = f
		if f.Searchable {
			searchable = append(searchable, f.Name)
		}
	}
	if d.Semantic == nil {
		for _, f := range fields {
			if f.Embed != nil {
				d.Semantic = &Semantic{Field: f.Name}
				break
			}
		}
	}
	if _, err := d.semanticBinding(fields); err != nil {
		return err
	}
	if d.QueryField == "" {
		if byName[preferred].Searchable {
			d.QueryField = preferred
		} else {
			for _, name := range []string{"content", "body", "text", "title"} {
				if byName[name].Searchable {
					d.QueryField = name
					break
				}
			}
			if d.QueryField == "" && len(searchable) > 0 {
				d.QueryField = searchable[0]
			}
		}
	}
	if d.QueryField != "" && !byName[d.QueryField].Searchable {
		return fmt.Errorf("queryField %q needs a full-text index", d.QueryField)
	}
	if d.Fields == nil {
		d.Fields = []string{}
		for _, f := range fields {
			if displayable(f.Type) && len(d.Fields) < 20 {
				d.Fields = append(d.Fields, f.Name)
			}
		}
	}
	if len(d.Fields) > 50 {
		return errors.New("at most 50 displayed fields are supported")
	}
	for _, name := range d.Fields {
		if !displayable(byName[name].Type) {
			return fmt.Errorf("display field %q is absent or unsupported", name)
		}
	}
	if d.FilterFields == nil {
		d.FilterFields = []string{}
		for _, f := range fields {
			if f.Filterable {
				d.FilterFields = append(d.FilterFields, f.Name)
			}
		}
	}
	for _, name := range d.FilterFields {
		if !byName[name].Filterable {
			return fmt.Errorf("filter field %q is absent or unsupported", name)
		}
	}
	for _, name := range []string{d.TitleField, d.ImageField, d.SourceField} {
		if name != "" && (byName[name].Type != "string" || !slices.Contains(d.Fields, name)) {
			return fmt.Errorf("mapping %q must be a displayed string field", name)
		}
	}
	return nil
}

type Predicate struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}
type Query struct {
	Mode    string      `json:"mode,omitempty"`
	Query   string      `json:"query"`
	Field   string      `json:"field"`
	Limit   int         `json:"limit"`
	Filters []Predicate `json:"filters"`
	After   string      `json:"after,omitempty"`
	Facet   string      `json:"facet,omitempty"`
}

func decode(r io.Reader, value any) error {
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}

func filterValue(raw json.RawMessage, typ string) (any, error) {
	var v any
	if err := decode(bytes.NewReader(raw), &v); err != nil {
		return nil, errors.New("invalid filter value")
	}
	if v == nil {
		return nil, nil
	}
	if numeric(typ) {
		var s string
		switch n := v.(type) {
		case string:
			s = n
		case json.Number:
			s = n.String()
		default:
			return nil, errors.New("expected a number")
		}
		switch typ {
		case "int":
			n, err := strconv.ParseInt(s, 10, 64)
			return n, err
		case "uint":
			n, err := strconv.ParseUint(s, 10, 64)
			return n, err
		default:
			n, err := strconv.ParseFloat(s, 64)
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, errors.New("expected finite number")
			}
			return n, err
		}
	}
	switch typ {
	case "bool":
		if _, ok := v.(bool); !ok {
			return nil, errors.New("expected true or false")
		}
	case "string":
		if _, ok := v.(string); !ok {
			return nil, errors.New("expected a string")
		}
	case "datetime":
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("expected an ISO timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
			return nil, errors.New("expected an ISO timestamp")
		}
	default:
		return nil, errors.New("unsupported filter type")
	}
	return v, nil
}

func compileFilters(predicates []Predicate, fields []Field, allowed []string, skip string) (tp.Filter, error) {
	if len(predicates) > 32 {
		return nil, errors.New("at most 32 predicates are supported")
	}
	byName := map[string]Field{}
	for _, f := range fields {
		byName[f.Name] = f
	}
	filters := []tp.Filter{}
	for _, p := range predicates {
		f, ok := byName[p.Field]
		if !ok || !f.Filterable || !slices.Contains(allowed, p.Field) {
			return nil, fmt.Errorf("filter field %q is absent or unsupported", p.Field)
		}
		if p.Op == "In" {
			var rawValues []json.RawMessage
			if (f.Type != "string" && !numeric(f.Type)) || json.Unmarshal(p.Value, &rawValues) != nil || len(rawValues) == 0 || len(rawValues) > 100 {
				return nil, errors.New("in filter requires 1–100 string or numeric values matching the field type")
			}
			values := make([]any, 0, len(rawValues))
			for _, raw := range rawValues {
				value, err := filterValue(raw, f.Type)
				if err != nil || value == nil {
					return nil, fmt.Errorf("invalid in value for %q", p.Field)
				}
				values = append(values, value)
			}
			if p.Field != skip {
				filters = append(filters, tp.NewFilterIn(p.Field, values))
			}
			continue
		}
		v, err := filterValue(p.Value, f.Type)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %w", p.Field, err)
		}
		var filter tp.Filter
		switch p.Op {
		case "Eq":
			filter = tp.NewFilterEq(p.Field, v)
		case "NotEq":
			filter = tp.NewFilterNotEq(p.Field, v)
		case "Gt", "Gte", "Lt", "Lte":
			if v == nil || (!numeric(f.Type) && f.Type != "datetime") {
				return nil, errors.New("ordered comparisons require a number or date")
			}
			switch p.Op {
			case "Gt":
				filter = tp.NewFilterGt(p.Field, v)
			case "Gte":
				filter = tp.NewFilterGte(p.Field, v)
			case "Lt":
				filter = tp.NewFilterLt(p.Field, v)
			case "Lte":
				filter = tp.NewFilterLte(p.Field, v)
			}
			if p.Op == "Lt" || p.Op == "Lte" {
				filter = tp.NewFilterAnd([]tp.Filter{filter, tp.NewFilterNotEq(p.Field, nil)})
			}
		default:
			return nil, errors.New("unsupported filter operator")
		}
		if p.Field != skip {
			filters = append(filters, filter)
		}
	}
	return and(filters), nil
}

func and(filters []tp.Filter) tp.Filter {
	out := []tp.Filter{}
	for _, f := range filters {
		if f != nil {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil
	}
	if len(out) == 1 {
		return out[0]
	}
	return tp.NewFilterAnd(out)
}

// Protect integer precision when JSON reaches JavaScript. SDK wire decoding uses
// json.Number separately so a uint64 cursor never goes through float64.
func browserValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		if !strings.ContainsAny(x.String(), ".eE") {
			if f, err := strconv.ParseFloat(x.String(), 64); err == nil && math.Abs(f) > 9007199254740991 {
				return x.String()
			}
		}
	case map[string]any:
		for k, v := range x {
			x[k] = browserValue(v)
		}
	case []any:
		for i, v := range x {
			x[i] = browserValue(v)
		}
	}
	return v
}
