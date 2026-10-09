package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	DefaultTimeoutMS   = 30000
	DefaultMaxAttempts = 1
	DefaultBackoffMS   = 200
	MaxNodes           = 50
	MaxEdges           = 200
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

// Compile resolves a declarative workflow into an immutable, deterministic DAG.
// It performs no business I/O and does not execute registered handlers.
func Compile(def Definition, registry *Registry) (*Plan, error) {
	var snapshot Definition
	if err := copyJSON(def, &snapshot); err != nil {
		return nil, fmt.Errorf("definition is not JSON-compatible: %w", err)
	}
	if snapshot.APIVersion != "workflow/v1" {
		return nil, fmt.Errorf("apiVersion: expected workflow/v1")
	}
	if !identifierPattern.MatchString(snapshot.WorkflowID) {
		return nil, fmt.Errorf("workflowId: invalid identifier")
	}
	if strings.TrimSpace(snapshot.DefinitionVersion) == "" {
		return nil, fmt.Errorf("definitionVersion: required")
	}
	if strings.TrimSpace(snapshot.Title) == "" {
		return nil, fmt.Errorf("title: required")
	}
	for _, field := range []struct {
		name   string
		schema Schema
	}{{"inputSchema", snapshot.InputSchema}, {"outputSchema", snapshot.OutputSchema}} {
		if err := ValidateSchema(field.schema); err != nil {
			return nil, fmt.Errorf("%s: %w", field.name, err)
		}
		if field.schema["type"] != "object" {
			return nil, fmt.Errorf("%s: root must be object", field.name)
		}
	}
	if len(snapshot.Nodes) == 0 || len(snapshot.Nodes) > MaxNodes {
		return nil, fmt.Errorf("nodes: must contain 1–%d nodes", MaxNodes)
	}
	plan := &Plan{definition: snapshot, nodes: make(map[string]compiledNode), edges: []Edge{}}
	for i, spec := range snapshot.Nodes {
		if !identifierPattern.MatchString(spec.ID) {
			return nil, fmt.Errorf("nodes[%d].id: invalid identifier", i)
		}
		if _, exists := plan.nodes[spec.ID]; exists {
			return nil, fmt.Errorf("node %s: duplicate id", spec.ID)
		}
		registration, ok := registry.lookup(spec.Type, spec.TypeVersion)
		if !ok {
			return nil, fmt.Errorf("node %s: unknown type/version %s@%s", spec.ID, spec.Type, spec.TypeVersion)
		}
		registration.Descriptor = cloneDescriptor(registration.Descriptor)
		if spec.Config == nil {
			spec.Config = Object{}
		}
		if spec.Inputs == nil {
			spec.Inputs = map[string]Binding{}
		}
		if err := validateValue(registration.Descriptor.ConfigSchema, spec.Config); err != nil {
			return nil, fmt.Errorf("node %s.config: %w", spec.ID, err)
		}
		if err := applyPolicy(&spec, registration.Descriptor.RetrySafe); err != nil {
			return nil, fmt.Errorf("node %s: %w", spec.ID, err)
		}
		plan.nodes[spec.ID] = compiledNode{spec: spec, registration: registration}
	}
	type edgeKey struct{ source, target string }
	edgeMap := map[edgeKey]*Edge{}
	addEdge := func(source, target, reason string, mapping *FieldMapping) {
		key := edgeKey{source, target}
		edge := edgeMap[key]
		if edge == nil {
			edge = &Edge{ID: source + "->" + target, Source: source, Target: target, Reasons: []string{}, Mappings: []FieldMapping{}}
			edgeMap[key] = edge
		}
		if !contains(edge.Reasons, reason) {
			edge.Reasons = append(edge.Reasons, reason)
		}
		if mapping != nil {
			edge.Mappings = append(edge.Mappings, *mapping)
		}
	}
	for _, id := range sortedKeys(plan.nodes) {
		node := plan.nodes[id]
		for _, dep := range node.spec.DependsOn {
			if _, ok := plan.nodes[dep]; !ok {
				return nil, fmt.Errorf("node %s.dependsOn: unknown node %q", id, dep)
			}
			addEdge(dep, id, "explicit", nil)
		}
		if err := checkBindings(node.spec.Inputs, node.registration.Descriptor.InputSchema, plan, func(source, sourceField, targetField string) {
			addEdge(source, id, "data", &FieldMapping{SourceField: sourceField, TargetField: targetField})
		}); err != nil {
			return nil, fmt.Errorf("node %s.inputs: %w", id, err)
		}
	}
	if len(edgeMap) > MaxEdges {
		return nil, fmt.Errorf("dependencies: exceeds %d edges", MaxEdges)
	}
	for _, edge := range edgeMap {
		sort.Strings(edge.Reasons)
		sort.Slice(edge.Mappings, func(i, j int) bool {
			a, b := edge.Mappings[i], edge.Mappings[j]
			if a.TargetField != b.TargetField {
				return a.TargetField < b.TargetField
			}
			return a.SourceField < b.SourceField
		})
		plan.edges = append(plan.edges, *edge)
	}
	sort.Slice(plan.edges, func(i, j int) bool {
		a, b := plan.edges[i], plan.edges[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Target < b.Target
	})
	for _, edge := range plan.edges {
		node := plan.nodes[edge.Target]
		node.dependencies = append(node.dependencies, edge.Source)
		plan.nodes[edge.Target] = node
	}
	var err error
	plan.order, err = topologicalOrder(plan.nodes)
	if err != nil {
		return nil, err
	}
	if err := checkBindings(snapshot.Outputs, snapshot.OutputSchema, plan, nil); err != nil {
		return nil, fmt.Errorf("outputs: %w", err)
	}
	// Canonical node order and sorted/deduplicated explicit dependencies make the
	// view independent of YAML ordering without changing dependency semantics.
	plan.definition.Nodes = make([]NodeSpec, 0, len(plan.nodes))
	for _, id := range sortedKeys(plan.nodes) {
		node := plan.nodes[id]
		node.spec.DependsOn = uniqueSorted(node.spec.DependsOn)
		sort.Strings(node.dependencies)
		plan.nodes[id] = node
		plan.definition.Nodes = append(plan.definition.Nodes, node.spec)
	}
	return plan, nil
}

func applyPolicy(spec *NodeSpec, retrySafe bool) error {
	if spec.TimeoutMS == 0 {
		spec.TimeoutMS = DefaultTimeoutMS
	}
	if spec.TimeoutMS < 1 || spec.TimeoutMS > 120000 {
		return fmt.Errorf("timeoutMs: must be 1–120000")
	}
	if spec.Retry.MaxAttempts == 0 {
		spec.Retry.MaxAttempts = DefaultMaxAttempts
	}
	if spec.Retry.MaxAttempts < 1 || spec.Retry.MaxAttempts > 3 {
		return fmt.Errorf("retry.maxAttempts: must be 1–3")
	}
	if spec.Retry.BackoffMS == 0 {
		spec.Retry.BackoffMS = DefaultBackoffMS
	}
	if spec.Retry.BackoffMS < 1 || spec.Retry.BackoffMS > 10000 {
		return fmt.Errorf("retry.backoffMs: must be 1–10000")
	}
	if spec.Retry.MaxAttempts > 1 && !retrySafe {
		return fmt.Errorf("retry.maxAttempts: node does not declare retrySafe")
	}
	return nil
}

func checkBindings(bindings map[string]Binding, target Schema, plan *Plan, onData func(string, string, string)) error {
	properties, _ := target["properties"].(map[string]any)
	required, _ := target["required"].([]any)
	for _, raw := range required {
		name := raw.(string)
		if _, ok := bindings[name]; !ok {
			return fmt.Errorf("%s: required binding is missing", name)
		}
	}
	for _, field := range sortedKeys(bindings) {
		binding := bindings[field]
		targetRaw, ok := properties[field]
		if !ok {
			return fmt.Errorf("%s: undeclared target field", field)
		}
		targetSchema := Schema(targetRaw.(map[string]any))
		var sourceSchema Schema
		switch binding.Kind {
		case "literal":
			if binding.Field != "" || binding.NodeID != "" {
				return fmt.Errorf("%s: literal binding cannot contain references", field)
			}
			if err := validateValue(targetSchema, binding.Value); err != nil {
				return fmt.Errorf("%s literal: %w", field, err)
			}
			continue
		case "workflowInput":
			if binding.Field == "" || binding.NodeID != "" || binding.Value != nil {
				return fmt.Errorf("%s: invalid workflowInput binding", field)
			}
			sourceSchema = plan.definition.InputSchema
		case "nodeOutput":
			if binding.Field == "" || binding.NodeID == "" || binding.Value != nil {
				return fmt.Errorf("%s: invalid nodeOutput binding", field)
			}
			source, exists := plan.nodes[binding.NodeID]
			if !exists {
				return fmt.Errorf("%s: unknown source node %q", field, binding.NodeID)
			}
			sourceSchema = source.registration.Descriptor.OutputSchema
			if onData != nil {
				onData(binding.NodeID, binding.Field, field)
			}
		default:
			return fmt.Errorf("%s: unknown binding kind %q", field, binding.Kind)
		}
		sourceProps, _ := sourceSchema["properties"].(map[string]any)
		raw, exists := sourceProps[binding.Field]
		if !exists {
			return fmt.Errorf("%s: undeclared source field %q", field, binding.Field)
		}
		if !compatibleTypes(raw.(map[string]any), targetSchema) {
			return fmt.Errorf("%s: source and target types conflict", field)
		}
	}
	return nil
}

func compatibleTypes(source, target Schema) bool {
	a, b := source["type"], target["type"]
	if a != b {
		return a == "integer" && b == "number"
	}
	if a == "array" {
		return compatibleTypes(source["items"].(map[string]any), target["items"].(map[string]any))
	}
	return true
}
func topologicalOrder(nodes map[string]compiledNode) ([]string, error) {
	counts := map[string]int{}
	children := map[string][]string{}
	ready := []string{}
	for id, node := range nodes {
		counts[id] = len(node.dependencies)
		if len(node.dependencies) == 0 {
			ready = append(ready, id)
		}
		for _, dep := range node.dependencies {
			children[dep] = append(children[dep], id)
		}
	}
	sort.Strings(ready)
	order := make([]string, 0, len(nodes))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		for _, child := range children[id] {
			counts[child]--
			if counts[child] == 0 {
				ready = append(ready, child)
			}
		}
		sort.Strings(ready)
	}
	if len(order) != len(nodes) {
		return nil, fmt.Errorf("dependencies: cycle detected")
	}
	return order, nil
}
func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	return sortedKeys(seen)
}
func sortDescriptors(descriptors []Descriptor) {
	sort.Slice(descriptors, func(i, j int) bool {
		a, b := descriptors[i], descriptors[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.TypeVersion < b.TypeVersion
	})
}

// View returns an isolated projection with the exact descriptor versions used.
func (p *Plan) View() View {
	view := View{Definition: p.definition, Edges: p.edges, NodeTypes: []Descriptor{}}
	seen := map[typeKey]bool{}
	for _, node := range p.nodes {
		d := node.registration.Descriptor
		key := typeKey{d.Type, d.TypeVersion}
		if !seen[key] {
			view.NodeTypes = append(view.NodeTypes, d)
			seen[key] = true
		}
	}
	sortDescriptors(view.NodeTypes)
	var result View
	_ = copyJSON(view, &result)
	return result
}

// ValidateInput checks a prospective run before it consumes execution capacity.
func (p *Plan) ValidateInput(input Object) error {
	return validateValue(p.definition.InputSchema, input)
}
