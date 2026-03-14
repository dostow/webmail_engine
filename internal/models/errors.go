package models

import (
	"errors"
	"fmt"
)

// Common errors
var (
	ErrAccountNotFound      = errors.New("account not found")
	ErrAccountExists        = errors.New("account already exists")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrConnectionFailed     = errors.New("connection failed")
	ErrMessageNotFound      = errors.New("message not found")
	ErrFolderNotFound       = errors.New("folder not found")
	ErrAttachmentNotFound   = errors.New("attachment not found")
	ErrInsufficientTokens   = errors.New("insufficient fair-use tokens")
	ErrAccountThrottled     = errors.New("account is throttled")
	ErrSystemAtCapacity     = errors.New("system at maximum capacity")
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrProxyUnreachable     = errors.New("proxy server unreachable")
	ErrInvalidConfiguration = errors.New("invalid configuration")
	ErrDuplicateEvent       = errors.New("duplicate event")
	ErrInvalidEventSource   = errors.New("invalid event source")
	ErrAttachmentExpired    = errors.New("attachment access URL expired")
	ErrPermissionDenied     = errors.New("permission denied")
)

// APIError represents a structured API error
type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Details    string `json:"details,omitempty"`
	StatusCode int    `json:"-"`
}

func (e *APIError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Details)
	}
	return e.Message
}

// NewAPIError creates a new API error
func NewAPIError(code, message string, statusCode int) *APIError {
	return &APIError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
	}
}

// NewValidationError creates a validation error
func NewValidationError(field, message string) *APIError {
	return &APIError{
		Code:       "VALIDATION_ERROR",
		Message:    fmt.Sprintf("Invalid %s", field),
		Details:    message,
		StatusCode: 400,
	}
}

// NewAuthError creates an authentication error
func NewAuthError(message string) *APIError {
	return &APIError{
		Code:       "AUTH_ERROR",
		Message:    "Authentication failed",
		Details:    message,
		StatusCode: 401,
	}
}

// NewNotFoundError creates a not found error
func NewNotFoundError(resource, id string) *APIError {
	return &APIError{
		Code:       "NOT_FOUND",
		Message:    fmt.Sprintf("%s not found", resource),
		Details:    fmt.Sprintf("ID: %s", id),
		StatusCode: 404,
	}
}

// NewThrottleError creates a rate limit error
func NewThrottleError(retryAfter int) *APIError {
	return &APIError{
		Code:       "RATE_LIMITED",
		Message:    "Rate limit exceeded",
		Details:    fmt.Sprintf("Retry after %d seconds", retryAfter),
		StatusCode: 429,
	}
}

// NewCapacityError creates a capacity error
func NewCapacityError() *APIError {
	return &APIError{
		Code:       "CAPACITY_EXCEEDED",
		Message:    "System at maximum capacity",
		StatusCode: 503,
	}
}

// NewConflictError creates a conflict error (e.g., duplicate resource)
func NewConflictError(resource, identifier string) *APIError {
	return &APIError{
		Code:       "CONFLICT",
		Message:    fmt.Sprintf("%s already exists", resource),
		Details:    fmt.Sprintf("Identifier: %s", identifier),
		StatusCode: 409,
	}
}

// NewTimeoutError creates a timeout error
func NewTimeoutError(operation string, timeoutSec int) *APIError {
	return &APIError{
		Code:       "TIMEOUT",
		Message:    fmt.Sprintf("%s timed out", operation),
		Details:    fmt.Sprintf("Operation did not complete within %d seconds", timeoutSec),
		StatusCode: 504,
	}
}

// NewServiceUnavailableError creates a service unavailable error
func NewServiceUnavailableError(service, reason string) *APIError {
	return &APIError{
		Code:       "SERVICE_UNAVAILABLE",
		Message:    fmt.Sprintf("%s is currently unavailable", service),
		Details:    reason,
		StatusCode: 503,
	}
}

// WrapError wraps an error with additional context
func WrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}

// IsErrorType checks if an error is of a specific type
func IsErrorType(err error, target error) bool {
	return errors.Is(err, target)
}
