package workflow

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func draftFixture(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("../../workflows/text-demo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := parseDefinition(data)
	if err != nil {
		t.Fatal(err)
	}
	// A parsed definition omits execution policy values; JSON is authored as a
	// draft so remove zero-valued struct fields to express compiler defaults.
	var object map[string]any
	if err := copyJSON(definition, &object); err != nil {
		t.Fatal(err)
	}
	for _, raw := range object["nodes"].([]any) {
		node := raw.(map[string]any)
		delete(node, "timeoutMs")
		delete(node, "retry")
	}
	return object
}

func TestParseJSONDefinitionStrictness(t *testing.T) {
	tests := []struct {
		name   string
		change func(map[string]any)
		want   string
	}{
		{"unknown root", func(d map[string]any) { d["unexpected"] = true }, "unknown field"},
		{"case variant", func(d map[string]any) { d["Title"] = "unexpected" }, "unknown field"},
		{"unknown node", func(d map[string]any) { d["nodes"].([]any)[0].(map[string]any)["unexpected"] = true }, "unknown field"},
		{"zero timeout", func(d map[string]any) { d["nodes"].([]any)[0].(map[string]any)["timeoutMs"] = 0 }, "timeoutMs"},
		{"null timeout", func(d map[string]any) { d["nodes"].([]any)[0].(map[string]any)["timeoutMs"] = nil }, "timeoutMs"},
		{"zero attempt", func(d map[string]any) {
			d["nodes"].([]any)[0].(map[string]any)["retry"] = map[string]any{"maxAttempts": 0}
		}, "maxAttempts"},
		{"null retry", func(d map[string]any) { d["nodes"].([]any)[0].(map[string]any)["retry"] = nil }, "must be an object"},
		{"mixed binding", func(d map[string]any) {
			d["nodes"].([]any)[0].(map[string]any)["inputs"] = map[string]any{"text": map[string]any{"kind": "workflowInput", "field": "text", "value": nil}}
		}, "invalid field"},
		{"missing literal", func(d map[string]any) {
			d["nodes"].([]any)[0].(map[string]any)["inputs"] = map[string]any{"text": map[string]any{"kind": "literal"}}
		}, "value: required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			object := draftFixture(t)
			test.change(object)
			data, _ := json.Marshal(object)
			if _, err := ParseJSONDefinition(data); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %s", err, test.want)
			}
		})
	}
	data, _ := json.Marshal(draftFixture(t))
	if _, err := ParseJSONDefinition(data); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`{"title":"a","title":"b"}`, `[]`, `{} {}`} {
		if _, err := ParseJSONDefinition([]byte(data)); err == nil {
			t.Errorf("invalid definition accepted: %s", data)
		}
	}
}

func TestDefinitionYAMLRoundTripNumbers(t *testing.T) {
	object := draftFixture(t)
	object["nodes"].([]any)[0].(map[string]any)["config"] = map[string]any{
		"bigInteger": json.Number("9007199254740993123456789"),
		"decimal":    json.Number("0.12345678901234567890123456789"),
		"scientific": json.Number("1.234567890123456789e-30"),
		"string":     "9007199254740993",
	}
	data, _ := json.Marshal(object)
	definition, err := ParseJSONDefinition(data)
	if err != nil {
		t.Fatal(err)
	}
	for i := range definition.Nodes {
		definition.Nodes[i].TimeoutMS = 30000
		definition.Nodes[i].Retry = RetryPolicy{MaxAttempts: 1, BackoffMS: 200}
	}
	yaml, err := MarshalDefinitionYAML(definition)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDefinition(yaml)
	if err != nil {
		t.Fatalf("reload YAML: %v\n%s", err, yaml)
	}
	if !reflect.DeepEqual(definition, loaded) {
		t.Fatalf("definition changed during YAML round trip\nbefore: %#v\nafter: %#v", definition.Nodes[0].Config, loaded.Nodes[0].Config)
	}
	second, err := MarshalDefinitionYAML(loaded)
	if err != nil || string(second) != string(yaml) {
		t.Fatalf("YAML export is not deterministic: %v", err)
	}
}
