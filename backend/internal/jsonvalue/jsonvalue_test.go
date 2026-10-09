package jsonvalue

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseStrictJSON(t *testing.T) {
	for _, input := range []string{`{"x":1,"x":2}`, `{"nested":{"a":1,"a":2}}`, `[{"a":1,"a":2}]`, `{} {}`, `{"x":}`, `{"x":1,}`, `[1,]`, `[}`, ``, strings.Repeat("[", 102) + "0" + strings.Repeat("]", 102)} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Errorf("accepted invalid JSON: %.100s", input)
		}
	}
	value, err := Parse([]byte(`{"integer":9007199254740993,"decimal":0.1234567890123456789,"array":[false,null,"a"]}`))
	if err != nil {
		t.Fatal(err)
	}
	object := value.(map[string]any)
	if object["integer"] != json.Number("9007199254740993") || object["decimal"] != json.Number("0.1234567890123456789") {
		t.Fatalf("numeric precision changed: %#v", object)
	}
}
