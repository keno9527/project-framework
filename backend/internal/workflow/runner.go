package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const maxOutputBytes = 256 * 1024

// Options bounds service resources. Zero or negative values use V1 defaults.
type Options struct {
	MaxActiveRuns      int
	MaxNodesPerRun     int
	MaxConcurrentNodes int
	RunTimeout         time.Duration
	RetainedRuns       int
	Logger             *slog.Logger
}

func (o Options) defaults() Options {
	if o.MaxActiveRuns <= 0 {
		o.MaxActiveRuns = 8
	}
	if o.MaxNodesPerRun <= 0 {
		o.MaxNodesPerRun = 4
	}
	if o.MaxConcurrentNodes <= 0 {
		o.MaxConcurrentNodes = 16
	}
	if o.RunTimeout <= 0 {
		o.RunTimeout = 120 * time.Second
	}
	if o.RetainedRuns <= 0 {
		o.RetainedRuns = 100
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}

// Engine executes immutable plans within a service-owned lifecycle. It must not
// be copied. Shutdown waits for physical handlers until its caller's deadline.
type Engine struct {
	options    Options
	ctx        context.Context
	cancel     context.CancelFunc
	instanceID string
	slots      chan struct{}
	wg         sync.WaitGroup
	mu         sync.RWMutex
	closed     bool
	active     int
	runs       map[string]*Snapshot
	completed  []string
}

// NewEngine binds execution to serviceCtx, never to an HTTP request context.
func NewEngine(serviceCtx context.Context, opts Options) *Engine {
	opts = opts.defaults()
	ctx, cancel := context.WithCancel(serviceCtx)
	return &Engine{
		options: opts, ctx: ctx, cancel: cancel, instanceID: randomID(),
		slots: make(chan struct{}, opts.MaxConcurrentNodes), runs: make(map[string]*Snapshot),
	}
}

func randomID() string {
	var data [16]byte
	// crypto/rand.Read never returns an error on supported Go versions.
	_, _ = rand.Read(data[:])
	return hex.EncodeToString(data[:])
}

// InstanceID identifies this temporary in-memory engine instance.
func (e *Engine) InstanceID() string { return e.instanceID }

// Start validates input and atomically admits a run. The initial query snapshot
// is created before scheduling; callers receive an actual snapshot at return.
func (e *Engine) Start(plan *Plan, input Object) (Snapshot, error) {
	if plan == nil {
		return Snapshot{}, &InputError{Err: errors.New("plan is required")}
	}
	if err := validateValue(plan.definition.InputSchema, input); err != nil {
		return Snapshot{}, &InputError{Err: err}
	}
	// Marshal rejects non-JSON input before cloning and bounds programmatic calls.
	data, err := json.Marshal(input)
	if err != nil {
		return Snapshot{}, &InputError{Err: errors.New("input must be JSON-compatible")}
	}
	if len(data) > maxOutputBytes {
		return Snapshot{}, &InputError{Err: ErrInputTooLarge}
	}
	input = cloneObject(input)
	now := time.Now().UTC()
	s := Snapshot{RunID: randomID(), WorkflowID: plan.definition.WorkflowID,
		DefinitionVersion: plan.definition.DefinitionVersion, Status: StatusPending,
		InstanceID: e.instanceID, Ephemeral: true, CreatedAt: now, Nodes: make([]NodeSnapshot, 0, len(plan.order))}
	for _, id := range plan.order {
		s.Nodes = append(s.Nodes, NodeSnapshot{NodeID: id, NodeExecutionID: randomID(), Status: StatusPending, Attempts: []AttemptSnapshot{}})
	}
	e.mu.Lock()
	if e.closed || e.ctx.Err() != nil {
		e.mu.Unlock()
		return Snapshot{}, ErrClosed
	}
	if e.active >= e.options.MaxActiveRuns {
		e.mu.Unlock()
		return Snapshot{}, ErrCapacity
	}
	e.active++
	record := copySnapshot(s)
	e.runs[s.RunID] = &record
	e.wg.Add(1)
	e.mu.Unlock()
	go func() { defer e.wg.Done(); e.run(plan, input, s) }()
	// Keep the admitted record reachable even if rapid concurrent completions
	// evict its lookup entry before this call resumes.
	e.mu.RLock()
	result := copySnapshot(record)
	e.mu.RUnlock()
	return result, nil
}

// Snapshot returns an isolated copy; absent, evicted and previous-instance IDs
// are indistinguishable because V1 has no historical storage.
func (e *Engine) Snapshot(runID string) (Snapshot, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, exists := e.runs[runID]
	if !exists {
		return Snapshot{}, false
	}
	return copySnapshot(*s), true
}

// Shutdown is idempotent. A timeout stops waiting, not the physical handler.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	e.closed = true
	e.cancel()
	e.mu.Unlock()
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type executionEvent struct {
	nodeID    string
	attempt   int
	startedAt time.Time
	ack       chan struct{}
	output    Object
	err       *RunError
	retryable bool
}

func (e *Engine) run(plan *Plan, input Object, s Snapshot) {
	ctx, cancel := context.WithDeadline(e.ctx, s.CreatedAt.Add(e.options.RunTimeout))
	defer cancel()
	now := time.Now().UTC()
	s.Status, s.StartedAt = StatusRunning, &now
	e.save(s)
	indexes := make(map[string]int, len(s.Nodes))
	for i := range s.Nodes {
		indexes[s.Nodes[i].NodeID] = i
	}
	outputs := make(map[string]Object, len(s.Nodes))
	inflight := make(map[string]bool)
	retryAt := make(map[string]time.Time)
	events := make(chan executionEvent, 2*len(s.Nodes)+1)
	for {
		if err := ctx.Err(); err != nil {
			e.finishContext(&s, err)
			return
		}
		allSucceeded := true
		for i := range s.Nodes {
			n := &s.Nodes[i]
			if n.Status == StatusSucceeded {
				continue
			}
			allSucceeded = false
			if inflight[n.NodeID] || (n.Status != StatusPending && n.Status != StatusRetryWait) || len(inflight) >= e.options.MaxNodesPerRun {
				continue
			}
			if when, exists := retryAt[n.NodeID]; exists && time.Now().Before(when) {
				continue
			}
			cn := plan.nodes[n.NodeID]
			ready := true
			for _, dep := range cn.dependencies {
				if s.Nodes[indexes[dep]].Status != StatusSucceeded {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			nodeInput, err := resolveBindings(cn.spec.Inputs, input, outputs)
			if err == nil {
				err = validateValue(cn.registration.Descriptor.InputSchema, nodeInput)
			}
			if err != nil {
				n.Status = StatusFailed
				n.Error = &RunError{Code: "NODE_INPUT_INVALID", Message: "resolved node input violates its contract", NodeID: n.NodeID}
				finished := time.Now().UTC()
				n.FinishedAt = &finished
				e.finish(&s, StatusFailed, n.Error)
				return
			}
			inflight[n.NodeID] = true
			delete(retryAt, n.NodeID)
			meta := ExecutionMeta{RunID: s.RunID, NodeID: n.NodeID, NodeExecutionID: n.NodeExecutionID, Attempt: n.Attempt + 1, IdempotencyKey: s.RunID + ":" + n.NodeExecutionID}
			e.wg.Add(1)
			go func() {
				defer e.wg.Done()
				e.execute(ctx, cn, Call{Input: nodeInput, Config: cloneObject(cn.spec.Config), Meta: meta}, events)
			}()
		}
		if allSucceeded {
			output, err := resolveBindings(plan.definition.Outputs, input, outputs)
			if err == nil {
				err = validateValue(plan.definition.OutputSchema, output)
			}
			// Final projection and validation are part of the run deadline too.
			// Cancellation observed here takes precedence over their result.
			if contextErr := ctx.Err(); contextErr != nil {
				e.finishContext(&s, contextErr)
				return
			}
			if err != nil {
				e.finish(&s, StatusFailed, &RunError{Code: "WORKFLOW_OUTPUT_INVALID", Message: "workflow output violates its contract"})
			} else {
				s.Output = output
				e.finish(&s, StatusSucceeded, nil)
			}
			return
		}
		var timer *time.Timer
		var timerC <-chan time.Time
		if len(inflight) < e.options.MaxNodesPerRun {
			var next time.Time
			for _, t := range retryAt {
				if next.IsZero() || t.Before(next) {
					next = t
				}
			}
			if !next.IsZero() {
				timer = time.NewTimer(time.Until(next))
				timerC = timer.C
			}
		}
		var event executionEvent
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			continue
		case <-timerC:
			continue
		case event = <-events:
			if timer != nil {
				timer.Stop()
			}
		}
		if ctx.Err() != nil {
			continue
		}
		n := &s.Nodes[indexes[event.nodeID]]
		if event.ack != nil {
			n.Status, n.Attempt = StatusRunning, event.attempt
			n.Error = nil
			if n.StartedAt == nil {
				n.StartedAt = &event.startedAt
			}
			n.Attempts = append(n.Attempts, AttemptSnapshot{Attempt: event.attempt, Status: StatusRunning, StartedAt: event.startedAt})
			e.save(s)
			close(event.ack)
			continue
		}
		if n.Attempt != event.attempt || n.Status != StatusRunning {
			continue
		}
		delete(inflight, event.nodeID)
		finished := time.Now().UTC()
		a := &n.Attempts[len(n.Attempts)-1]
		a.FinishedAt, a.Error = &finished, event.err
		cn := plan.nodes[event.nodeID]
		if event.err == nil {
			a.Status, n.Status, n.FinishedAt = StatusSucceeded, StatusSucceeded, &finished
			outputs[n.NodeID] = event.output
		} else {
			a.Status = StatusFailed
			if event.err.Code == "NODE_TIMEOUT" {
				a.Status = StatusTimedOut
			}
			n.Error = event.err
			if event.retryable && cn.registration.Descriptor.RetrySafe && n.Attempt < cn.spec.Retry.MaxAttempts {
				n.Status = StatusRetryWait
				retryAt[n.NodeID] = finished.Add(time.Duration(cn.spec.Retry.BackoffMS) * time.Millisecond)
			} else {
				n.Status, n.FinishedAt = a.Status, &finished
				e.logNode(s, *n)
				e.finish(&s, StatusFailed, event.err)
				return
			}
		}
		e.logNode(s, *n)
		e.save(s)
	}
}

func (e *Engine) finishContext(s *Snapshot, cause error) {
	status, code := StatusCancelled, "RUN_CANCELLED"
	if errors.Is(cause, context.DeadlineExceeded) {
		status, code = StatusTimedOut, "RUN_TIMEOUT"
	}
	e.finish(s, status, &RunError{Code: code, Message: "workflow execution ended"})
}

func (e *Engine) finish(s *Snapshot, status string, err *RunError) {
	now := time.Now().UTC()
	s.Status, s.Error, s.FinishedAt = status, err, &now
	for i := range s.Nodes {
		n := &s.Nodes[i]
		if terminal(n.Status) {
			continue
		}
		n.Status = StatusCancelled
		if n.Attempt == 0 {
			n.Status = StatusSkipped
		} else {
			code := "NODE_CANCELLED"
			if err != nil && (err.Code == "RUN_TIMEOUT" || err.Code == "RUN_CANCELLED") {
				code = err.Code
			}
			n.Error = &RunError{Code: code, Message: "node cancelled because workflow terminated", NodeID: n.NodeID, Attempt: n.Attempt}
		}
		n.FinishedAt = &now
		if len(n.Attempts) > 0 {
			a := &n.Attempts[len(n.Attempts)-1]
			if a.Status == StatusRunning {
				a.Status, a.FinishedAt, a.Error = StatusCancelled, &now, copyError(n.Error)
				e.logNode(*s, *n)
			}
		}
	}
	e.save(*s)
	e.options.Logger.Info("workflow completed", "workflowId", s.WorkflowID, "definitionVersion", s.DefinitionVersion,
		"runId", s.RunID, "status", s.Status, "durationMs", now.Sub(s.CreatedAt).Milliseconds(), "errorCode", errorCode(err))
}

func errorCode(err *RunError) string {
	if err == nil {
		return ""
	}
	return err.Code
}

func (e *Engine) logNode(s Snapshot, n NodeSnapshot) {
	a := n.Attempts[len(n.Attempts)-1]
	e.options.Logger.Info("node attempt completed", "workflowId", s.WorkflowID, "definitionVersion", s.DefinitionVersion,
		"runId", s.RunID, "nodeId", n.NodeID, "nodeExecutionId", n.NodeExecutionID, "attempt", n.Attempt,
		"status", a.Status, "durationMs", a.FinishedAt.Sub(a.StartedAt).Milliseconds(), "errorCode", errorCode(a.Error))
}

func resolveBindings(bindings map[string]Binding, input Object, outputs map[string]Object) (Object, error) {
	result := make(Object, len(bindings))
	for field, b := range bindings {
		var v any
		var ok bool
		switch b.Kind {
		case "literal":
			v, ok = b.Value, true
		case "workflowInput":
			v, ok = input[b.Field]
		case "nodeOutput":
			v, ok = outputs[b.NodeID][b.Field]
		default:
			return nil, fmt.Errorf("unknown binding kind %q", b.Kind)
		}
		if !ok {
			return nil, fmt.Errorf("missing bound field %q", field)
		}
		result[field] = v
	}
	return cloneObject(result), nil
}

func (e *Engine) execute(ctx context.Context, cn compiledNode, call Call, events chan<- executionEvent) {
	select {
	case e.slots <- struct{}{}:
	case <-ctx.Done():
		return
	}
	if ctx.Err() != nil {
		<-e.slots
		return
	}
	nodeCtx, cancel := context.WithTimeout(ctx, time.Duration(cn.spec.TimeoutMS)*time.Millisecond)
	defer cancel()
	started := executionEvent{nodeID: call.Meta.NodeID, attempt: call.Meta.Attempt, startedAt: time.Now().UTC(), ack: make(chan struct{})}
	select {
	case events <- started:
	case <-ctx.Done():
		<-e.slots
		return
	}
	select {
	case <-started.ack:
	case <-ctx.Done():
		<-e.slots
		return
	}
	if nodeCtx.Err() != nil {
		<-e.slots
		e.sendResult(ctx, events, call.Meta, nil, &RunError{Code: "NODE_TIMEOUT", Message: "node attempt exceeded its deadline"}, false)
		return
	}
	result := make(chan executionEvent, 1)
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		output, err, retry := invoke(cn, nodeCtx, call)
		// The physical slot is released only once the real handler has returned.
		<-e.slots
		result <- executionEvent{output: output, err: err, retryable: retry}
	}()
	select {
	case r := <-result:
		if nodeCtx.Err() != nil {
			e.sendResult(ctx, events, call.Meta, nil, &RunError{Code: "NODE_TIMEOUT", Message: "node attempt exceeded its deadline"}, false)
			return
		}
		e.sendResult(ctx, events, call.Meta, r.output, r.err, r.retryable)
	case <-nodeCtx.Done():
		if ctx.Err() == nil {
			e.options.Logger.Warn("node exceeded deadline; physical work may still be running", "runId", call.Meta.RunID, "nodeId", call.Meta.NodeID, "nodeExecutionId", call.Meta.NodeExecutionID, "attempt", call.Meta.Attempt)
			e.sendResult(ctx, events, call.Meta, nil, &RunError{Code: "NODE_TIMEOUT", Message: "node attempt exceeded its deadline"}, false)
		}
	}
}

func (e *Engine) sendResult(ctx context.Context, events chan<- executionEvent, meta ExecutionMeta, output Object, err *RunError, retry bool) {
	if err != nil {
		err.NodeID, err.Attempt = meta.NodeID, meta.Attempt
	}
	select {
	case events <- executionEvent{nodeID: meta.NodeID, attempt: meta.Attempt, output: output, err: err, retryable: retry}:
	case <-ctx.Done():
	}
}

func invoke(cn compiledNode, ctx context.Context, call Call) (output Object, failure *RunError, retry bool) {
	defer func() {
		if recover() != nil {
			output = nil
			failure = &RunError{Code: "NODE_PANIC", Message: "node handler panicked"}
			retry = false
		}
	}()
	output, err := cn.registration.Handler(ctx, call)
	if err != nil {
		return nil, &RunError{Code: "NODE_EXECUTION_FAILED", Message: "node handler returned an error"}, isRetryable(err)
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, &RunError{Code: "NODE_OUTPUT_INVALID", Message: "node output is not valid JSON data"}, false
	}
	if len(data) > maxOutputBytes {
		return nil, &RunError{Code: "NODE_OUTPUT_TOO_LARGE", Message: "node output exceeds 256 KiB"}, false
	}
	if err = validateValue(cn.registration.Descriptor.OutputSchema, output); err != nil {
		return nil, &RunError{Code: "NODE_OUTPUT_INVALID", Message: "node output violates its contract"}, false
	}
	return cloneObject(output), nil, false
}
