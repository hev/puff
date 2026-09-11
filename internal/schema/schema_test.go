package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeSchemaType tests type normalization to comparable strings.
func TestNormalizeSchemaType(t *testing.T) {
	t.Run("simple string type", func(t *testing.T) {
		assert.Equal(t, "string", NormalizeSchemaType("string"))
		assert.Equal(t, "uint64", NormalizeSchemaType("uint64"))
	})

	t.Run("dict type with sorted keys", func(t *testing.T) {
		input := map[string]any{"type": "string", "full_text_search": true}
		result := NormalizeSchemaType(input)
		assert.Equal(t, `{"full_text_search":true,"type":"string"}`, result)
	})

	t.Run("non-string non-map type uses fmt", func(t *testing.T) {
		result := NormalizeSchemaType(42)
		assert.Equal(t, "42", result)
	})
}

// TestSchemaTypeForDisplay tests human-readable display formatting.
func TestSchemaTypeForDisplay(t *testing.T) {
	t.Run("simple string", func(t *testing.T) {
		assert.Equal(t, "string", SchemaTypeForDisplay("string"))
		assert.Equal(t, "[1536]f32", SchemaTypeForDisplay("[1536]f32"))
	})

	t.Run("single-key dict with only type is simplified", func(t *testing.T) {
		result := SchemaTypeForDisplay(map[string]any{"type": "string"})
		assert.Equal(t, "string", result)
	})

	t.Run("complex dict shown as JSON", func(t *testing.T) {
		input := map[string]any{"type": "string", "full_text_search": true}
		result := SchemaTypeForDisplay(input)
		assert.Contains(t, result, "string")
		assert.Contains(t, result, "full_text_search")
	})
}

// TestValidateSchemaType tests validation of individual attribute types.
func TestValidateSchemaType(t *testing.T) {
	t.Run("valid simple types", func(t *testing.T) {
		for _, typeName := range []string{"string", "uint64", "uuid", "bool"} {
			errors := ValidateSchemaType("test_attr", typeName)
			assert.Empty(t, errors, "Expected no errors for %s", typeName)
		}
	})

	t.Run("valid vector types", func(t *testing.T) {
		errors := ValidateSchemaType("vec", "[1536]f32")
		assert.Empty(t, errors)
		errors = ValidateSchemaType("vec", "[768]f16")
		assert.Empty(t, errors)
	})

	t.Run("invalid simple type", func(t *testing.T) {
		errors := ValidateSchemaType("test_attr", "invalid_type")
		assert.Len(t, errors, 1)
		assert.Contains(t, errors[0], "invalid type")
	})

	t.Run("valid complex type", func(t *testing.T) {
		errors := ValidateSchemaType("content", map[string]any{"type": "string", "full_text_search": true})
		assert.Empty(t, errors)
	})

	t.Run("complex type missing type key", func(t *testing.T) {
		errors := ValidateSchemaType("content", map[string]any{"full_text_search": true})
		assert.Len(t, errors, 1)
		assert.Contains(t, errors[0], "'type' key")
	})

	t.Run("complex type unknown keys", func(t *testing.T) {
		errors := ValidateSchemaType("content", map[string]any{"type": "string", "unknown_key": true})
		assert.Len(t, errors, 1)
		assert.Contains(t, errors[0], "unknown key")
	})

	t.Run("complex type with invalid base type", func(t *testing.T) {
		errors := ValidateSchemaType("content", map[string]any{"type": "invalid"})
		assert.Len(t, errors, 1)
		assert.Contains(t, errors[0], "invalid base type")
	})

	t.Run("complex type with non-string type value", func(t *testing.T) {
		errors := ValidateSchemaType("content", map[string]any{"type": 42})
		assert.Len(t, errors, 1)
		assert.Contains(t, errors[0], "'type' must be a string")
	})

	t.Run("full_text_search must be boolean", func(t *testing.T) {
		errors := ValidateSchemaType("content", map[string]any{"type": "string", "full_text_search": "yes"})
		assert.Len(t, errors, 1)
		assert.Contains(t, errors[0], "'full_text_search' must be a boolean")
	})

	t.Run("invalid type argument (not string or map)", func(t *testing.T) {
		errors := ValidateSchemaType("test_attr", 42)
		assert.Len(t, errors, 1)
		assert.Contains(t, errors[0], "type must be a string or object")
	})
}

