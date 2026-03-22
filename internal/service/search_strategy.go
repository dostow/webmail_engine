package service

import (
	"context"
	"time"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
)

// SearchStrategy defines the interface for IMAP search operations
// Different implementations can provide different search behaviors
// (e.g., with or without BODY search support)
type SearchStrategy interface {
	// BuildSearchQuery converts a SearchQuery into an IMAP search criteria string
	BuildSearchQuery(query models.SearchQuery) string

	// ExecuteSearch performs the actual IMAP search with the given criteria
	ExecuteSearch(ctx context.Context, client *pool.IMAPAdapter, criteria string) ([]uint32, error)
}

// DefaultSearchStrategy is the default search implementation
// It does NOT support BODY searches for performance reasons
type DefaultSearchStrategy struct{}

// NewDefaultSearchStrategy creates a new default search strategy
func NewDefaultSearchStrategy() *DefaultSearchStrategy {
	return &DefaultSearchStrategy{}
}

// BuildSearchQuery converts a SearchQuery into an IMAP search criteria string
// This implementation ignores the Body field for performance
func (s *DefaultSearchStrategy) BuildSearchQuery(query models.SearchQuery) string {
	var parts []string

	// Add keyword search - uses SUBJECT only (fast, indexed)
	if len(query.Keywords) > 0 && query.Keywords[0] != "" {
		parts = append(parts, buildSubjectClause(query.Keywords[0]))
	}

	// Note: Body field is intentionally ignored in this strategy
	// Use BodySearchStrategy if BODY search is required

	// Add FROM search
	if query.From != "" {
		parts = append(parts, buildFromClause(query.From))
	}

	// Add TO search
	if query.To != "" {
		parts = append(parts, buildToClause(query.To))
	}

	// Add SUBJECT search (separate from keywords for explicit field search)
	if query.Subject != "" {
		parts = append(parts, buildSubjectClause(query.Subject))
	}

	// Add date range
	if query.Since != nil {
		parts = append(parts, buildSinceClause(query.Since))
	}
	if query.Before != nil {
		parts = append(parts, buildBeforeClause(query.Before))
	}

	// Add UNSEEN flag
	for _, flag := range query.HasFlags {
		if flag == "seen" {
			parts = append(parts, "UNSEEN")
		}
	}

	// Default to ALL if no criteria
	if len(parts) == 0 {
		return "ALL"
	}

	return joinStrings(parts, " ")
}

// ExecuteSearch performs the actual IMAP search with the given criteria
func (s *DefaultSearchStrategy) ExecuteSearch(ctx context.Context, client *pool.IMAPAdapter, criteria string) ([]uint32, error) {
	return client.Search(criteria)
}

// buildSubjectClause builds a SUBJECT search clause
func buildSubjectClause(value string) string {
	return `SUBJECT "` + escapeSearchValue(value) + `"`
}

// buildFromClause builds a FROM search clause
func buildFromClause(value string) string {
	return `FROM "` + escapeSearchValue(value) + `"`
}

// buildToClause builds a TO search clause
func buildToClause(value string) string {
	return `TO "` + escapeSearchValue(value) + `"`
}

// buildSinceClause builds a SINCE search clause
func buildSinceClause(date *time.Time) string {
	return "SINCE " + date.Format("02-Jan-2006")
}

// buildBeforeClause builds a BEFORE search clause
func buildBeforeClause(date *time.Time) string {
	return "BEFORE " + date.Format("02-Jan-2006")
}

// escapeSearchValue escapes special characters in search values
func escapeSearchValue(value string) string {
	// For now, just return the value as-is
	// Can be extended to handle quotes, backslashes, etc.
	return value
}

// BodySearchStrategy is a search implementation that supports BODY searches
// Use with caution - BODY searches are slow on large mailboxes without FTS indexing
type BodySearchStrategy struct{}

// NewBodySearchStrategy creates a new body search strategy
func NewBodySearchStrategy() *BodySearchStrategy {
	return &BodySearchStrategy{}
}

// BuildSearchQuery converts a SearchQuery into an IMAP search criteria string
// This implementation supports BODY searches when the Body field is set
func (s *BodySearchStrategy) BuildSearchQuery(query models.SearchQuery) string {
	var parts []string

	// Add keyword search - uses SUBJECT only (fast, indexed)
	if len(query.Keywords) > 0 && query.Keywords[0] != "" {
		parts = append(parts, buildSubjectClause(query.Keywords[0]))
	}

	// Add BODY search if requested (slow operation)
	if query.Body != "" {
		parts = append(parts, buildBodyClause(query.Body))
	}

	// Add FROM search
	if query.From != "" {
		parts = append(parts, buildFromClause(query.From))
	}

	// Add TO search
	if query.To != "" {
		parts = append(parts, buildToClause(query.To))
	}

	// Add SUBJECT search (separate from keywords for explicit field search)
	if query.Subject != "" {
		parts = append(parts, buildSubjectClause(query.Subject))
	}

	// Add date range
	if query.Since != nil {
		parts = append(parts, buildSinceClause(query.Since))
	}
	if query.Before != nil {
		parts = append(parts, buildBeforeClause(query.Before))
	}

	// Add UNSEEN flag
	for _, flag := range query.HasFlags {
		if flag == "seen" {
			parts = append(parts, "UNSEEN")
		}
	}

	// Default to ALL if no criteria
	if len(parts) == 0 {
		return "ALL"
	}

	return joinStrings(parts, " ")
}

// ExecuteSearch performs the actual IMAP search with the given criteria
func (s *BodySearchStrategy) ExecuteSearch(ctx context.Context, client *pool.IMAPAdapter, criteria string) ([]uint32, error) {
	return client.Search(criteria)
}

// buildBodyClause builds a BODY search clause
func buildBodyClause(value string) string {
	return `BODY "` + escapeSearchValue(value) + `"`
}

