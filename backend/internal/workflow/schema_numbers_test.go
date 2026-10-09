package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaNumericResourceBounds(t *testing.T) {
	oversized := []json.Number{
		"1e-1000000000",
		"1e1025",
		"1e-1025",
		json.Number("0." + strings.Repeat("0", 1024) + "1"),
	}
	for _, value := range oversized {
		t.Run(string(value[:min(len(value), 40)]), func(t *testing.T) {
			if _, ok := numericRat(value); ok {
				t.Fatal("out-of-bound numeric text reached rational parsing")
			}
			if err := ValidateValue(Schema{"type": "number"}, value); err == nil {
				t.Fatal("oversized numeric input accepted")
			}
			if err := ValidateValue(Schema{"type": "object"}, Object{"undeclared": []any{value}}); err == nil {
				t.Fatal("oversized number escaped validation through undeclared property")
			}
			for _, schema := range []Schema{
				{"type": "number", "minimum": value},
				{"type": "number", "enum": []any{value}},
				{"type": "object", "default": map[string]any{"nested": []any{value}}},
			} {
				if err := ValidateSchema(schema); err == nil {
					t.Fatal("oversized numeric schema annotation or constraint accepted")
				}
			}
			if jsonEqual(value, value) {
				t.Fatal("invalid numeric enum candidate matched through fallback equality")
			}
		})
	}
	for _, value := range []json.Number{"1e-1024", "0.125", "1e3", "9007199254740993", "18446744073709551615"} {
		if err := ValidateValue(Schema{"type": "number"}, value); err != nil {
			t.Fatalf("valid bounded number %s rejected: %v", value, err)
		}
	}
}

func TestSchemaExactIntegerAndDecimalComparisons(t *testing.T) {
	large := json.Number("9007199254740993")
	previous := json.Number("9007199254740992")
	schema := Schema{"type": "integer", "minimum": large, "enum": []any{large}}
	if err := ValidateValue(schema, large); err != nil {
		t.Fatal(err)
	}
	if err := ValidateValue(schema, previous); err == nil {
		t.Fatal("integer precision lost beyond 2^53")
	}
	if err := ValidateValue(Schema{"type": "integer"}, json.Number("9007199254740993.1")); err == nil {
		t.Fatal("fractional large integer accepted")
	}
	if err := ValidateSchema(Schema{"type": "integer", "minimum": large, "maximum": previous}); err == nil {
		t.Fatal("inverted schema bounds hidden by float rounding")
	}
	if err := ValidateSchema(Schema{"type": "string", "minLength": json.Number("1e-1000")}); err == nil {
		t.Fatal("fractional minLength rounded to zero")
	}
	if err := ValidateValue(Schema{"type": "number", "minimum": json.Number("0.1"), "maximum": json.Number("0.3")}, json.Number("0.2")); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(map[string]any{"n": json.Number("1.0")}, map[string]any{"n": json.Number("1")}) {
		t.Fatal("mathematically equal numeric enum objects do not match")
	}
}
