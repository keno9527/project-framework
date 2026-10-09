// Package workflow loads, validates and executes versioned static workflows.
package workflow

import "context"

// Object is a JSON-compatible object. Callers must not mutate objects after
// handing them to the engine; inputs and query results are copied at boundaries.
type Object map[string]any

// Schema describes the supported subset of JSON Schema 2020-12.
type Schema map[string]any

// Definition is the configuration format for one workflow version.
type Definition struct {
	APIVersion        string             `json:"apiVersion" yaml:"apiVersion"`
	WorkflowID        string             `json:"workflowId" yaml:"workflowId"`
	DefinitionVersion string             `json:"definitionVersion" yaml:"definitionVersion"`
	Title             string             `json:"title" yaml:"title"`
	InputSchema       Schema             `json:"inputSchema" yaml:"inputSchema"`
	OutputSchema      Schema             `json:"outputSchema" yaml:"outputSchema"`
	Nodes             []NodeSpec         `json:"nodes" yaml:"nodes"`
	Outputs           map[string]Binding `json:"outputs" yaml:"outputs"`
}

// Binding names a literal, workflow input field, or upstream output field.
type Binding struct {
	Kind   string `json:"kind" yaml:"kind"`
	Value  any    `json:"value,omitempty" yaml:"value,omitempty"`
	Field  string `json:"field,omitempty" yaml:"field,omitempty"`
	NodeID string `json:"nodeId,omitempty" yaml:"nodeId,omitempty"`
}

// RetryPolicy counts the initial attempt in MaxAttempts.
type RetryPolicy struct {
	MaxAttempts int `json:"maxAttempts" yaml:"maxAttempts"`
	BackoffMS   int `json:"backoffMs" yaml:"backoffMs"`
}

// NodeSpec configures one occurrence of a registered node type.
type NodeSpec struct {
	ID          string             `json:"id" yaml:"id"`
	Type        string             `json:"type" yaml:"type"`
	TypeVersion string             `json:"typeVersion" yaml:"typeVersion"`
	Title       string             `json:"title,omitempty" yaml:"title,omitempty"`
	Config      Object             `json:"config" yaml:"config"`
	Inputs      map[string]Binding `json:"inputs" yaml:"inputs"`
	DependsOn   []string           `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	TimeoutMS   int                `json:"timeoutMs" yaml:"timeoutMs"`
	Retry       RetryPolicy        `json:"retry" yaml:"retry"`
}

// UIHints are presentation hints, not executable frontend content.
type UIHints struct {
	IconKey    string   `json:"iconKey,omitempty"`
	FieldOrder []string `json:"fieldOrder,omitempty"`
	Advanced   []string `json:"advanced,omitempty"`
}

// Descriptor is the versioned, serializable contract of a node type.
type Descriptor struct {
	Type         string  `json:"type"`
	TypeVersion  string  `json:"typeVersion"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	Category     string  `json:"category"`
	ConfigSchema Schema  `json:"configSchema"`
	InputSchema  Schema  `json:"inputSchema"`
	OutputSchema Schema  `json:"outputSchema"`
	RetrySafe    bool    `json:"retrySafe"`
	UIHints      UIHints `json:"uiHints,omitempty"`
}

// ExecutionMeta identifies a logical node execution and its current attempt.
type ExecutionMeta struct {
	RunID           string `json:"runId"`
	NodeID          string `json:"nodeId"`
	NodeExecutionID string `json:"nodeExecutionId"`
	Attempt         int    `json:"attempt"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

// Call carries isolated business data and explicit execution metadata.
type Call struct {
	Input  Object
	Config Object
	Meta   ExecutionMeta
}

// Handler performs one attempt. It must honor cancellation and be concurrency
// safe. External side effects must implement their own idempotency contract.
type Handler func(context.Context, Call) (Object, error)

// Registration binds a node's contract to its implementation.
type Registration struct {
	Descriptor Descriptor
	Handler    Handler
}

// FieldMapping records why a data dependency exists.
type FieldMapping struct {
	SourceField string `json:"sourceField"`
	TargetField string `json:"targetField"`
}

// Edge is a normalized, directed dependency between two node instances.
type Edge struct {
	ID       string         `json:"id"`
	Source   string         `json:"source"`
	Target   string         `json:"target"`
	Reasons  []string       `json:"reasons"`
	Mappings []FieldMapping `json:"mappings"`
}

// View is a safe projection of a compiled plan for the read-only UI.
type View struct {
	Definition
	Edges     []Edge       `json:"edges"`
	NodeTypes []Descriptor `json:"nodeTypes"`
}

// Summary identifies a loaded workflow version.
type Summary struct {
	WorkflowID        string `json:"workflowId"`
	DefinitionVersion string `json:"definitionVersion"`
	Title             string `json:"title"`
	NodeCount         int    `json:"nodeCount"`
}

// Plan is immutable after compilation. Only copied projections are exported.
type Plan struct {
	definition Definition
	nodes      map[string]compiledNode
	order      []string
	edges      []Edge
}

type compiledNode struct {
	spec         NodeSpec
	registration Registration
	dependencies []string
}
