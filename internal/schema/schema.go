package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Valid simple schema types.
var ValidSimpleTypes = map[string]bool{
	"string":   true,
	"[]string": true,
	"uint64":   true,
	"int":      true,
	"float":    true,
	"uuid":     true,
	"bool":     true,
}

// VectorTypePattern matches vector types like [1536]f32 or [768]f16.
var VectorTypePattern = regexp.MustCompile(`^\[\d+\]f(16|32)$`)

// Valid keys for complex type objects.
var ValidTypeKeys = map[string]bool{
	"type":             true,
	"full_text_search": true,
	"regex_index":      true,
	"filterable":       true,
	"ann":              true,
	"glob":             true,
	"regex":            true,
}

// PropertyChange represents a single property change within an attribute.
type PropertyChange struct {
	Key      string
	OldValue string
	NewValue string
}

// AttrModification holds the details of a modified attribute.
type AttrModification struct {
	BaseType string
	Changes  []PropertyChange
}

// SchemaDiff holds the result of comparing two schemas.
type SchemaDiff struct {
	Unchanged     map[string]string         // attr -> display type
	Additions     map[string]string         // attr -> display type (new fields)
	Modifications map[string]AttrModification // attr -> modification details
	Conflicts     map[string][2]string      // attr -> [old_type, new_type] (base type change — blocked)
}

// HasConflicts returns true if there are any type conflicts.
func (d *SchemaDiff) HasConflicts() bool {
	return len(d.Conflicts) > 0
}

// HasModifications returns true if there are any property modifications.
func (d *SchemaDiff) HasModifications() bool {
	return len(d.Modifications) > 0
}

// HasChanges returns true if there are any additions, modifications, or conflicts.
func (d *SchemaDiff) HasChanges() bool {
	return len(d.Additions) > 0 || len(d.Modifications) > 0 || len(d.Conflicts) > 0
}

