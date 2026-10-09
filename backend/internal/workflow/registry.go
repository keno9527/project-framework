package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

type typeKey struct{ name, version string }

// Registry holds explicitly registered node implementations. A compiled plan
// captures registrations, so later registry mutations never alter an existing plan.
type Registry struct {
	mu            sync.RWMutex
	registrations map[typeKey]Registration
	frozen        bool
}

// NewRegistry creates an empty registration directory.
func NewRegistry() *Registry { return &Registry{registrations: make(map[typeKey]Registration)} }

var nodeTypePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)

// Register validates and snapshots the descriptor before registering a handler.
func (r *Registry) Register(reg Registration) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	d := reg.Descriptor
	if !nodeTypePattern.MatchString(d.Type) {
		return fmt.Errorf("node type: invalid identifier")
	}
	for _, field := range []struct{ name, value string }{{"typeVersion", d.TypeVersion}, {"title", d.Title}, {"description", d.Description}, {"category", d.Category}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("node %s: %s is required", d.Type, field.name)
		}
	}
	if reg.Handler == nil {
		return fmt.Errorf("node %s: handler is required", d.Type)
	}
	var snapshot Descriptor
	if err := copyJSON(d, &snapshot); err != nil {
		return fmt.Errorf("node %s: descriptor: %w", d.Type, err)
	}
	for _, field := range []struct {
		name   string
		schema Schema
	}{{"configSchema", snapshot.ConfigSchema}, {"inputSchema", snapshot.InputSchema}, {"outputSchema", snapshot.OutputSchema}} {
		if err := ValidateSchema(field.schema); err != nil {
			return fmt.Errorf("node %s.%s: %w", d.Type, field.name, err)
		}
		if field.schema["type"] != "object" {
			return fmt.Errorf("node %s.%s: root must be object", d.Type, field.name)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("registry is frozen")
	}
	if r.registrations == nil {
		r.registrations = make(map[typeKey]Registration)
	}
	key := typeKey{d.Type, d.TypeVersion}
	if _, exists := r.registrations[key]; exists {
		return fmt.Errorf("node %s@%s: duplicate registration", d.Type, d.TypeVersion)
	}
	reg.Descriptor = snapshot
	r.registrations[key] = reg
	return nil
}

// Freeze prevents registrations after the application becomes ready.
func (r *Registry) Freeze() { r.mu.Lock(); defer r.mu.Unlock(); r.frozen = true }

func (r *Registry) lookup(name, version string) (Registration, bool) {
	if r == nil {
		return Registration{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	registration, ok := r.registrations[typeKey{name, version}]
	return registration, ok
}

// Descriptors returns sorted, isolated metadata for the node directory.
func (r *Registry) Descriptors() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Descriptor, 0, len(r.registrations))
	for _, reg := range r.registrations {
		result = append(result, cloneDescriptor(reg.Descriptor))
	}
	sortDescriptors(result)
	return result
}

func cloneDescriptor(d Descriptor) Descriptor {
	var result Descriptor
	_ = copyJSON(d, &result)
	return result
}

// Adapt bridges a strongly typed business function to the engine's object model.
// Input and configuration are decoded independently; execution metadata remains explicit.
func Adapt[I, C, O any](execute func(context.Context, I, C, ExecutionMeta) (O, error)) Handler {
	if execute == nil {
		return nil
	}
	return func(ctx context.Context, call Call) (Object, error) {
		var input I
		var config C
		if err := decodeTyped(call.Input, &input); err != nil {
			return nil, fmt.Errorf("decode node input: %w", err)
		}
		if err := decodeTyped(call.Config, &config); err != nil {
			return nil, fmt.Errorf("decode node config: %w", err)
		}
		output, err := execute(ctx, input, config, call.Meta)
		if err != nil {
			return nil, err
		}
		var object Object
		if err := copyJSON(output, &object); err != nil {
			return nil, fmt.Errorf("encode node output: %w", err)
		}
		if object == nil {
			return nil, fmt.Errorf("node output must be an object")
		}
		return object, nil
	}
}
func decodeTyped(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	return decoder.Decode(target)
}
