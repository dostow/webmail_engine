package cachekey

import (
	"errors"
	"fmt"
)

// Error definitions for cachekey package
var (
	ErrEmptyAccountID   = errors.New("account ID cannot be empty")
	ErrEmptyFolder      = errors.New("folder cannot be empty")
	ErrEmptyUID         = errors.New("UID cannot be empty")
	ErrInvalidUID       = errors.New("UID must be a valid positive integer")
	ErrEmptyContentHash = errors.New("content hash cannot be empty")
	ErrEmptyThreadID    = errors.New("thread ID cannot be empty")
	ErrInvalidKeyFormat = errors.New("invalid cache key format")
)

// KeyError represents an error related to cache key generation or parsing
type KeyError struct {
	KeyType string
	Field   string
	Value   string
	Err     error
}

// Error implements the error interface
func (e *KeyError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("cachekey: invalid %s for %s: %v", e.Field, e.KeyType, e.Err)
	}
	return fmt.Sprintf("cachekey: invalid %s for %s", e.Field, e.KeyType)
}

// Unwrap returns the underlying error
func (e *KeyError) Unwrap() error {
	return e.Err
}

// NewAccountIDError creates a KeyError for account ID validation failures
func NewAccountIDError(value string, err error) *KeyError {
	return &KeyError{
		KeyType: "account",
		Field:   "account_id",
		Value:   value,
		Err:     err,
	}
}

// NewFolderError creates a KeyError for folder validation failures
func NewFolderError(value string, err error) *KeyError {
	return &KeyError{
		KeyType: "message",
		Field:   "folder",
		Value:   value,
		Err:     err,
	}
}

// NewUIDError creates a KeyError for UID validation failures
func NewUIDError(value string, err error) *KeyError {
	return &KeyError{
		KeyType: "message",
		Field:   "uid",
		Value:   value,
		Err:     err,
	}
}

// NewContentHashError creates a KeyError for content hash validation failures
func NewContentHashError(value string, err error) *KeyError {
	return &KeyError{
		KeyType: "content_hash",
		Field:   "content_hash",
		Value:   value,
		Err:     err,
	}
}
