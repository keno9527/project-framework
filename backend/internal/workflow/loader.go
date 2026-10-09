package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const MaxFileBytes = 1 << 20

type workflowKey struct{ id, version string }

// Catalog is an immutable collection published only after every file compiles.
type Catalog struct{ plans map[workflowKey]*Plan }

// Find resolves an exact version; there is deliberately no implicit latest.
func (c *Catalog) Find(id, version string) (*Plan, bool) {
	if c == nil {
		return nil, false
	}
	plan, ok := c.plans[workflowKey{id, version}]
	return plan, ok
}

// Summaries returns stable workflow/version ordering without exposing plans.
func (c *Catalog) Summaries() []Summary {
	result := make([]Summary, 0, len(c.plans))
	for _, plan := range c.plans {
		d := plan.definition
		result = append(result, Summary{WorkflowID: d.WorkflowID, DefinitionVersion: d.DefinitionVersion, Title: d.Title, NodeCount: len(d.Nodes)})
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.WorkflowID != b.WorkflowID {
			return a.WorkflowID < b.WorkflowID
		}
		return a.DefinitionVersion < b.DefinitionVersion
	})
	return result
}

// LoadDir strictly loads direct YAML children in filename order. One invalid
// file prevents publishing any part of the catalog. Errors use basenames only.
func LoadDir(dir string, registry *Registry) (*Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("workflow directory: %w", filesystemCause(err))
	}
	catalog := &Catalog{plans: map[workflowKey]*Plan{}}
	files := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := filepath.Ext(entry.Name())
		if extension != ".yaml" && extension != ".yml" {
			continue
		}
		files++
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("file %s: symbolic links are not supported", entry.Name())
		}
		path := filepath.Join(dir, entry.Name())
		data, err := readLimited(path)
		if err != nil {
			return nil, fmt.Errorf("file %s: %w", entry.Name(), err)
		}
		definition, err := parseDefinition(data)
		if err != nil {
			return nil, fmt.Errorf("file %s: parse: %w", entry.Name(), err)
		}
		key := workflowKey{definition.WorkflowID, definition.DefinitionVersion}
		if _, exists := catalog.plans[key]; exists {
			return nil, fmt.Errorf("file %s: duplicate workflow version %s@%s", entry.Name(), key.id, key.version)
		}
		plan, err := Compile(definition, registry)
		if err != nil {
			return nil, fmt.Errorf("file %s: compile: %w", entry.Name(), err)
		}
		catalog.plans[key] = plan
	}
	if files == 0 {
		return nil, fmt.Errorf("workflow directory: no .yaml or .yml files")
	}
	return catalog, nil
}

func filesystemCause(err error) error {
	if pathErr, ok := err.(*os.PathError); ok {
		return pathErr.Err
	}
	return err
}
func readLimited(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, filesystemCause(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, filesystemCause(err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("must be a regular file")
	}
	if info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("exceeds 1 MiB limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return nil, filesystemCause(err)
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("exceeds 1 MiB limit")
	}
	return data, nil
}

func parseDefinition(data []byte) (Definition, error) {
	var root yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&root); err != nil {
		return Definition{}, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return Definition{}, err
		}
		return Definition{}, fmt.Errorf("multiple YAML documents are not supported")
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return Definition{}, fmt.Errorf("root must be an object")
	}
	if err := checkYAML(&root, "definition", 0); err != nil {
		return Definition{}, err
	}
	if err := checkYAMLBindingsAndPolicies(root.Content[0]); err != nil {
		return Definition{}, err
	}
	// Decode through JSON so YAML scalar coercion cannot turn an integer into
	// a string version or a quoted number into an integer execution policy.
	value, err := yamlJSONValue(root.Content[0])
	if err != nil {
		return Definition{}, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return Definition{}, fmt.Errorf("configuration must be JSON-compatible: %w", err)
	}
	var definition Definition
	strict := json.NewDecoder(bytes.NewReader(encoded))
	strict.DisallowUnknownFields()
	strict.UseNumber()
	if err := strict.Decode(&definition); err != nil {
		return Definition{}, err
	}
	return definition, nil
}

