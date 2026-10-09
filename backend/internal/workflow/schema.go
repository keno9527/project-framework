package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ValidateSchema checks the deliberately small schema vocabulary supported by V1.
func ValidateSchema(schema Schema) error {
	normalized, err := normalizeSchema(schema)
	if err != nil {
		return err
	}
	return checkSchema(normalized, "schema")
}

// ValidateValue validates a JSON-compatible value without applying schema defaults.
func ValidateValue(schema Schema, value any) error { return validateValue(schema, value) }

func normalizeSchema(schema Schema) (Schema, error) {
	var normalized Schema
	if err := copyJSON(schema, &normalized); err != nil {
		return nil, fmt.Errorf("schema is not JSON-compatible: %w", err)
	}
	if normalized == nil {
		return nil, fmt.Errorf("schema must be an object")
	}
	if err := checkJSONNumbers(map[string]any(normalized), "schema"); err != nil {
		return nil, err
	}
	return normalized, nil
}

func copyJSON(from, to any) error {
	data, err := json.Marshal(from)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(to)
}

func checkSchema(s Schema, path string) error {
	allowed := map[string]bool{"$schema": true, "type": true, "title": true, "description": true, "default": true, "enum": true, "properties": true, "required": true, "additionalProperties": true, "items": true, "minLength": true, "maxLength": true, "minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true, "minItems": true, "maxItems": true}
	for _, key := range sortedKeys(s) {
		if !allowed[key] {
			return fmt.Errorf("%s.%s: unsupported schema keyword", path, key)
		}
	}
	typ, ok := s["type"].(string)
	if !ok || !contains([]string{"object", "array", "string", "number", "integer", "boolean"}, typ) {
		return fmt.Errorf("%s.type: unsupported or missing type", path)
	}
	for _, key := range []string{"title", "description"} {
		if v, exists := s[key]; exists {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("%s.%s: must be a string", path, key)
			}
		}
	}
	if v, ok := s["$schema"]; ok && v != "https://json-schema.org/draft/2020-12/schema" {
		return fmt.Errorf("%s.$schema: only JSON Schema 2020-12 is supported", path)
	}
	groups := map[string][]string{"object": {"properties", "required", "additionalProperties"}, "array": {"items", "minItems", "maxItems"}, "string": {"minLength", "maxLength"}, "number": {"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"}}
	for group, keys := range groups {
		for _, key := range keys {
			if _, found := s[key]; found && typ != group && !(group == "number" && typ == "integer") {
				return fmt.Errorf("%s.%s: invalid for %s", path, key, typ)
			}
		}
	}
	if typ == "object" {
		properties := map[string]any{}
		if raw, ok := s["properties"]; ok {
			var valid bool
			properties, valid = raw.(map[string]any)
			if !valid {
				return fmt.Errorf("%s.properties: must be an object", path)
			}
		}
		for _, name := range sortedKeys(properties) {
			child, ok := properties[name].(map[string]any)
			if !ok {
				return fmt.Errorf("%s.properties.%s: must be a schema", path, name)
			}
			if err := checkSchema(child, path+".properties."+name); err != nil {
				return err
			}
		}
		if raw, ok := s["required"]; ok {
			names, ok := raw.([]any)
			if !ok {
				return fmt.Errorf("%s.required: must be an array", path)
			}
			seen := map[string]bool{}
			for _, rawName := range names {
				name, ok := rawName.(string)
				if !ok || seen[name] {
					return fmt.Errorf("%s.required: entries must be unique strings", path)
				}
				if _, exists := properties[name]; !exists {
					return fmt.Errorf("%s.required: undeclared property %q", path, name)
				}
				seen[name] = true
			}
		}
		if raw, ok := s["additionalProperties"]; ok && raw != false {
			return fmt.Errorf("%s.additionalProperties: only false is supported", path)
		}
	}
	if typ == "array" {
		raw, ok := s["items"].(map[string]any)
		if !ok {
			return fmt.Errorf("%s.items: homogeneous item schema is required", path)
		}
		if err := checkSchema(raw, path+".items"); err != nil {
			return err
		}
	}
	for _, key := range []string{"minLength", "maxLength", "minItems", "maxItems", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"} {
		if raw, ok := s[key]; ok {
			n, valid := numericRat(raw)
			if !valid {
				return fmt.Errorf("%s.%s: must be a finite number", path, key)
			}
			if strings.HasSuffix(key, "Length") || strings.HasSuffix(key, "Items") {
				if n.Sign() < 0 || !n.IsInt() {
					return fmt.Errorf("%s.%s: must be a nonnegative integer", path, key)
				}
			}
		}
	}
	for _, pair := range [][2]string{{"minLength", "maxLength"}, {"minItems", "maxItems"}, {"minimum", "maximum"}, {"exclusiveMinimum", "exclusiveMaximum"}} {
		a, aok := numericRat(s[pair[0]])
		b, bok := numericRat(s[pair[1]])
		if aok && bok && a.Cmp(b) > 0 {
			return fmt.Errorf("%s: %s exceeds %s", path, pair[0], pair[1])
		}
	}
	if raw, exists := s["enum"]; exists {
		values, ok := raw.([]any)
		if !ok || len(values) == 0 {
			return fmt.Errorf("%s.enum: must be a nonempty array", path)
		}
		without := make(Schema, len(s))
		for k, v := range s {
			if k != "enum" {
				without[k] = v
			}
		}
		for i, v := range values {
			if err := checkValue(without, v, path+".enum"); err != nil {
				return err
			}
			for _, prior := range values[:i] {
				if jsonEqual(v, prior) {
					return fmt.Errorf("%s.enum: duplicate value", path)
				}
			}
		}
	}
	return nil
}

