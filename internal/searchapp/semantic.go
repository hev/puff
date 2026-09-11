package searchapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// Semantic binds text queries to native embedding on a source field, or to an
// existing vector index using the same model that originally embedded its rows.
// An explicitly empty Field disables semantic search; nil derives it from schema.
type Semantic struct {
	Field string `json:"field"`
	Model string `json:"model,omitempty"`
}
type Embedding struct {
	Model     string `json:"model"`
	Attribute string `json:"attribute,omitempty"`
}

var denseVector = regexp.MustCompile(`^\[[1-9][0-9]*\]f(16|32)$`)
var modelSlug = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,199}$`)

func parseEmbedding(raw json.RawMessage, typ string) (*Embedding, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var e Embedding
	if err := json.Unmarshal(raw, &e); err != nil {
		if err = json.Unmarshal(raw, &e.Model); err != nil {
			return nil, errors.New("invalid native embedding declaration")
		}
	}
	if typ != "string" || !modelSlug.MatchString(e.Model) {
		return nil, errors.New("native embeddings require a string field and model slug")
	}
	return &e, nil
}

type semanticBinding struct {
	field, model string
	native       bool
}

func (d Definition) semanticBinding(fields []Field) (*semanticBinding, error) {
	if d.Semantic == nil || d.Semantic.Field == "" {
		return nil, nil
	}
	for _, f := range fields {
		if f.Name != d.Semantic.Field {
			continue
		}
		if f.Embed != nil {
			if d.Semantic.Model != "" && d.Semantic.Model != f.Embed.Model {
				return nil, errors.New("semantic model must match the schema embedding model")
			}
			target := f.Embed.Attribute
			if target == "" {
				target = "embed_" + f.Name
			}
			for _, v := range fields {
				if v.Name == target && (!v.Vector || !v.ANN) {
					return nil, errors.New("native embedding target needs an ANN index")
				}
			}
			return &semanticBinding{field: f.Name, model: f.Embed.Model, native: true}, nil
		}
		if !f.Vector || !f.ANN {
			return nil, errors.New("semantic field needs native embeddings or an indexed dense vector")
		}
		if !modelSlug.MatchString(d.Semantic.Model) {
			return nil, errors.New("semantic vector field requires an embedding model slug")
		}
		return &semanticBinding{field: f.Name, model: d.Semantic.Model}, nil
	}
	return nil, fmt.Errorf("semantic field %q is absent from schema", d.Semantic.Field)
}
func (b semanticBinding) rank(query string) []any {
	embed := []any{"Embed", query}
	if !b.native {
		embed = append(embed, map[string]string{"model": b.model})
	}
	return []any{b.field, "ANN", embed}
}
