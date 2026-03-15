package cachekey

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Cache key prefixes for different cache entities
const (
	PrefixMessage      = "msg"
	PrefixMessageHash  = "msg:hash"
	PrefixMessageList  = "msglist"
	PrefixFolder       = "fld"
	PrefixAccount      = "acct"
	PrefixEnvelope     = "env"
	PrefixThread       = "thd"
	PrefixSearch       = "srch"
	PrefixTokenBucket  = "tkn"
	PrefixAttachment   = "att"
)

// Separator used in cache keys
const Separator = ":"

// MessageKeyBuilder builds validated message cache keys
type MessageKeyBuilder struct {
	accountID string
	folder    string
	uid       string
}

// NewMessageKeyBuilder creates a new message cache key builder
func NewMessageKeyBuilder() *MessageKeyBuilder {
	return &MessageKeyBuilder{}
}

// AccountID sets the account ID
func (b *MessageKeyBuilder) AccountID(id string) *MessageKeyBuilder {
	b.accountID = id
	return b
}

// Folder sets the folder name
func (b *MessageKeyBuilder) Folder(folder string) *MessageKeyBuilder {
	b.folder = folder
	return b
}

// UID sets the message UID
func (b *MessageKeyBuilder) UID(uid string) *MessageKeyBuilder {
	b.uid = uid
	return b
}

// Build validates and builds the message cache key
// Returns an error if any required field is missing or invalid
func (b *MessageKeyBuilder) Build() (string, error) {
	if err := ValidateAccountID(b.accountID); err != nil {
		return "", err
	}
	if err := ValidateFolder(b.folder); err != nil {
		return "", err
	}
	if err := ValidateUID(b.uid); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s%s%s%s%s%s%s", PrefixMessage, Separator, b.accountID, Separator, b.folder, Separator, b.uid), nil
}

// BuildMust validates and builds the message cache key
// Panics if validation fails - use only when inputs are guaranteed valid
func (b *MessageKeyBuilder) BuildMust() string {
	key, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("invalid message cache key: %v", err))
	}
	return key
}

// BuildOrDefault validates and builds the message cache key
// Returns the default key if validation fails
func (b *MessageKeyBuilder) BuildOrDefault(defaultKey string) string {
	key, err := b.Build()
	if err != nil {
		return defaultKey
	}
	return key
}

// MessageKey builds a validated message cache key
// Equivalent to NewMessageKeyBuilder().AccountID(accountID).Folder(folder).UID(uid).Build()
func MessageKey(accountID, folder, uid string) (string, error) {
	return NewMessageKeyBuilder().
		AccountID(accountID).
		Folder(folder).
		UID(uid).
		Build()
}

// MessageKeySafe builds a message cache key with safe defaults
// Uses "INBOX" as default folder if empty, but still validates accountID and UID
func MessageKeySafe(accountID, folder, uid string) string {
	if folder == "" {
		folder = "INBOX"
	}

	key, err := NewMessageKeyBuilder().
		AccountID(accountID).
		Folder(folder).
		UID(uid).
		Build()

	if err != nil {
		// Log error and return empty key or handle as needed
		// In production, this should be logged
		return ""
	}
	return key
}

// ContentHashKey builds a cache key for content-based deduplication
func ContentHashKey(contentHash string) (string, error) {
	if contentHash == "" {
		return "", ErrEmptyContentHash
	}
	return fmt.Sprintf("%s%s%s", PrefixMessageHash, Separator, contentHash), nil
}

// ContentHashKeySafe builds a content hash key without validation
// Returns empty string if hash is empty
func ContentHashKeySafe(contentHash string) string {
	if contentHash == "" {
		return ""
	}
	return fmt.Sprintf("%s%s%s", PrefixMessageHash, Separator, contentHash)
}