func validateValue(schema Schema, value any) error {
	normalized, err := normalizeSchema(schema)
	if err != nil {
		return err
	}
	if err := checkSchema(normalized, "schema"); err != nil {
		return err
	}
	// Validation also rejects non-JSON values (NaN, channels, functions, etc.).
	var normalizedValue any
	if err := copyJSON(value, &normalizedValue); err != nil {
		return fmt.Errorf("value is not JSON-compatible: %w", err)
	}
	if err := checkJSONNumbers(normalizedValue, "value"); err != nil {
		return err
	}
	return checkValue(normalized, normalizedValue, "value")
}

func checkValue(s Schema, value any, path string) error {
	typ, _ := s["type"].(string)
	switch typ {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object", path)
		}
		props, _ := s["properties"].(map[string]any)
		required, _ := s["required"].([]any)
		for _, raw := range required {
			key, _ := raw.(string)
			if _, ok := obj[key]; !ok {
				return fmt.Errorf("%s.%s: required field is missing", path, key)
			}
		}
		for _, key := range sortedKeys(obj) {
			child, exists := props[key]
			if !exists {
				if s["additionalProperties"] == false {
					return fmt.Errorf("%s.%s: unknown field", path, key)
				}
				continue
			}
			if err := checkValue(child.(map[string]any), obj[key], path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		values, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: expected array", path)
		}
		if err := checkLength(s, len(values), "Items", path); err != nil {
			return err
		}
		child, _ := s["items"].(map[string]any)
		for i, v := range values {
			if err := checkValue(child, v, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		v, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s: expected string", path)
		}
		if err := checkLength(s, utf8.RuneCountInString(v), "Length", path); err != nil {
			return err
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: expected boolean", path)
		}
	case "number", "integer":
		n, ok := numericRat(value)
		if !ok || (typ == "integer" && !n.IsInt()) {
			return fmt.Errorf("%s: expected %s", path, typ)
		}
		if v, ok := numericRat(s["minimum"]); ok && n.Cmp(v) < 0 {
			return fmt.Errorf("%s: below minimum", path)
		}
		if v, ok := numericRat(s["maximum"]); ok && n.Cmp(v) > 0 {
			return fmt.Errorf("%s: above maximum", path)
		}
		if v, ok := numericRat(s["exclusiveMinimum"]); ok && n.Cmp(v) <= 0 {
			return fmt.Errorf("%s: below exclusive minimum", path)
		}
		if v, ok := numericRat(s["exclusiveMaximum"]); ok && n.Cmp(v) >= 0 {
			return fmt.Errorf("%s: above exclusive maximum", path)
		}
	default:
		return fmt.Errorf("%s: unsupported schema type", path)
	}
	if values, ok := s["enum"].([]any); ok {
		for _, candidate := range values {
			if jsonEqual(value, candidate) {
				return nil
			}
		}
		return fmt.Errorf("%s: value is not in enum", path)
	}
	return nil
}

func checkLength(s Schema, length int, suffix, path string) error {
	if n, ok := number(s["min"+suffix]); ok && float64(length) < n {
		return fmt.Errorf("%s: shorter than min%s", path, suffix)
	}
	if n, ok := number(s["max"+suffix]); ok && float64(length) > n {
		return fmt.Errorf("%s: longer than max%s", path, suffix)
	}
	return nil
}

func number(v any) (float64, bool) {
	var n float64
	switch value := v.(type) {
	case json.Number:
		if !boundedNumericText(string(value)) {
			return 0, false
		}
		var err error
		n, err = value.Float64()
		if err != nil {
			return 0, false
		}
	case float64:
		n = value
	case float32:
		n = float64(value)
	default:
		rv := reflect.ValueOf(v)
		if !rv.IsValid() {
			return 0, false
		}
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n = float64(rv.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			n = float64(rv.Uint())
		default:
			return 0, false
		}
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0)
}

// Bound decimal text before big.Rat can expand an exponent. The JSON byte
// limit alone cannot bound numbers such as 1e-1000000000.
func boundedNumericText(text string) bool {
	const maxNumericTextBytes = 1024
	const maxDecimalExponent = 1024
	if len(text) > maxNumericTextBytes {
		return false
	}
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(text[i+1:])
		if err != nil || exponent < -maxDecimalExponent || exponent > maxDecimalExponent {
			return false
		}
	}
	return true
}

