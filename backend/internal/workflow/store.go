package workflow

import (
	"encoding/json"
	"time"
)

const (
	StatusPending   = "PENDING"
	StatusRunning   = "RUNNING"
	StatusRetryWait = "RETRY_WAIT"
	StatusSucceeded = "SUCCEEDED"
	StatusFailed    = "FAILED"
	StatusTimedOut  = "TIMED_OUT"
	StatusCancelled = "CANCELLED"
	StatusSkipped   = "SKIPPED"
)

// AttemptSnapshot describes one physical invocation; logical cancellation does
// not imply that an uncooperative handler has physically exited.
type AttemptSnapshot struct {
	Attempt    int        `json:"attempt"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Error      *RunError  `json:"error,omitempty"`
}

// NodeSnapshot groups retry attempts under one stable logical execution ID.
type NodeSnapshot struct {
	NodeID          string            `json:"nodeId"`
	NodeExecutionID string            `json:"nodeExecutionId"`
	Status          string            `json:"status"`
	Attempt         int               `json:"attempt"`
	Attempts        []AttemptSnapshot `json:"attempts"`
	StartedAt       *time.Time        `json:"startedAt,omitempty"`
	FinishedAt      *time.Time        `json:"finishedAt,omitempty"`
	Error           *RunError         `json:"error,omitempty"`
}

// Snapshot is a detached, ephemeral query result. Outputs are exposed only for
// successful runs. Full inputs and intermediate outputs are not query data.
type Snapshot struct {
	RunID             string         `json:"runId"`
	WorkflowID        string         `json:"workflowId"`
	DefinitionVersion string         `json:"definitionVersion"`
	Status            string         `json:"status"`
	InstanceID        string         `json:"instanceId"`
	Ephemeral         bool           `json:"ephemeral"`
	Output            Object         `json:"output,omitempty"`
	Nodes             []NodeSnapshot `json:"nodes"`
	CreatedAt         time.Time      `json:"createdAt"`
	StartedAt         *time.Time     `json:"startedAt,omitempty"`
	FinishedAt        *time.Time     `json:"finishedAt,omitempty"`
	Error             *RunError      `json:"error,omitempty"`
}

// MarshalJSON includes an empty output object for a successful run while
// withholding the output field entirely from pending or unsuccessful runs.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	type plainSnapshot Snapshot
	var output *Object
	if s.Status == StatusSucceeded {
		value := s.Output
		if value == nil {
			value = Object{}
		}
		output = &value
	}
	return json.Marshal(struct {
		plainSnapshot
		Output *Object `json:"output,omitempty"`
	}{plainSnapshot: plainSnapshot(s), Output: output})
}

func terminal(status string) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusTimedOut, StatusCancelled, StatusSkipped:
		return true
	default:
		return false
	}
}

func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

func copyError(err *RunError) *RunError {
	if err == nil {
		return nil
	}
	v := *err
	return &v
}

func copySnapshot(s Snapshot) Snapshot {
	s.Output = cloneObject(s.Output)
	s.StartedAt = copyTime(s.StartedAt)
	s.FinishedAt = copyTime(s.FinishedAt)
	s.Error = copyError(s.Error)
	s.Nodes = append([]NodeSnapshot(nil), s.Nodes...)
	for i := range s.Nodes {
		n := &s.Nodes[i]
		n.StartedAt = copyTime(n.StartedAt)
		n.FinishedAt = copyTime(n.FinishedAt)
		n.Error = copyError(n.Error)
		n.Attempts = append([]AttemptSnapshot{}, n.Attempts...)
		for j := range n.Attempts {
			n.Attempts[j].FinishedAt = copyTime(n.Attempts[j].FinishedAt)
			n.Attempts[j].Error = copyError(n.Attempts[j].Error)
		}
	}
	return s
}

// save publishes a whole snapshot atomically. A terminal snapshot is immutable;
// late events cannot reinsert an evicted record or overwrite its final status.
func (e *Engine) save(s Snapshot) {
	e.mu.Lock()
	defer e.mu.Unlock()
	previous, exists := e.runs[s.RunID]
	if !exists || terminal(previous.Status) {
		return
	}
	*previous = copySnapshot(s)
	if !terminal(s.Status) {
		return
	}
	e.active--
	e.completed = append(e.completed, s.RunID)
	if len(e.completed) > e.options.RetainedRuns {
		delete(e.runs, e.completed[0])
		e.completed = e.completed[1:]
	}
}
