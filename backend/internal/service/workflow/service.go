// Package workflow contains the workflow use-case facade over the pure domain
// engine. It parses requests, applies the shared compiler and maps failures.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync/atomic"

	"project-framework/internal/jsonvalue"
	"project-framework/internal/workflow"
)

// Request size limits shared with the handler.
const (
	MaxRunRequestBytes   = 256 << 10
	MaxDraftRequestBytes = 2 << 20
)

// Kind is the stable, public error category returned to clients.
type Kind string

const (
	KindNotReady          Kind = "NOT_READY"
	KindInvalidRequest    Kind = "INVALID_REQUEST"
	KindDefinitionInvalid Kind = "DEFINITION_INVALID"
	KindInputInvalid      Kind = "INPUT_INVALID"
	KindWorkflowNotFound  Kind = "WORKFLOW_NOT_FOUND"
	KindRunNotFound       Kind = "RUN_NOT_FOUND"
	KindPayloadTooLarge   Kind = "PAYLOAD_TOO_LARGE"
	KindCapacityExceeded  Kind = "CAPACITY_EXCEEDED"
	KindInternal          Kind = "INTERNAL_ERROR"
)

// Failure is a domain failure carrying a public kind and a safe message.
type Failure struct {
	Kind    Kind
	Message string
	Cause   error
}

func (e *Failure) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Kind)
}

func (e *Failure) Unwrap() error { return e.Cause }

func fail(kind Kind, message string, cause error) *Failure {
	return &Failure{Kind: kind, Message: message, Cause: cause}
}

// Service implements workflow discovery, draft validation and execution.
type Service struct {
	catalog  *workflow.Catalog
	registry *workflow.Registry
	engine   *workflow.Engine
	ready    atomic.Bool
}

// New builds the service after all definitions have compiled.
func New(catalog *workflow.Catalog, registry *workflow.Registry, engine *workflow.Engine) *Service {
	s := &Service{catalog: catalog, registry: registry, engine: engine}
	s.ready.Store(true)
	return s
}

// SetReady controls admission during startup and shutdown.
func (s *Service) SetReady(ready bool) { s.ready.Store(ready) }

// Ready reports whether the service admits new work.
func (s *Service) Ready() bool { return s.ready.Load() }

// InstanceID identifies the in-memory engine instance.
func (s *Service) InstanceID() string { return s.engine.InstanceID() }

// Shutdown stops accepting work and waits for physical handlers until ctx.
func (s *Service) Shutdown(ctx context.Context) error { return s.engine.Shutdown(ctx) }

// NodeTypes returns the sorted node descriptor directory.
func (s *Service) NodeTypes() []workflow.Descriptor { return s.registry.Descriptors() }

// Workflows returns the loaded workflow version summaries.
func (s *Service) Workflows() []workflow.Summary { return s.catalog.Summaries() }

// GetWorkflow returns the redacted compiled view of one loaded version.
func (s *Service) GetWorkflow(workflowID, version string) (workflow.View, error) {
	plan, ok := s.catalog.Find(workflowID, version)
	if !ok {
		return workflow.View{}, fail(KindWorkflowNotFound, "workflow version is not loaded", nil)
	}
	return redactView(plan.View()), nil
}

// GetRun returns an isolated snapshot of a run.
func (s *Service) GetRun(runID string) (workflow.Snapshot, error) {
	snapshot, ok := s.engine.Snapshot(runID)
	if !ok {
		return workflow.Snapshot{}, fail(KindRunNotFound,
			"run not found; records are ephemeral and may have expired", nil)
	}
	return snapshot, nil
}