// Check every normalized JSON number, including metadata defaults and values
// beneath undeclared object properties, before schema matching or comparisons.
func checkJSONNumbers(value any, path string) error {
	switch value := value.(type) {
	case json.Number:
		if _, valid := number(value); !valid {
			return fmt.Errorf("%s: number must be finite with at most 1024 text bytes and decimal exponent within -1024..1024", path)
		}
	case map[string]any:
		for _, key := range sortedKeys(value) {
			if err := checkJSONNumbers(value[key], path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range value {
			if err := checkJSONNumbers(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func jsonEqual(a, b any) bool {
	_, aJSONNumber := a.(json.Number)
	_, bJSONNumber := b.(json.Number)
	if aJSONNumber || bJSONNumber {
		av, aok := numericRat(a)
		bv, bok := numericRat(b)
		return aok && bok && av.Cmp(bv) == 0
	}
	if av, ok := numericRat(a); ok {
		if bv, ok := numericRat(b); ok {
			return av.Cmp(bv) == 0
		}
	}
	if av, ok := a.([]any); ok {
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	}
	if av, ok := a.(map[string]any); ok {
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for key, value := range av {
			other, exists := bv[key]
			if !exists || !jsonEqual(value, other) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// Preserve integer precision when comparing JSON numbers beyond float64's
// exact range, including enum membership and fractional integer rejection.
func numericRat(value any) (*big.Rat, bool) {
	if _, ok := number(value); !ok {
		return nil, false
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	return new(big.Rat).SetString(string(encoded))
}
func sortedKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func cloneObject(input Object) Object {
	if input == nil {
		return nil
	}
	result := make(Object, len(input))
	for key, value := range input {
		result[key] = cloneValue(value)
	}
	return result
}
func cloneValue(value any) any {
	switch v := value.(type) {
	case Object:
		return cloneObject(v)
	case Schema:
		result := make(Schema, len(v))
		for key, value := range v {
			result[key] = cloneValue(value)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, value := range v {
			result[key] = cloneValue(value)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, value := range v {
			result[i] = cloneValue(value)
		}
		return result
	case []string:
		return append([]string(nil), v...)
	default:
		// Typed slices/maps are accepted at API boundaries too; normalize their copy.
		rv := reflect.ValueOf(value)
		if rv.IsValid() && (rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array || rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Struct) {
			var result any
			if copyJSON(value, &result) == nil {
				return result
			}
		}
		return value
	}
}
