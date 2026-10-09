package workflow

import (
	"errors"
	"fmt"
)

var (
	// ErrCapacity means all active-run admission slots are occupied.
	ErrCapacity = errors.New("active run capacity exceeded")
	// ErrInputTooLarge means the canonical JSON encoding exceeds 256 KiB.
	ErrInputTooLarge = errors.New("input canonical JSON encoding exceeds 256 KiB")
	// ErrClosed means the service is shutting down or has stopped.
	ErrClosed = errors.New("workflow engine is closed")
)

// InputError reports invalid workflow input before a run is admitted.
type InputError struct{ Err error }

func (e *InputError) Error() string { return fmt.Sprintf("invalid workflow input: %v", e.Err) }
func (e *InputError) Unwrap() error { return e.Err }

// RunError is the stable public failure summary for a run or node attempt.
// Business data is intentionally absent from this diagnostic object.
type RunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	NodeID  string `json:"nodeId,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
}

func (e *RunError) Error() string { return e.Code + ": " + e.Message }

type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// Retryable explicitly marks a transient failure. The engine will retry only
// when the node also declares RetrySafe and has attempts remaining.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return &retryableError{err: err}
}

func isRetryable(err error) bool {
	var marked *retryableError
	return errors.As(err, &marked)
}
