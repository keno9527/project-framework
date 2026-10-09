package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func runtimeSchema(fields ...string) Schema {
	properties := map[string]any{}
	required := make([]any, 0, len(fields))
	for _, f := range fields {
		properties[f] = map[string]any{"type": "string"}
		required = append(required, f)
	}
	return Schema{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func runtimePlan(t *testing.T, specs []NodeSpec, handlers map[string]Handler) *Plan {
	t.Helper()
	r := NewRegistry()
	for i := range specs {
		spec := &specs[i]
		spec.Type, spec.TypeVersion = spec.ID, "1"
		if spec.Inputs == nil {
			spec.Inputs = map[string]Binding{"value": {Kind: "workflowInput", Field: "value"}}
		}
		fields := sortedKeys(spec.Inputs)
		err := r.Register(Registration{Descriptor: Descriptor{Type: spec.ID, TypeVersion: "1", Title: spec.ID, Description: "runtime test", Category: "test",
			InputSchema: runtimeSchema(fields...), OutputSchema: runtimeSchema("value"), ConfigSchema: runtimeSchema(), RetrySafe: true}, Handler: handlers[spec.ID]})
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err := Compile(Definition{APIVersion: "workflow/v1", WorkflowID: "runtime-test", DefinitionVersion: "1", Title: "Runtime test", InputSchema: runtimeSchema("value"), OutputSchema: runtimeSchema("value"), Nodes: specs,
		Outputs: map[string]Binding{"value": {Kind: "nodeOutput", NodeID: specs[len(specs)-1].ID, Field: "value"}}}, r)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func runtimeEngine(t *testing.T, options Options) *Engine {
	t.Helper()
	options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	e := NewEngine(context.Background(), options)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := e.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	return e
}

func runtimeStart(t *testing.T, e *Engine, p *Plan, value string) Snapshot {
	t.Helper()
	s, err := e.Start(p, Object{"value": value})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func runtimeAwait(t *testing.T, e *Engine, id string, predicate func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		s, ok := e.Snapshot(id)
		if ok && predicate(s) {
			return s
		}
		select {
		case <-deadline.C:
			t.Fatalf("run %s did not reach expected state: %+v", id, s)
		case <-ticker.C:
		}
	}
}

func runtimeDone(t *testing.T, e *Engine, id string) Snapshot {
	t.Helper()
	return runtimeAwait(t, e, id, func(s Snapshot) bool { return terminal(s.Status) })
}

func receiveRuntime(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
		return ""
	}
}

func TestRuntimeParallelJoinAndDetachedSnapshots(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	var joined atomic.Bool
	handlers := map[string]Handler{}
	for _, id := range []string{"left", "right"} {
		handlers[id] = func(ctx context.Context, call Call) (Object, error) {
			started <- call.Meta.NodeID
			select {
			case <-release:
				return Object{"value": call.Input["value"].(string) + call.Meta.NodeID}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	handlers["join"] = func(_ context.Context, c Call) (Object, error) {
		joined.Store(true)
		return Object{"value": c.Input["left"].(string) + "/" + c.Input["right"].(string)}, nil
	}
	p := runtimePlan(t, []NodeSpec{{ID: "left"}, {ID: "right"}, {ID: "join", Inputs: map[string]Binding{"left": {Kind: "nodeOutput", NodeID: "left", Field: "value"}, "right": {Kind: "nodeOutput", NodeID: "right", Field: "value"}}}}, handlers)
	e := runtimeEngine(t, Options{MaxNodesPerRun: 2})
	s := runtimeStart(t, e, p, "x")
	a, b := receiveRuntime(t, started), receiveRuntime(t, started)
	if a == b {
		t.Fatal("branches did not execute independently")
	}
	if joined.Load() {
		t.Fatal("join started before dependencies completed")
	}
	close(release)
	final := runtimeDone(t, e, s.RunID)
	if final.Status != StatusSucceeded || final.Output["value"] != "xleft/xright" {
		t.Fatalf("unexpected result: %+v", final)
	}
	final.Output["value"] = "mutated"
	final.Nodes[0].Status = "corrupt"
	final.Nodes[0].Attempts[0].Status = "corrupt"
	*final.FinishedAt = time.Time{}
	again, _ := e.Snapshot(s.RunID)
	if again.Output["value"] != "xleft/xright" || again.Nodes[0].Status != StatusSucceeded || again.FinishedAt.IsZero() || again.Nodes[0].Attempts[0].Status != StatusSucceeded {
		t.Fatalf("snapshot shares mutable data: %+v", again)
	}
}

func TestRuntimeRetriesUseStableIdentityAndPreserveAttempts(t *testing.T) {
	var calls []ExecutionMeta
	var mu sync.Mutex
	p := runtimePlan(t, []NodeSpec{{ID: "retry", Retry: RetryPolicy{MaxAttempts: 3, BackoffMS: 1}}}, map[string]Handler{"retry": func(_ context.Context, c Call) (Object, error) {
		mu.Lock()
		calls = append(calls, c.Meta)
		mu.Unlock()
		if c.Meta.Attempt < 2 {
			return nil, Retryable(errors.New("transient"))
		}
		return c.Input, nil
	}})
	e := runtimeEngine(t, Options{})
	s := runtimeStart(t, e, p, "ok")
	final := runtimeDone(t, e, s.RunID)
	if final.Status != StatusSucceeded || final.Nodes[0].Attempt != 2 || len(final.Nodes[0].Attempts) != 2 {
		t.Fatalf("unexpected retry result: %+v", final)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0].IdempotencyKey != calls[1].IdempotencyKey || calls[0].NodeExecutionID != calls[1].NodeExecutionID || calls[0].Attempt != 1 || calls[1].Attempt != 2 {
		t.Fatalf("retry identity changed: %+v", calls)
	}
	if final.Nodes[0].Attempts[0].Error.Code != "NODE_EXECUTION_FAILED" || final.Nodes[0].Attempts[1].Error != nil {
		t.Fatal("attempt history lost")
	}
}

func TestRuntimeTerminalErrorsDoNotRetry(t *testing.T) {
	cases := []struct {
		name, code string
		handler    Handler
	}{
		{"permanent", "NODE_EXECUTION_FAILED", func(context.Context, Call) (Object, error) { return nil, errors.New("permanent") }},
		{"panic", "NODE_PANIC", func(context.Context, Call) (Object, error) { panic("secret details") }},
		{"invalid", "NODE_OUTPUT_INVALID", func(context.Context, Call) (Object, error) { return Object{"value": 7}, nil }},
		{"too-large", "NODE_OUTPUT_TOO_LARGE", func(context.Context, Call) (Object, error) {
			return Object{"value": strings.Repeat("a", maxOutputBytes)}, nil
		}},
		{"non-json", "NODE_OUTPUT_INVALID", func(context.Context, Call) (Object, error) { return Object{"value": make(chan int)}, nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var count atomic.Int32
			var downstream atomic.Bool
			p := runtimePlan(t, []NodeSpec{{ID: "fail", Retry: RetryPolicy{MaxAttempts: 3, BackoffMS: 1}}, {ID: "after", DependsOn: []string{"fail"}}}, map[string]Handler{"fail": func(ctx context.Context, c Call) (Object, error) { count.Add(1); return tc.handler(ctx, c) }, "after": func(_ context.Context, c Call) (Object, error) { downstream.Store(true); return c.Input, nil }})
			e := runtimeEngine(t, Options{})
			s := runtimeStart(t, e, p, "ok")
			final := runtimeDone(t, e, s.RunID)
			if final.Status != StatusFailed || final.Error.Code != tc.code || count.Load() != 1 || downstream.Load() {
				t.Fatalf("wrong failure result: %+v calls=%d", final, count.Load())
			}
			for _, n := range final.Nodes {
				if n.NodeID == "after" && n.Status != StatusSkipped {
					t.Fatalf("downstream not skipped: %+v", n)
				}
			}
		})
	}
}

func TestRuntimeRetryExhaustion(t *testing.T) {
	p := runtimePlan(t, []NodeSpec{{ID: "retry", Retry: RetryPolicy{MaxAttempts: 3, BackoffMS: 1}}}, map[string]Handler{"retry": func(context.Context, Call) (Object, error) { return nil, Retryable(errors.New("temporary")) }})
	e := runtimeEngine(t, Options{})
	s := runtimeStart(t, e, p, "x")
	final := runtimeDone(t, e, s.RunID)
	if final.Status != StatusFailed || final.Nodes[0].Attempt != 3 {
		t.Fatalf("wrong exhausted run: %+v", final)
	}
}

func TestRuntimeTimeoutRetainsPhysicalSlotAndIgnoresLateSuccess(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	var released sync.Once
	defer released.Do(func() { close(release) })
	p := runtimePlan(t, []NodeSpec{{ID: "wait", TimeoutMS: 25}}, map[string]Handler{"wait": func(_ context.Context, c Call) (Object, error) {
		started <- c.Input["value"].(string)
		<-release
		return c.Input, nil
	}})
	e := runtimeEngine(t, Options{MaxConcurrentNodes: 1, RunTimeout: time.Second})
	s1 := runtimeStart(t, e, p, "one")
	if receiveRuntime(t, started) != "one" {
		t.Fatal("wrong first invocation")
	}
	final := runtimeDone(t, e, s1.RunID)
	if final.Status != StatusFailed || final.Error.Code != "NODE_TIMEOUT" || final.Nodes[0].Status != StatusTimedOut {
		t.Fatalf("wrong timeout: %+v", final)
	}
	s2 := runtimeStart(t, e, p, "two")
	select {
	case <-started:
		t.Fatal("physical capacity released before handler returned")
	case <-time.After(40 * time.Millisecond):
	}
	waiting, _ := e.Snapshot(s2.RunID)
	if waiting.Nodes[0].Attempt != 0 {
		t.Fatal("node deadline started while waiting for physical capacity")
	}
	released.Do(func() { close(release) })
	if receiveRuntime(t, started) != "two" {
		t.Fatal("wrong second invocation")
	}
	if next := runtimeDone(t, e, s2.RunID); next.Status != StatusSucceeded {
		t.Fatalf("second invocation timed out in queue: %+v", next)
	}
	again, _ := e.Snapshot(s1.RunID)
	if again.Status != StatusFailed || again.Error.Code != "NODE_TIMEOUT" {
		t.Fatal("late success overwrote terminal run")
	}
}

func TestRuntimeAdmissionAndMultiRunIsolation(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	p := runtimePlan(t, []NodeSpec{{ID: "block"}}, map[string]Handler{"block": func(ctx context.Context, c Call) (Object, error) {
		started <- c.Input["value"].(string)
		select {
		case <-release:
			return c.Input, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})
	e := runtimeEngine(t, Options{MaxActiveRuns: 2})
	one := runtimeStart(t, e, p, "one")
	two := runtimeStart(t, e, p, "two")
	receiveRuntime(t, started)
	receiveRuntime(t, started)
	if _, err := e.Start(p, Object{"value": "three"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("expected capacity error, got %v", err)
	}
	if _, err := e.Start(p, Object{"value": 5}); err == nil {
		t.Fatal("invalid input admitted")
	}
	close(release)
	if s := runtimeDone(t, e, one.RunID); s.Output["value"] != "one" {
		t.Fatal("first input leaked")
	}
	if s := runtimeDone(t, e, two.RunID); s.Output["value"] != "two" {
		t.Fatal("second input leaked")
	}
	three := runtimeStart(t, e, p, "three")
	if s := runtimeDone(t, e, three.RunID); s.Status != StatusSucceeded {
		t.Fatal("capacity was not released")
	}
}

func TestRuntimeShutdownCancelsAndRespectsDeadline(t *testing.T) {
	started := make(chan string, 1)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	p := runtimePlan(t, []NodeSpec{{ID: "blocked"}}, map[string]Handler{"blocked": func(_ context.Context, c Call) (Object, error) { started <- "started"; <-release; return c.Input, nil }})
	e := runtimeEngine(t, Options{})
	s := runtimeStart(t, e, p, "x")
	receiveRuntime(t, started)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := e.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown did not respect caller deadline: %v", err)
	}
	final := runtimeDone(t, e, s.RunID)
	if final.Status != StatusCancelled || final.Nodes[0].Status != StatusCancelled {
		t.Fatalf("not cancelled: %+v", final)
	}
	if _, err := e.Start(p, Object{"value": "x"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("accepted after shutdown: %v", err)
	}
	once.Do(func() { close(release) })
}

func TestRuntimeOverallDeadlineIncludesBackoff(t *testing.T) {
	p := runtimePlan(t, []NodeSpec{{ID: "retry", Retry: RetryPolicy{MaxAttempts: 3, BackoffMS: 1000}}}, map[string]Handler{"retry": func(context.Context, Call) (Object, error) { return nil, Retryable(errors.New("temporary")) }})
	e := runtimeEngine(t, Options{RunTimeout: 30 * time.Millisecond})
	s := runtimeStart(t, e, p, "x")
	final := runtimeDone(t, e, s.RunID)
	if final.Status != StatusTimedOut || final.Nodes[0].Attempt != 1 {
		t.Fatalf("overall deadline did not cancel backoff: %+v", final)
	}
}

func TestRuntimeRetentionPreservesActiveRuns(t *testing.T) {
	p := runtimePlan(t, []NodeSpec{{ID: "value"}}, map[string]Handler{"value": func(_ context.Context, c Call) (Object, error) { return c.Input, nil }})
	e := runtimeEngine(t, Options{RetainedRuns: 2})
	var ids []string
	for i := 0; i < 3; i++ {
		s := runtimeStart(t, e, p, "x")
		runtimeDone(t, e, s.RunID)
		ids = append(ids, s.RunID)
	}
	if _, ok := e.Snapshot(ids[0]); ok {
		t.Fatal("oldest completed run retained beyond bound")
	}
	for _, id := range ids[1:] {
		if _, ok := e.Snapshot(id); !ok {
			t.Fatal("recent terminal run was evicted")
		}
	}
}

func TestRuntimePhysicalConcurrencyLimits(t *testing.T) {
	started := make(chan string, 20)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var active, peak atomic.Int32
	handlers := make(map[string]Handler)
	var specs []NodeSpec
	for _, id := range []string{"a", "b", "c", "d"} {
		specs = append(specs, NodeSpec{ID: id})
		handlers[id] = func(ctx context.Context, c Call) (Object, error) {
			count := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); count > old && !peak.CompareAndSwap(old, count); old = peak.Load() {
			}
			started <- c.Meta.RunID
			select {
			case <-release:
				return c.Input, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	p := runtimePlan(t, specs, handlers)
	e := runtimeEngine(t, Options{MaxNodesPerRun: 2, MaxConcurrentNodes: 3})
	one := runtimeStart(t, e, p, "one")
	if a, b := receiveRuntime(t, started), receiveRuntime(t, started); a != one.RunID || b != one.RunID {
		t.Fatal("unexpected per-run slots")
	}
	select {
	case <-started:
		t.Fatal("per-run physical concurrency exceeded")
	case <-time.After(10 * time.Millisecond):
	}
	two := runtimeStart(t, e, p, "two")
	if receiveRuntime(t, started) != two.RunID {
		t.Fatal("global slot did not serve second run")
	}
	select {
	case <-started:
		t.Fatal("global physical concurrency exceeded")
	case <-time.After(10 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	for _, id := range []string{one.RunID, two.RunID} {
		if s := runtimeDone(t, e, id); s.Status != StatusSucceeded {
			t.Fatalf("run failed: %+v", s)
		}
	}
	if peak.Load() > 3 {
		t.Fatalf("peak physical concurrency %d", peak.Load())
	}
}

func TestRuntimeFatalFailureCancelsRunningSibling(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	p := runtimePlan(t, []NodeSpec{{ID: "a-fail"}, {ID: "b-running"}, {ID: "c-after", DependsOn: []string{"a-fail"}}}, map[string]Handler{
		"a-fail": func(context.Context, Call) (Object, error) { <-started; return nil, errors.New("permanent") },
		"b-running": func(ctx context.Context, c Call) (Object, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return c.Input, nil
		},
		"c-after": func(context.Context, Call) (Object, error) { t.Error("dependent was executed"); return nil, nil },
	})
	e := runtimeEngine(t, Options{MaxNodesPerRun: 2})
	s := runtimeStart(t, e, p, "x")
	final := runtimeDone(t, e, s.RunID)
	if final.Status != StatusFailed || final.Nodes[0].Status != StatusFailed || final.Nodes[1].Status != StatusCancelled || final.Nodes[2].Status != StatusSkipped {
		t.Fatalf("incorrect failure propagation: %+v", final)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("running sibling did not receive cancellation")
	}
}

func TestRuntimeAdmissionIsAtomic(t *testing.T) {
	p := runtimePlan(t, []NodeSpec{{ID: "block"}}, map[string]Handler{"block": func(ctx context.Context, _ Call) (Object, error) { <-ctx.Done(); return nil, ctx.Err() }})
	e := runtimeEngine(t, Options{MaxActiveRuns: 3})
	var accepted atomic.Int32
	var rejected atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.Start(p, Object{"value": "x"})
			if err == nil {
				accepted.Add(1)
			} else if errors.Is(err, ErrCapacity) {
				rejected.Add(1)
			} else {
				t.Errorf("unexpected admission error: %v", err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 3 || rejected.Load() != 17 {
		t.Fatalf("admission raced: accepted=%d rejected=%d", accepted.Load(), rejected.Load())
	}
}

func TestRuntimeEmptySuccessfulOutputSerialization(t *testing.T) {
	p := runtimePlan(t, []NodeSpec{{ID: "value"}}, map[string]Handler{"value": func(_ context.Context, c Call) (Object, error) { return c.Input, nil }})
	p.definition.Outputs = map[string]Binding{}
	p.definition.OutputSchema = runtimeSchema()
	e := runtimeEngine(t, Options{})
	run := runtimeStart(t, e, p, "x")
	s := runtimeDone(t, e, run.RunID)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["output"]) != "{}" {
		t.Fatalf("empty successful output missing: %s", data)
	}
	for _, status := range []string{StatusPending, StatusRunning, StatusFailed, StatusTimedOut, StatusCancelled} {
		s.Status = status
		data, err = json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		fields = nil
		if err = json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["output"]; ok {
			t.Fatalf("non-success exposes output: %s", data)
		}
	}
}

func TestRuntimeConcurrentStartReturnsRecordDespiteEviction(t *testing.T) {
	p := runtimePlan(t, []NodeSpec{{ID: "value"}}, map[string]Handler{"value": func(_ context.Context, c Call) (Object, error) { return c.Input, nil }})
	e := runtimeEngine(t, Options{RetainedRuns: 1, MaxActiveRuns: 256})
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := e.Start(p, Object{"value": "x"})
			if err != nil {
				t.Errorf("start: %v", err)
				return
			}
			if s.RunID == "" || s.WorkflowID != "runtime-test" || s.InstanceID != e.InstanceID() {
				t.Errorf("admitted run missing from start response: %+v", s)
			}
		}()
	}
	wg.Wait()
}

func TestRuntimeNestedObjectsAreIsolatedAcrossBranchesAndCaller(t *testing.T) {
	mutated := make(chan struct{})
	valueSchema := map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "required": []any{"name"}, "additionalProperties": false}
	objectSchema := Schema{"type": "object", "properties": map[string]any{"value": valueSchema}, "required": []any{"value"}, "additionalProperties": false}
	r := NewRegistry()
	if err := r.Register(Registration{
		Descriptor: Descriptor{Type: "test.nested", TypeVersion: "1", Title: "Nested", Description: "Nested isolation", Category: "test", ConfigSchema: runtimeSchema(), InputSchema: objectSchema, OutputSchema: objectSchema},
		Handler: func(ctx context.Context, c Call) (Object, error) {
			switch c.Meta.NodeID {
			case "mutate":
				c.Input["value"].(map[string]any)["name"] = "changed"
				close(mutated)
			case "read":
				select {
				case <-mutated:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return c.Input, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	p, err := Compile(Definition{APIVersion: "workflow/v1", WorkflowID: "nested", DefinitionVersion: "1", Title: "Nested", InputSchema: objectSchema, OutputSchema: objectSchema,
		Nodes: []NodeSpec{
			{ID: "source", Type: "test.nested", TypeVersion: "1", Inputs: map[string]Binding{"value": {Kind: "workflowInput", Field: "value"}}},
			{ID: "mutate", Type: "test.nested", TypeVersion: "1", Inputs: map[string]Binding{"value": {Kind: "nodeOutput", NodeID: "source", Field: "value"}}},
			{ID: "read", Type: "test.nested", TypeVersion: "1", Inputs: map[string]Binding{"value": {Kind: "nodeOutput", NodeID: "source", Field: "value"}}},
		}, Outputs: map[string]Binding{"value": {Kind: "nodeOutput", NodeID: "read", Field: "value"}}}, r)
	if err != nil {
		t.Fatal(err)
	}
	e := runtimeEngine(t, Options{})
	input := Object{"value": map[string]any{"name": "original"}}
	s, err := e.Start(p, input)
	if err != nil {
		t.Fatal(err)
	}
	input["value"].(map[string]any)["name"] = "external mutation"
	final := runtimeDone(t, e, s.RunID)
	if final.Status != StatusSucceeded || final.Output["value"].(map[string]any)["name"] != "original" {
		t.Fatalf("mutable references leaked across boundaries: %+v", final)
	}
	final.Output["value"].(map[string]any)["name"] = "snapshot mutation"
	again, _ := e.Snapshot(s.RunID)
	if again.Output["value"].(map[string]any)["name"] != "original" {
		t.Fatal("nested query snapshot shared references")
	}
}

func TestRuntimeCanonicalInputSizeIsClassified(t *testing.T) {
	p := runtimePlan(t, []NodeSpec{{ID: "value"}}, map[string]Handler{"value": func(_ context.Context, c Call) (Object, error) { return c.Input, nil }})
	e := runtimeEngine(t, Options{})
	_, err := e.Start(p, Object{"value": strings.Repeat("<", 50000)})
	var invalid *InputError
	if !errors.Is(err, ErrInputTooLarge) || !errors.As(err, &invalid) {
		t.Fatalf("expected classified canonical byte limit error, got %v", err)
	}
}

func TestRuntimeCancelledAttemptHasCorrelatedLog(t *testing.T) {
	for _, cause := range []string{"shutdown", "deadline"} {
		t.Run(cause, func(t *testing.T) {
			var logs bytes.Buffer
			started := make(chan string, 1)
			p := runtimePlan(t, []NodeSpec{{ID: "blocked"}}, map[string]Handler{"blocked": func(ctx context.Context, _ Call) (Object, error) {
				started <- "started"
				<-ctx.Done()
				return nil, ctx.Err()
			}})
			options := Options{Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			if cause == "deadline" {
				options.RunTimeout = 25 * time.Millisecond
			}
			e := NewEngine(context.Background(), options)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = e.Shutdown(ctx)
			})
			s := runtimeStart(t, e, p, "x")
			receiveRuntime(t, started)
			code := "RUN_CANCELLED"
			if cause == "deadline" {
				runtimeDone(t, e, s.RunID)
				code = "RUN_TIMEOUT"
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := e.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			final := runtimeDone(t, e, s.RunID)
			node := final.Nodes[0]
			if node.Status != StatusCancelled || node.Error == nil || node.Error.Code != code || node.Attempts[0].Error == nil || node.Attempts[0].Error.Code != code {
				t.Fatalf("missing cancellation diagnostic: %+v", node)
			}
			found := false
			for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
				var record map[string]any
				if err := json.Unmarshal(line, &record); err != nil {
					t.Fatal(err)
				}
				if record["msg"] != "node attempt completed" {
					continue
				}
				found = true
				if record["runId"] != s.RunID || record["nodeId"] != "blocked" || record["nodeExecutionId"] != node.NodeExecutionID || record["status"] != StatusCancelled || record["errorCode"] != code {
					t.Fatalf("incomplete attempt log: %+v", record)
				}
			}
			if !found {
				t.Fatalf("cancelled attempt did not emit terminal log: %s", logs.String())
			}
		})
	}
}