// ValidateDraft compiles a submitted definition without installing it and
// returns the redacted view plus deterministic YAML export.
func (s *Service) ValidateDraft(body []byte) (workflow.View, string, error) {
	plan, _, err := s.readDraft(body, false)
	if err != nil {
		return workflow.View{}, "", err
	}
	view := plan.View()
	exported, marshalErr := workflow.MarshalDefinitionYAML(view.Definition)
	if marshalErr != nil {
		return workflow.View{}, "", fail(KindInternal, "could not export workflow definition", marshalErr)
	}
	if len(exported) > workflow.MaxFileBytes {
		return workflow.View{}, "", fail(KindPayloadTooLarge,
			"exported workflow YAML exceeds 1 MiB; shorten the definition", nil)
	}
	return redactView(view), string(exported), nil
}

// StartTestRun compiles and executes a submitted draft with the given input.
func (s *Service) StartTestRun(body []byte) (workflow.Snapshot, error) {
	plan, input, err := s.readDraft(body, true)
	if err != nil {
		return workflow.Snapshot{}, err
	}
	return s.startPlan(plan, input)
}

// StartRun executes a precise loaded version identified in the request body.
func (s *Service) StartRun(body []byte) (workflow.Snapshot, error) {
	if !s.ready.Load() {
		return workflow.Snapshot{}, fail(KindNotReady, "service is shutting down", nil)
	}
	var request struct {
		WorkflowID        string          `json:"workflowId"`
		DefinitionVersion string          `json:"definitionVersion"`
		Input             workflow.Object `json:"input"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	err := decoder.Decode(&request)
	if err == nil {
		var extra any
		if trailing := decoder.Decode(&extra); trailing != io.EOF {
			if trailing == nil {
				err = errors.New("multiple JSON values")
			} else {
				err = trailing
			}
		}
	}
	// Note: decoding above cannot fail with a size error because the handler has
	// already bounded the body to MaxRunRequestBytes.
	if err != nil {
		return workflow.Snapshot{}, fail(KindInvalidRequest,
			"expected one JSON object with workflowId, definitionVersion and input", err)
	}
	if request.WorkflowID == "" || request.DefinitionVersion == "" || request.Input == nil {
		return workflow.Snapshot{}, fail(KindInvalidRequest,
			"workflowId, definitionVersion and input are required", nil)
	}
	plan, ok := s.catalog.Find(request.WorkflowID, request.DefinitionVersion)
	if !ok {
		return workflow.Snapshot{}, fail(KindWorkflowNotFound, "workflow version is not loaded", nil)
	}
	if err := plan.ValidateInput(request.Input); err != nil {
		return workflow.Snapshot{}, fail(KindInputInvalid, "input does not match the workflow schema", err)
	}
	return s.startPlan(plan, request.Input)
}

func (s *Service) startPlan(plan *workflow.Plan, input workflow.Object) (workflow.Snapshot, error) {
	snapshot, err := s.engine.Start(plan, input)
	if err != nil {
		var inputError *workflow.InputError
		switch {
		case errors.Is(err, workflow.ErrInputTooLarge):
			return workflow.Snapshot{}, fail(KindPayloadTooLarge, "encoded workflow input exceeds 256 KiB", err)
		case errors.As(err, &inputError):
			return workflow.Snapshot{}, fail(KindInputInvalid, "input does not match the workflow contract", err)
		case errors.Is(err, workflow.ErrCapacity):
			return workflow.Snapshot{}, fail(KindCapacityExceeded, "active run limit reached", err)
		case errors.Is(err, workflow.ErrClosed):
			return workflow.Snapshot{}, fail(KindNotReady, "service is shutting down", err)
		default:
			return workflow.Snapshot{}, fail(KindInternal, "could not start workflow", err)
		}
	}
	return snapshot, nil
}

// readDraft uses the same compiler as startup loading, but never installs the
// resulting plan in the catalog. Test runs own that immutable plan independently.
func (s *Service) readDraft(body []byte, withInput bool) (*workflow.Plan, workflow.Object, *Failure) {
	if !s.ready.Load() {
		return nil, nil, fail(KindNotReady, "service is shutting down", nil)
	}
	value, err := jsonvalue.Parse(body)
	if err != nil {
		return nil, nil, fail(KindInvalidRequest, diagnostic(err), err)
	}
	request, ok := value.(map[string]any)
	if !ok {
		return nil, nil, fail(KindInvalidRequest, "request must be an object", nil)
	}
	for key := range request {
		if key != "definition" && !(withInput && key == "input") {
			return nil, nil, fail(KindInvalidRequest, "request contains an unknown field", nil)
		}
	}
	rawDefinition, exists := request["definition"]
	if !exists {
		return nil, nil, fail(KindInvalidRequest, "definition is required", nil)
	}
	encodedDefinition, err := json.Marshal(rawDefinition)
	if err != nil {
		return nil, nil, fail(KindInvalidRequest, "definition must be JSON-compatible", err)
	}
	if len(encodedDefinition) > workflow.MaxFileBytes {
		return nil, nil, fail(KindPayloadTooLarge, "encoded definition exceeds 1 MiB", nil)
	}
	if err := rejectRedacted(rawDefinition, "definition"); err != nil {
		return nil, nil, fail(KindDefinitionInvalid, diagnostic(err), err)
	}
	definition, err := workflow.ParseJSONDefinition(encodedDefinition)
	if err != nil {
		return nil, nil, fail(KindDefinitionInvalid, diagnostic(err), err)
	}
	plan, err := workflow.Compile(definition, s.registry)
	if err != nil {
		return nil, nil, fail(KindDefinitionInvalid, diagnostic(err), err)
	}
	if !withInput {
		return plan, nil, nil
	}
	input, ok := request["input"].(map[string]any)
	if !ok {
		return nil, nil, fail(KindInvalidRequest, "input is required and must be an object", nil)
	}
	encodedInput, err := json.Marshal(input)
	if err != nil || len(encodedInput) > MaxRunRequestBytes {
		return nil, nil, fail(KindPayloadTooLarge, "encoded workflow input exceeds 256 KiB", err)
	}
	if err := plan.ValidateInput(input); err != nil {
		return nil, nil, fail(KindInputInvalid, diagnostic(err), err)
	}
	return plan, workflow.Object(input), nil
}

func diagnostic(err error) string {
	message := []rune(err.Error())
	if len(message) > 1024 {
		return string(message[:1024]) + "…"
	}
	return string(message)
}

func rejectRedacted(value any, path string) error {
	switch value := value.(type) {
	case string:
		if value == "[REDACTED]" {
			return fmt.Errorf("%s: replace the redacted placeholder before validating or running the draft", path)
		}
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := rejectRedacted(value[key], path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		for index, item := range value {
			if err := rejectRedacted(item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func redactView(view workflow.View) workflow.View {
	for i := range view.Nodes {
		view.Nodes[i].Config = redact(view.Nodes[i].Config)
		for name, binding := range view.Nodes[i].Inputs {
			if binding.Kind == "literal" {
				if sensitiveKey(name) {
					binding.Value = "[REDACTED]"
				} else {
					binding.Value = redactValue(binding.Value)
				}
				view.Nodes[i].Inputs[name] = binding
			}
		}
	}
	for name, binding := range view.Outputs {
		if binding.Kind == "literal" {
			if sensitiveKey(name) {
				binding.Value = "[REDACTED]"
			} else {
				binding.Value = redactValue(binding.Value)
			}
			view.Outputs[name] = binding
		}
	}
	return view
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	switch key {
	case "password", "secret", "clientsecret", "token", "accesstoken", "refreshtoken",
		"apikey", "authorization", "credential", "credentials":
		return true
	default:
		return false
	}
}

func redact(value workflow.Object) workflow.Object {
	for key, item := range value {
		if sensitiveKey(key) {
			value[key] = "[REDACTED]"
			continue
		}
		value[key] = redactValue(item)
	}
	return value
}

func redactValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return redact(value)
	case workflow.Object:
		return redact(value)
	case []any:
		for i, item := range value {
			value[i] = redactValue(item)
		}
		return value
	default:
		return value
	}
}
