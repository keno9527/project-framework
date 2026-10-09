package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func coreObject(properties map[string]any, required ...string) Schema {
	if required == nil {
		required = []string{}
	}
	return Schema{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func coreStringObject() Schema {
	return coreObject(map[string]any{"text": Schema{"type": "string"}}, "text")
}
func coreRegistry(t *testing.T) *Registry {
	t.Helper()
	registry := NewRegistry()
	err := registry.Register(Registration{Descriptor: Descriptor{Type: "test.echo", TypeVersion: "1", Title: "Echo", Description: "Returns its input", Category: "test", ConfigSchema: coreObject(map[string]any{}), InputSchema: coreStringObject(), OutputSchema: coreStringObject(), RetrySafe: true}, Handler: func(_ context.Context, call Call) (Object, error) { return call.Input, nil }})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
func coreDefinition() Definition {
	return Definition{APIVersion: "workflow/v1", WorkflowID: "example", DefinitionVersion: "1", Title: "Example", InputSchema: coreStringObject(), OutputSchema: coreStringObject(), Nodes: []NodeSpec{{ID: "source", Type: "test.echo", TypeVersion: "1", Config: Object{}, Inputs: map[string]Binding{"text": {Kind: "workflowInput", Field: "text"}}}, {ID: "sink", Type: "test.echo", TypeVersion: "1", Config: Object{}, DependsOn: []string{"source", "source"}, Inputs: map[string]Binding{"text": {Kind: "nodeOutput", NodeID: "source", Field: "text"}}}}, Outputs: map[string]Binding{"text": {Kind: "nodeOutput", NodeID: "sink", Field: "text"}}}
}

func TestCompileNormalizesDependenciesAndSnapshots(t *testing.T) {
	registry := coreRegistry(t)
	definition := coreDefinition()
	plan, err := Compile(definition, registry)
	if err != nil {
		t.Fatal(err)
	}
	view := plan.View()
	if len(view.Edges) != 1 || !reflect.DeepEqual(view.Edges[0].Reasons, []string{"data", "explicit"}) || len(view.Edges[0].Mappings) != 1 {
		t.Fatalf("unexpected merged edge: %#v", view.Edges)
	}
	if !reflect.DeepEqual(plan.order, []string{"source", "sink"}) {
		t.Fatalf("order = %v", plan.order)
	}
	if view.Nodes[0].TimeoutMS != 30000 || view.Nodes[0].Retry.MaxAttempts != 1 || view.Nodes[0].Retry.BackoffMS != 200 {
		t.Fatalf("defaults = %#v", view.Nodes[0])
	}
	// Neither caller-owned config, the view, nor catalog metadata may mutate plans.
	definition.Nodes[0].Inputs["text"] = Binding{Kind: "literal", Value: "changed"}
	view.Nodes[0].Inputs["text"] = Binding{Kind: "literal", Value: "changed"}
	view.NodeTypes[0].InputSchema["properties"].(map[string]any)["text"] = map[string]any{"type": "integer"}
	view.Edges[0].Reasons[0] = "changed"
	descriptors := registry.Descriptors()
	descriptors[0].Title = "changed"
	next := plan.View()
	if next.Nodes[0].Inputs["text"].Kind != "nodeOutput" || next.Edges[0].Reasons[0] != "data" || registry.Descriptors()[0].Title == "changed" {
		t.Fatal("mutation escaped copy boundary")
	}
	if err := plan.ValidateInput(Object{"text": "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := plan.ValidateInput(Object{"text": 4}); err == nil {
		t.Fatal("expected input type error")
	}
	reverse := coreDefinition()
	reverse.Nodes[0], reverse.Nodes[1] = reverse.Nodes[1], reverse.Nodes[0]
	other, err := Compile(reverse, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.View(), other.View()) {
		t.Fatal("node declaration ordering changed compiled view")
	}
}

func TestCompileRejectsInvalidGraphsAndContracts(t *testing.T) {
	cases := []struct {
		name     string
		change   func(*Definition)
		contains string
	}{
		{"unknown version", func(d *Definition) { d.Nodes[0].TypeVersion = "2" }, "unknown type/version"},
		{"duplicate ID", func(d *Definition) { d.Nodes[1].ID = "source" }, "duplicate id"},
		{"self dependency", func(d *Definition) { d.Nodes[0].DependsOn = []string{"source"} }, "cycle"},
		{"cycle", func(d *Definition) { d.Nodes[0].DependsOn = []string{"sink"} }, "cycle"},
		{"unknown dependency", func(d *Definition) { d.Nodes[0].DependsOn = []string{"absent"} }, "unknown node"},
		{"missing source field", func(d *Definition) { d.Nodes[0].Inputs["text"] = Binding{Kind: "workflowInput", Field: "absent"} }, "undeclared source field"},
		{"missing binding", func(d *Definition) { delete(d.Nodes[0].Inputs, "text") }, "required binding"},
		{"unknown target", func(d *Definition) { d.Nodes[0].Inputs["extra"] = Binding{Kind: "literal", Value: "x"} }, "undeclared target"},
		{"type conflict", func(d *Definition) {
			d.InputSchema = coreObject(map[string]any{"text": Schema{"type": "number"}}, "text")
		}, "types conflict"},
		{"literal contract", func(d *Definition) { d.Nodes[0].Inputs["text"] = Binding{Kind: "literal", Value: 3} }, "expected string"},
		{"config contract", func(d *Definition) { d.Nodes[0].Config = Object{"extra": true} }, "unknown field"},
		{"output binding", func(d *Definition) { d.Outputs["text"] = Binding{Kind: "nodeOutput", NodeID: "absent", Field: "text"} }, "unknown source node"},
		{"strategy range", func(d *Definition) { d.Nodes[0].Retry.MaxAttempts = 4 }, "maxAttempts"},
		{"too many nodes", func(d *Definition) {
			for len(d.Nodes) <= MaxNodes {
				d.Nodes = append(d.Nodes, d.Nodes[0])
			}
		}, "1–50"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := coreDefinition()
			tc.change(&def)
			_, err := Compile(def, coreRegistry(t))
			if err == nil || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("expected %q, got %v", tc.contains, err)
			}
		})
	}
}

func TestRegistryValidationAndFreeze(t *testing.T) {
	registry := coreRegistry(t)
	reg, _ := registry.lookup("test.echo", "1")
	if err := registry.Register(reg); err == nil {
		t.Fatal("expected duplicate error")
	}
	reg.Descriptor.TypeVersion = "2"
	reg.Descriptor.RetrySafe = false
	if err := registry.Register(reg); err != nil {
		t.Fatal(err)
	}
	def := coreDefinition()
	def.Nodes[0].TypeVersion = "2"
	def.Nodes[0].Retry.MaxAttempts = 2
	if _, err := Compile(def, registry); err == nil || !strings.Contains(err.Error(), "retrySafe") {
		t.Fatalf("expected retry safety error: %v", err)
	}
	registry.Freeze()
	reg.Descriptor.TypeVersion = "3"
	if err := registry.Register(reg); err == nil {
		t.Fatal("expected frozen error")
	}
	reg.Descriptor.TypeVersion = "4"
	reg.Handler = nil
	if err := NewRegistry().Register(reg); err == nil {
		t.Fatal("accepted nil handler")
	}
}

const coreYAML = `apiVersion: workflow/v1
workflowId: example
definitionVersion: "1"
title: Example
inputSchema:
  type: object
  additionalProperties: false
  required: [text]
  properties:
    text: {type: string}
outputSchema:
  type: object
  additionalProperties: false
  required: [text]
  properties:
    text: {type: string}
nodes:
  - id: echo
    type: test.echo
    typeVersion: "1"
    config: {}
    inputs:
      text: {kind: workflowInput, field: text}
outputs:
  text: {kind: nodeOutput, nodeId: echo, field: text}
`

func TestStrictYAMLRejectsUnsupportedSyntax(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"unknown field", coreYAML + "unknown: value\n", "unknown field"},
		{"duplicate key", coreYAML + "title: Duplicate\n", "duplicate key"},
		{"multiple documents", coreYAML + "---\n{}\n", "multiple YAML documents"},
		{"nonstring key", strings.Replace(coreYAML, "config: {}", "config: {1: value}", 1), "keys must be strings"},
		{"alias", strings.Replace(coreYAML, "config: {}", "config: &config {}", 1), "aliases and anchors"},
		{"custom tag", strings.Replace(coreYAML, "title: Example", "title: !custom Example", 1), "unsupported YAML tag"},
		{"timestamp", strings.Replace(coreYAML, "title: Example", "title: 2026-09-20", 1), "unsupported YAML tag"},
		{"mixed binding", strings.Replace(coreYAML, "kind: workflowInput, field: text", "kind: workflowInput, field: text, value: null", 1), "invalid field"},
		{"missing literal", strings.Replace(coreYAML, "kind: workflowInput, field: text", "kind: literal", 1), "value: required"},
		{"explicit zero", strings.Replace(coreYAML, "config: {}", "config: {}\n    timeoutMs: 0", 1), "timeoutMs"},
		{"explicit zero retry", strings.Replace(coreYAML, "config: {}", "config: {}\n    retry: {maxAttempts: 0}", 1), "maxAttempts"},
		{"unknown policy", strings.Replace(coreYAML, "config: {}", "config: {}\n    retry: {jitter: true}", 1), "unknown field"},
		{"version type", strings.Replace(coreYAML, "definitionVersion: \"1\"", "definitionVersion: 1", 1), "cannot unmarshal number"},
		{"nonfinite number", strings.Replace(coreYAML, "config: {}", "config: {number: .inf}", 1), "JSON-compatible"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDefinition([]byte(tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadDirAtomicVersionedCatalog(t *testing.T) {
	directory := t.TempDir()
	registry := coreRegistry(t)
	if _, err := LoadDir(directory, registry); err == nil {
		t.Fatal("accepted empty directory")
	}
	if err := os.WriteFile(filepath.Join(directory, "one.yaml"), []byte(coreYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "ignored.yaml"), 0700); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadDir(directory, registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Summaries()) != 1 {
		t.Fatal("missing workflow")
	}
	if _, ok := catalog.Find("example", "latest"); ok {
		t.Fatal("implicit latest was accepted")
	}
	invalid := []byte(strings.Replace(coreYAML, "test.echo", "test.absent", 1))
	if err := os.WriteFile(filepath.Join(directory, "two.yml"), invalid, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err = LoadDir(directory, registry)
	if catalog != nil || err == nil || !strings.Contains(err.Error(), "two.yml") {
		t.Fatalf("expected atomic error, got %v %v", catalog, err)
	}
	if strings.Contains(err.Error(), directory) {
		t.Fatal("error exposes absolute directory")
	}
	if err := os.WriteFile(filepath.Join(directory, "two.yml"), []byte(coreYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(directory, registry); err == nil || !strings.Contains(err.Error(), "duplicate workflow version") {
		t.Fatalf("expected duplicate: %v", err)
	}
	oversized := make([]byte, MaxFileBytes+1)
	if err := os.WriteFile(filepath.Join(directory, "two.yml"), oversized, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(directory, registry); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("expected size limit: %v", err)
	}
}

func TestSchemaSubsetAndActualValues(t *testing.T) {
	schema := coreObject(map[string]any{"count": Schema{"type": "integer", "minimum": 1, "maximum": 3}, "tags": Schema{"type": "array", "minItems": 1, "maxItems": 2, "items": Schema{"type": "string", "minLength": 2, "maxLength": 3, "enum": []string{"你好", "abc"}}}}, "count", "tags")
	if err := ValidateSchema(schema); err != nil {
		t.Fatal(err)
	}
	if err := ValidateValue(schema, Object{"count": json.Number("2"), "tags": []string{"你好"}}); err != nil {
		t.Fatal(err)
	}
	cases := []Object{{"count": 2.5, "tags": []string{"你好"}}, {"count": 0, "tags": []string{"你好"}}, {"count": 2, "tags": []string{}}, {"count": 2, "tags": []string{"no"}}, {"count": 2, "tags": []string{"abc"}, "extra": true}, {"tags": []string{"abc"}}}
	for _, value := range cases {
		if err := ValidateValue(schema, value); err == nil {
			t.Fatalf("accepted invalid value: %#v", value)
		}
	}
	invalid := []Schema{{"type": "object", "$ref": "remote"}, {"type": "array"}, {"type": "string", "minLength": -1}, {"type": "object", "required": []string{"missing"}}, {"type": "number", "enum": []int{1, 1}}, {"type": "object", "additionalProperties": true}, {"type": "string", "minimum": 3}}
	for _, s := range invalid {
		if err := ValidateSchema(s); err == nil {
			t.Fatalf("accepted invalid schema: %#v", s)
		}
	}
}

func TestAdaptAndNestedCopyIsolation(t *testing.T) {
	type input struct {
		Text string `json:"text"`
	}
	type config struct {
		Upper bool `json:"upper"`
	}
	type output struct {
		Result string `json:"result"`
	}
	handler := Adapt(func(ctx context.Context, in input, cfg config, meta ExecutionMeta) (output, error) {
		if meta.Attempt != 2 {
			t.Error("metadata was not forwarded")
		}
		if cfg.Upper {
			in.Text = strings.ToUpper(in.Text)
		}
		return output{Result: in.Text}, nil
	})
	result, err := handler(context.Background(), Call{Input: Object{"text": "hello"}, Config: Object{"upper": true}, Meta: ExecutionMeta{Attempt: 2}})
	if err != nil || result["result"] != "HELLO" {
		t.Fatalf("result %#v, error %v", result, err)
	}
	if _, err := handler(context.Background(), Call{Input: Object{"text": 42}, Config: Object{}}); err == nil {
		t.Fatal("typed adapter accepted mismatched type")
	}
	original := Object{"nested": []any{Object{"value": "original"}}}
	copied := cloneObject(original)
	copied["nested"].([]any)[0].(Object)["value"] = "changed"
	if original["nested"].([]any)[0].(Object)["value"] != "original" {
		t.Fatal("nested copy aliases original")
	}
}