// NormalizeSchemaType normalizes a schema type to a comparable string.
// Handles strings, map[string]any, and other types.
func NormalizeSchemaType(attrType any) string {
	switch v := attrType.(type) {
	case string:
		return v
	case map[string]any:
		b, _ := json.Marshal(v)
		return string(b)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// SchemaTypeForDisplay converts a schema type to a human-readable string.
func SchemaTypeForDisplay(attrType any) string {
	switch v := attrType.(type) {
	case string:
		return v
	case map[string]any:
		if len(v) == 1 {
			if t, ok := v["type"]; ok {
				return fmt.Sprintf("%v", t)
			}
		}
		b, _ := json.Marshal(v)
		return string(b)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ValidateSchemaType validates a single schema attribute type.
// Returns a list of error messages (empty if valid).
func ValidateSchemaType(attrName string, attrType any) []string {
	var errors []string

	switch v := attrType.(type) {
	case string:
		if ValidSimpleTypes[v] {
			return nil
		}
		if VectorTypePattern.MatchString(v) {
			return nil
		}
		validTypes := sortedSimpleTypes()
		errors = append(errors, fmt.Sprintf(
			"Attribute '%s': invalid type '%s'. Valid types: %s, or vector format [dims]f32/f16",
			attrName, v, strings.Join(validTypes, ", ")))

	case map[string]any:
		typeVal, hasType := v["type"]
		if !hasType {
			errors = append(errors, fmt.Sprintf(
				"Attribute '%s': complex type object must have a 'type' key", attrName))
		} else {
			baseType, isStr := typeVal.(string)
			if !isStr {
				errors = append(errors, fmt.Sprintf(
					"Attribute '%s': 'type' must be a string", attrName))
			} else if !ValidSimpleTypes[baseType] && !VectorTypePattern.MatchString(baseType) {
				validTypes := sortedSimpleTypes()
				errors = append(errors, fmt.Sprintf(
					"Attribute '%s': invalid base type '%s'. Valid types: %s, or vector format [dims]f32/f16",
					attrName, baseType, strings.Join(validTypes, ", ")))
			}
		}

		// Check for unknown keys
		var unknownKeys []string
		for k := range v {
			if !ValidTypeKeys[k] {
				unknownKeys = append(unknownKeys, k)
			}
		}
		if len(unknownKeys) > 0 {
			sort.Strings(unknownKeys)
			validKeys := sortedValidTypeKeys()
			errors = append(errors, fmt.Sprintf(
				"Attribute '%s': unknown keys %v. Valid keys: %s",
				attrName, unknownKeys, strings.Join(validKeys, ", ")))
		}

		// Validate specific option types
		if fts, ok := v["full_text_search"]; ok {
			switch fts.(type) {
			case bool:
				// ok
			case map[string]any:
				// ok — full config object from API
			default:
				errors = append(errors, fmt.Sprintf(
					"Attribute '%s': 'full_text_search' must be a boolean or object", attrName))
			}
		}
		if ri, ok := v["regex_index"]; ok {
			if _, isBool := ri.(bool); !isBool {
				errors = append(errors, fmt.Sprintf(
					"Attribute '%s': 'regex_index' must be a boolean", attrName))
			}
		}
		if f, ok := v["filterable"]; ok {
			if _, isBool := f.(bool); !isBool {
				errors = append(errors, fmt.Sprintf(
					"Attribute '%s': 'filterable' must be a boolean", attrName))
			}
		}

	default:
		errors = append(errors, fmt.Sprintf(
			"Attribute '%s': type must be a string or object, got %T", attrName, attrType))
	}

	return errors
}

// ValidateSchema validates a complete schema dictionary.
// Returns a list of error messages (empty if valid).
func ValidateSchema(schemaData map[string]any) []string {
	var errors []string

	for attrName, attrType := range schemaData {
		if attrName == "" {
			errors = append(errors, "Attribute name cannot be empty")
			continue
		}
		errors = append(errors, ValidateSchemaType(attrName, attrType)...)
	}

	// The "id" attribute must have filterable: true if present.
	if idType, ok := schemaData["id"]; ok {
		if m, isMap := idType.(map[string]any); isMap {
			if f, hasFilt := m["filterable"]; hasFilt {
				if b, isBool := f.(bool); isBool && !b {
					errors = append(errors, "Attribute 'id': must have \"filterable\" set to true")
				}
			}
		}
	}

	// Vector attributes must have ann: true.
	for attrName, attrType := range schemaData {
		m, isMap := attrType.(map[string]any)
		if !isMap {
			continue
		}
		baseType, _ := m["type"].(string)
		if !VectorTypePattern.MatchString(baseType) {
			continue
		}
		ann, hasAnn := m["ann"]
		if !hasAnn {
			errors = append(errors, fmt.Sprintf(
				"Attribute '%s': vector type must have \"ann\" set to true", attrName))
		} else if b, isBool := ann.(bool); isBool && !b {
			errors = append(errors, fmt.Sprintf(
				"Attribute '%s': vector type must have \"ann\" set to true", attrName))
		}
	}

	return errors
}

// ExtractBaseType returns the base type string from a schema attribute value.
// For simple strings like "string", returns the string itself.
// For complex objects like {"type":"string","filterable":true}, returns "string".
func ExtractBaseType(attrType any) string {
	switch v := attrType.(type) {
	case string:
		return v
	case map[string]any:
		if t, ok := v["type"].(string); ok {
			return t
		}
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ParseVectorDims extracts the dimension count from a vector type string like "[768]f32".
func ParseVectorDims(vectorType string) int {
	matches := VectorTypePattern.FindStringSubmatch(vectorType)
	if matches == nil {
		return 0
	}
	// Extract number between brackets
	start := 1 // skip '['
	end := strings.Index(vectorType, "]")
	if end <= start {
		return 0
	}
	var dims int
	_, _ = fmt.Sscanf(vectorType[start:end], "%d", &dims)
	return dims
}

// annEnabled returns true if the ann value represents "enabled" (either bool true or a config object).
func annEnabled(v any) bool {
	switch a := v.(type) {
	case bool:
		return a
	case map[string]any:
		// Any non-empty object means ann is enabled
		return len(a) > 0
	}
	return false
}

// computePropertyChanges compares two attribute maps and returns the changed properties.
// attrName is used for special-case handling (e.g. "id").
func computePropertyChanges(attrName string, oldMap, newMap map[string]any) []PropertyChange {
	var changes []PropertyChange
	// Collect all keys
	allKeys := make(map[string]bool)
	for k := range oldMap {
		allKeys[k] = true
	}
	for k := range newMap {
		allKeys[k] = true
	}

	sorted := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	for _, k := range sorted {
		if k == "type" {
			continue // base type is shown separately
		}

		// The "id" attribute is always filterable regardless of what the API reports.
		if attrName == "id" && k == "filterable" {
			continue
		}

		// For "ann", compare by enabled/disabled rather than exact representation,
		// since the API returns an object but writes accept a boolean.
		if k == "ann" {
			if annEnabled(oldMap[k]) != annEnabled(newMap[k]) {
				oldVal, _ := json.Marshal(oldMap[k])
				newVal, _ := json.Marshal(newMap[k])
				changes = append(changes, PropertyChange{
					Key:      k,
					OldValue: string(oldVal),
					NewValue: string(newVal),
				})
			}
			continue
		}

		oldVal, _ := json.Marshal(oldMap[k])
		newVal, _ := json.Marshal(newMap[k])
		if string(oldVal) != string(newVal) {
			changes = append(changes, PropertyChange{
				Key:      k,
				OldValue: string(oldVal),
				NewValue: string(newVal),
			})
		}
	}
	return changes
}

// toMap normalizes an attribute type to a map for property comparison.
func toMap(attrType any) map[string]any {
	switch v := attrType.(type) {
	case map[string]any:
		return v
	case string:
		return map[string]any{"type": v}
	default:
		return map[string]any{"type": fmt.Sprintf("%v", v)}
	}
}

// ComputeSchemaDiff computes the difference between current and new schemas.
// currentSchema can be nil (namespace doesn't exist yet).
func ComputeSchemaDiff(currentSchema, newSchema map[string]any) SchemaDiff {
	diff := SchemaDiff{
		Unchanged:     make(map[string]string),
		Additions:     make(map[string]string),
		Modifications: make(map[string]AttrModification),
		Conflicts:     make(map[string][2]string),
	}

	if currentSchema == nil {
		currentSchema = map[string]any{}
	}

	// Normalize current schema
	currentNormalized := make(map[string]string)
	for attr, attrType := range currentSchema {
		currentNormalized[attr] = NormalizeSchemaType(attrType)
	}

	// Compare each attribute in new schema
	for attr, newType := range newSchema {
		newTypeNormalized := NormalizeSchemaType(newType)
		newTypeDisplay := SchemaTypeForDisplay(newType)

		oldNorm, exists := currentNormalized[attr]
		if !exists {
			diff.Additions[attr] = newTypeDisplay
		} else if oldNorm == newTypeNormalized {
			diff.Unchanged[attr] = newTypeDisplay
		} else {
			oldTypeDisplay := SchemaTypeForDisplay(currentSchema[attr])
			oldBase := ExtractBaseType(currentSchema[attr])
			newBase := ExtractBaseType(newType)
			if oldBase == newBase || oldBase == "unknown" || oldBase == "[]unknown" {
				// Same base type, different properties — check for meaningful changes
				changes := computePropertyChanges(attr, toMap(currentSchema[attr]), toMap(newType))
				if len(changes) > 0 {
					diff.Modifications[attr] = AttrModification{
						BaseType: newBase,
						Changes:  changes,
					}
				} else {
					// All differences were suppressed (e.g. ann normalization)
					diff.Unchanged[attr] = newTypeDisplay
				}
			} else {
				// Different base type — conflict (blocked)
				diff.Conflicts[attr] = [2]string{oldTypeDisplay, newTypeDisplay}
			}
		}
	}

	return diff
}

// LoadSchemaFile loads and validates a schema from a JSON file.
func LoadSchemaFile(filePath string) (map[string]any, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("schema file not found: %s", filePath)
		}
		return nil, fmt.Errorf("error reading schema file: %w", err)
	}

	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON in schema file: %s", err)
	}

	schemaData, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema file must contain a JSON object")
	}

	errors := ValidateSchema(schemaData)
	if len(errors) > 0 {
		return nil, fmt.Errorf("invalid schema:\n  %s", strings.Join(errors, "\n  "))
	}

	return schemaData, nil
}

func sortedSimpleTypes() []string {
	types := make([]string, 0, len(ValidSimpleTypes))
	for t := range ValidSimpleTypes {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

func sortedValidTypeKeys() []string {
	keys := make([]string, 0, len(ValidTypeKeys))
	for k := range ValidTypeKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