func checkYAML(node *yaml.Node, path string, depth int) error {
	if depth > 100 {
		return fmt.Errorf("%s: nesting exceeds 100 levels", path)
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("%s (line %d): aliases and anchors are not supported", path, node.Line)
	}
	switch node.Kind {
	case yaml.DocumentNode:
	case yaml.MappingNode:
		if node.Tag != "!!map" {
			return fmt.Errorf("%s: custom tags are not supported", path)
		}
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return fmt.Errorf("%s (line %d): object keys must be strings", path, key.Line)
			}
			if seen[key.Value] {
				return fmt.Errorf("%s.%s (line %d): duplicate key", path, key.Value, key.Line)
			}
			seen[key.Value] = true
			if err := checkYAML(node.Content[i+1], path+"."+key.Value, depth+1); err != nil {
				return err
			}
		}
		return nil
	case yaml.SequenceNode:
		if node.Tag != "!!seq" {
			return fmt.Errorf("%s: custom tags are not supported", path)
		}
	case yaml.ScalarNode:
		if !contains([]string{"!!str", "!!bool", "!!int", "!!float", "!!null"}, node.Tag) {
			return fmt.Errorf("%s (line %d): unsupported YAML tag", path, node.Line)
		}
	default:
		return fmt.Errorf("%s: unsupported YAML value", path)
	}
	for i, child := range node.Content {
		if err := checkYAML(child, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
			return err
		}
	}
	return nil
}

func yamlField(node *yaml.Node, name string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}
func checkYAMLBindingsAndPolicies(root *yaml.Node) error {
	if err := checkBindingMap(yamlField(root, "outputs"), "outputs"); err != nil {
		return err
	}
	nodes := yamlField(root, "nodes")
	if nodes == nil || nodes.Kind != yaml.SequenceNode {
		return nil
	}
	for i, node := range nodes.Content {
		path := fmt.Sprintf("nodes[%d]", i)
		if id := yamlField(node, "id"); id != nil {
			path = "node " + id.Value
		}
		if err := checkBindingMap(yamlField(node, "inputs"), path+".inputs"); err != nil {
			return err
		}
		for _, field := range []struct {
			node *yaml.Node
			name string
			max  int
		}{{yamlField(node, "timeoutMs"), "timeoutMs", 120000}, {yamlField(yamlField(node, "retry"), "maxAttempts"), "retry.maxAttempts", 3}, {yamlField(yamlField(node, "retry"), "backoffMs"), "retry.backoffMs", 10000}} {
			if field.node == nil {
				continue
			}
			var value int
			if field.node.Tag != "!!int" || field.node.Decode(&value) != nil || value < 1 || value > field.max {
				return fmt.Errorf("%s.%s (line %d): must be integer between 1 and %d", path, field.name, field.node.Line, field.max)
			}
		}
	}
	return nil
}
func checkBindingMap(node *yaml.Node, path string) error {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: must be an object", path)
	}
	for i := 0; i < len(node.Content); i += 2 {
		name, binding := node.Content[i].Value, node.Content[i+1]
		kindNode := yamlField(binding, "kind")
		if kindNode == nil {
			return fmt.Errorf("%s.%s.kind: required", path, name)
		}
		var required []string
		switch kindNode.Value {
		case "literal":
			required = []string{"kind", "value"}
		case "workflowInput":
			required = []string{"kind", "field"}
		case "nodeOutput":
			required = []string{"kind", "field", "nodeId"}
		default:
			return fmt.Errorf("%s.%s.kind: unknown binding kind", path, name)
		}
		for _, key := range required {
			if yamlField(binding, key) == nil {
				return fmt.Errorf("%s.%s.%s: required", path, name, key)
			}
		}
		if binding.Kind != yaml.MappingNode {
			return fmt.Errorf("%s.%s: must be an object", path, name)
		}
		for j := 0; j < len(binding.Content); j += 2 {
			key := binding.Content[j].Value
			if !contains(required, key) {
				return fmt.Errorf("%s.%s.%s: invalid field for %s", path, name, key, strings.TrimSpace(kindNode.Value))
			}
		}
	}
	return nil
}