// TestValidateSchema tests full schema validation.
func TestValidateSchema(t *testing.T) {
	t.Run("valid schema", func(t *testing.T) {
		schema := map[string]any{
			"content":   "string",
			"vector":    "[1536]f32",
			"timestamp": "uint64",
		}
		errors := ValidateSchema(schema)
		assert.Empty(t, errors)
	})

	t.Run("invalid attribute type", func(t *testing.T) {
		schema := map[string]any{"content": "invalid"}
		errors := ValidateSchema(schema)
		assert.Len(t, errors, 1)
	})

	t.Run("empty attribute name", func(t *testing.T) {
		schema := map[string]any{"": "string"}
		errors := ValidateSchema(schema)
		assert.Len(t, errors, 1)
		assert.Contains(t, errors[0], "empty")
	})
}

// TestComputeSchemaDiff tests diff computation between schemas.
func TestComputeSchemaDiff(t *testing.T) {
	t.Run("all new attributes", func(t *testing.T) {
		diff := ComputeSchemaDiff(nil, map[string]any{"field1": "string", "field2": "uint64"})
		assert.Len(t, diff.Additions, 2)
		assert.Empty(t, diff.Unchanged)
		assert.Empty(t, diff.Conflicts)
	})

	t.Run("all unchanged", func(t *testing.T) {
		current := map[string]any{"field1": "string", "field2": "uint64"}
		newSchema := map[string]any{"field1": "string", "field2": "uint64"}
		diff := ComputeSchemaDiff(current, newSchema)
		assert.Empty(t, diff.Additions)
		assert.Len(t, diff.Unchanged, 2)
		assert.Empty(t, diff.Conflicts)
	})

	t.Run("mixed changes", func(t *testing.T) {
		current := map[string]any{"field1": "string"}
		newSchema := map[string]any{"field1": "string", "field2": "uint64"}
		diff := ComputeSchemaDiff(current, newSchema)
		assert.Len(t, diff.Additions, 1)
		assert.Contains(t, diff.Additions, "field2")
		assert.Len(t, diff.Unchanged, 1)
		assert.Contains(t, diff.Unchanged, "field1")
	})

	t.Run("type conflict", func(t *testing.T) {
		current := map[string]any{"field1": "string"}
		newSchema := map[string]any{"field1": "uint64"}
		diff := ComputeSchemaDiff(current, newSchema)
		assert.Len(t, diff.Conflicts, 1)
		assert.Contains(t, diff.Conflicts, "field1")
		assert.True(t, diff.HasConflicts())
	})

	t.Run("has_changes property", func(t *testing.T) {
		// No changes
		diff := SchemaDiff{Unchanged: map[string]string{"a": "string"}}
		assert.False(t, diff.HasChanges())

		// With additions
		diff = SchemaDiff{Additions: map[string]string{"a": "string"}}
		assert.True(t, diff.HasChanges())

		// With conflicts
		diff = SchemaDiff{Conflicts: map[string][2]string{"a": {"string", "uint64"}}}
		assert.True(t, diff.HasChanges())
	})
}

// TestLoadSchemaFile tests JSON schema file loading and validation.
func TestLoadSchemaFile(t *testing.T) {
	t.Run("file not found", func(t *testing.T) {
		_, err := LoadSchemaFile("/nonexistent/path/schema.json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "invalid.json")
		require.NoError(t, os.WriteFile(path, []byte("{ not valid json }"), 0644))

		_, err := LoadSchemaFile(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid JSON")
	})

	t.Run("not an object", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "array.json")
		require.NoError(t, os.WriteFile(path, []byte(`["a", "b"]`), 0644))

		_, err := LoadSchemaFile(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "JSON object")
	})

	t.Run("valid schema file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "schema.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"content": "string", "timestamp": "uint64"}`), 0644))

		result, err := LoadSchemaFile(path)
		require.NoError(t, err)
		assert.Equal(t, "string", result["content"])
		assert.Equal(t, "uint64", result["timestamp"])
	})

	t.Run("invalid schema content", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "schema.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"content": "invalid_type"}`), 0644))

		_, err := LoadSchemaFile(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid schema")
	})
}
