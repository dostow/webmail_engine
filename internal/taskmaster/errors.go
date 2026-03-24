package taskmaster

import (
	"errors"
	"fmt"
)

// Common dispatcher errors.
var (
	// ErrTaskNotFound is returned when dispatching an unregistered task.
	ErrTaskNotFound = errors.New("task not found")

	// ErrTaskAlreadyRegistered is returned when registering a duplicate task ID.
	ErrTaskAlreadyRegistered = errors.New("task already registered")

	// ErrDispatcherNotStarted is returned when dispatching before Start is called.
	ErrDispatcherNotStarted = errors.New("dispatcher not started")

	// ErrDispatcherAlreadyStarted is returned when calling Start on a running dispatcher.
	ErrDispatcherAlreadyStarted = errors.New("dispatcher already started")

	// ErrDispatcherStopped is returned when the dispatcher has been stopped.
	ErrDispatcherStopped = errors.New("dispatcher stopped")

	// ErrTaskTimeout is returned when a task execution exceeds its timeout.
	ErrTaskTimeout = errors.New("task execution timed out")

	// ErrQueueFull is returned when the task queue is at capacity.
	ErrQueueFull = errors.New("task queue is full")

	// ErrInvalidPayload is returned when a task receives an invalid payload.
	ErrInvalidPayload = errors.New("invalid task payload")

	// ErrGracefulShutdownTimeout is returned when graceful shutdown exceeds the timeout.
	ErrGracefulShutdownTimeout = errors.New("graceful shutdown timed out")
)

// ErrorCategory classifies errors for handling and retry decisions.
type ErrorCategory int

const (
	// ErrorCategoryUnknown is the default category for unclassified errors.
	ErrorCategoryUnknown ErrorCategory = iota

	// ErrorCategoryRetryable indicates the error may be transient and retrying could succeed.
	// Examples: network timeouts, temporary service unavailability.
	ErrorCategoryRetryable

	// ErrorCategoryNonRetryable indicates the error is permanent and retrying will not help.
	// Examples: invalid payload, task not found, authentication failure.
	ErrorCategoryNonRetryable

	// ErrorCategorySystem indicates a system-level error that requires attention.
	// Examples: out of memory, disk full, configuration error.
	ErrorCategorySystem
)

// TaskError is a structured error type for task execution failures.
// It provides additional context and categorization for error handling.
type TaskError struct {
	// TaskID identifies the task that failed.
	TaskID string

	// Message is a human-readable error description.
	Message string

	// Err is the underlying error (optional).
	Err error

	// Category classifies the error for retry decisions.
	Category ErrorCategory

	// RetryAfter suggests a delay before retrying (optional).
	// Only applicable for retryable errors.
	RetryAfter int
}

// Error implements the error interface.
func (e *TaskError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("task %s: %s: %v", e.TaskID, e.Message, e.Err)
	}
	return fmt.Sprintf("task %s: %s", e.TaskID, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *TaskError) Unwrap() error {
	return e.Err
}

// IsRetryable returns true if the error is retryable.
func (e *TaskError) IsRetryable() bool {
	return e.Category == ErrorCategoryRetryable
}

// NewTaskError creates a new TaskError with the given parameters.
func NewTaskError(taskID, message string, err error, category ErrorCategory) *TaskError {
	return &TaskError{
		TaskID:   taskID,
		Message:  message,
		Err:      err,
		Category: category,
	}
}

// NewRetryableTaskError creates a retryable TaskError.
func NewRetryableTaskError(taskID, message string, err error) *TaskError {
	return NewTaskError(taskID, message, err, ErrorCategoryRetryable)
}

// NewNonRetryableTaskError creates a non-retryable TaskError.
func NewNonRetryableTaskError(taskID, message string, err error) *TaskError {
	return NewTaskError(taskID, message, err, ErrorCategoryNonRetryable)
}

// NewSystemTaskError creates a system-level TaskError.
func NewSystemTaskError(taskID, message string, err error) *TaskError {
	return NewTaskError(taskID, message, err, ErrorCategorySystem)
}

// WrapError wraps an existing error with task context.
// It preserves the error category if the wrapped error is a TaskError.
func WrapError(taskID, message string, err error) error {
	if err == nil {
		return nil
	}

	var taskErr *TaskError
	if errors.As(err, &taskErr) {
		// Preserve existing category
		return &TaskError{
			TaskID:     taskID,
			Message:    message,
			Err:        err,
			Category:   taskErr.Category,
			RetryAfter: taskErr.RetryAfter,
		}
	}

	// Default to unknown category
	return &TaskError{
		TaskID:   taskID,
		Message:  message,
		Err:      err,
		Category: ErrorCategoryUnknown,
	}
}

// IsTaskNotFound returns true if the error indicates a task was not found.
func IsTaskNotFound(err error) bool {
	return errors.Is(err, ErrTaskNotFound)
}

// IsRetryable returns true if the error is retryable.
func IsRetryable(err error) bool {
	var taskErr *TaskError
	if errors.As(err, &taskErr) {
		return taskErr.IsRetryable()
	}
	return false
}