// MessageListKey builds a cache key for message list queries
func MessageListKey(accountID, folder, cursor string, limit int, sortBy, sortOrder string) string {
	if folder == "" {
		folder = "INBOX"
	}
	return fmt.Sprintf("%s%s%s%s%s%s%s%s%d%s%s%s%s",
		PrefixMessageList, Separator,
		accountID, Separator,
		folder, Separator,
		cursor, Separator,
		limit, Separator,
		sortBy, Separator,
		sortOrder,
	)
}

// FolderKey builds a cache key for folder information
func FolderKey(accountID, folder string) (string, error) {
	if err := ValidateAccountID(accountID); err != nil {
		return "", err
	}
	if folder == "" {
		folder = "INBOX"
	}
	return fmt.Sprintf("%s%s%s%s%s", PrefixFolder, Separator, accountID, Separator, folder), nil
}

// FolderListKey builds a cache key for folder listings
func FolderListKey(accountID string) (string, error) {
	if err := ValidateAccountID(accountID); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%s", PrefixFolder, Separator, accountID), nil
}

// AccountKey builds a cache key for account data
func AccountKey(accountID string) (string, error) {
	if err := ValidateAccountID(accountID); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%s", PrefixAccount, Separator, accountID), nil
}

// EnvelopeKey builds a cache key for message envelope (metadata)
func EnvelopeKey(accountID, folder, uid string) (string, error) {
	if err := ValidateAccountID(accountID); err != nil {
		return "", err
	}
	if folder == "" {
		folder = "INBOX"
	}
	if uid == "" {
		return "", ErrEmptyUID
	}
	return fmt.Sprintf("%s%s%s%s%s%s%s", PrefixEnvelope, Separator, accountID, Separator, folder, Separator, uid), nil
}

// ThreadKey builds a cache key for message thread
func ThreadKey(accountID, threadID string) (string, error) {
	if err := ValidateAccountID(accountID); err != nil {
		return "", err
	}
	if threadID == "" {
		return "", errors.New("thread ID cannot be empty")
	}
	return fmt.Sprintf("%s%s%s%s%s", PrefixThread, Separator, accountID, Separator, threadID), nil
}

// SearchKey builds a cache key for search results
func SearchKey(queryHash string) string {
	if queryHash == "" {
		return ""
	}
	return fmt.Sprintf("%s%s%s", PrefixSearch, Separator, queryHash)
}

// TokenBucketKey builds a cache key for token bucket state
func TokenBucketKey(accountID string) (string, error) {
	if err := ValidateAccountID(accountID); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%s", PrefixTokenBucket, Separator, accountID), nil
}

// AttachmentKey builds a cache key for attachment metadata
func AttachmentKey(attachmentID string) string {
	if attachmentID == "" {
		return ""
	}
	return fmt.Sprintf("%s%s%s", PrefixAttachment, Separator, attachmentID)
}

// Validation functions

// ValidateAccountID ensures account ID is not empty and has valid format
func ValidateAccountID(id string) error {
	if id == "" {
		return ErrEmptyAccountID
	}
	// Account IDs should start with "acc_"
	if !strings.HasPrefix(id, "acc_") {
		return fmt.Errorf("invalid account ID format: %s", id)
	}
	return nil
}

// ValidateFolder ensures folder name is not empty
func ValidateFolder(folder string) error {
	if folder == "" {
		return ErrEmptyFolder
	}
	return nil
}

// ValidateUID ensures UID is not empty and is a valid positive integer
func ValidateUID(uid string) error {
	if uid == "" {
		return ErrEmptyUID
	}
	
	// UID should be numeric
	uidNum, err := strconv.ParseUint(uid, 10, 32)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidUID, err)
	}
	if uidNum == 0 {
		return fmt.Errorf("%w: UID must be positive", ErrInvalidUID)
	}
	return nil
}

// ParseMessageKey parses a message cache key into its components
func ParseMessageKey(key string) (accountID, folder, uid string, err error) {
	parts := strings.Split(key, Separator)
	if len(parts) != 4 || parts[0] != PrefixMessage {
		return "", "", "", fmt.Errorf("invalid message key format: %s", key)
	}
	return parts[1], parts[2], parts[3], nil
}
