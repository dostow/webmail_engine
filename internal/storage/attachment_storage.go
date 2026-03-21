package storage

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// AttachmentStorage defines the interface for attachment storage implementations
type AttachmentStorage interface {
	// Store stores attachment data and returns the attachment ID
	// accountID, folder, messageUID provide context for organization
	Store(accountID, folder, messageUID, filename string, data []byte) (string, error)

	// Get retrieves attachment data by account, folder, message, and attachment ID
	Get(accountID, folder, messageUID, attachmentID string) ([]byte, error)

	// Delete removes an attachment by account, folder, message, and attachment ID
	Delete(accountID, folder, messageUID, attachmentID string) error

	// DeleteMessageAttachments removes all attachments for a message
	DeleteMessageAttachments(accountID, folder, messageUID string) error

	// Exists checks if an attachment exists
	Exists(accountID, folder, messageUID, attachmentID string) bool

	// Path returns the file path for an attachment (for direct file serving)
	Path(accountID, folder, messageUID, attachmentID string) string

	// Cleanup removes all attachments (use with caution)
	Cleanup() int

	// GetStats returns storage statistics
	GetStats() StorageStats

	// Shutdown gracefully stops the storage backend (no-op for file storage)
	Shutdown()
}

// StorageStats holds storage statistics
type StorageStats struct {
	Count     int   `json:"count"`
	TotalSize int64 `json:"total_size"`
}

// FileAttachmentStorage implements AttachmentStorage using the local filesystem
// Directory structure: basePath/accountID/folder/messageUID/attachmentID
type FileAttachmentStorage struct {
	basePath string
}

// NewFileAttachmentStorage creates a new file-based attachment storage
func NewFileAttachmentStorage(basePath string) AttachmentStorage {
	if basePath == "" {
		basePath = "./temp/attachments"
	}

	// Ensure base path exists
	os.MkdirAll(basePath, 0755)

	return &FileAttachmentStorage{
		basePath: basePath,
	}
}

// Store stores attachment data and returns the attachment ID
// Directory structure: basePath/accountID/folder/messageUID/attachmentID
func (s *FileAttachmentStorage) Store(accountID, folder, messageUID, filename string, data []byte) (string, error) {
	// Generate attachment ID from filename + content (first 16 chars of SHA256)
	hashInput := fmt.Sprintf("%s:%s:%s:%s", accountID, folder, messageUID, filename)
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(hashInput)))[:16]

	// Create directory structure: accountID/folder/messageUID
	// Sanitize folder name (replace / with _)
	safeFolder := filepath.Base(folder)
	dir := filepath.Join(s.basePath, accountID, safeFolder, messageUID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file
	path := filepath.Join(dir, id)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return id, nil
}

// Get retrieves attachment data by account, folder, message, and attachment ID
func (s *FileAttachmentStorage) Get(accountID, folder, messageUID, attachmentID string) ([]byte, error) {
	path := s.Path(accountID, folder, messageUID, attachmentID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("attachment not found")
		}
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return data, nil
}

// GetReader returns a reader for streaming attachment (for large files)
func (s *FileAttachmentStorage) GetReader(accountID, folder, messageUID, attachmentID string) (io.ReadCloser, error) {
	path := s.Path(accountID, folder, messageUID, attachmentID)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("attachment not found")
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

// Delete removes an attachment by account, folder, message, and attachment ID
func (s *FileAttachmentStorage) Delete(accountID, folder, messageUID, attachmentID string) error {
	path := s.Path(accountID, folder, messageUID, attachmentID)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("attachment not found")
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

// DeleteMessageAttachments removes all attachments for a message
func (s *FileAttachmentStorage) DeleteMessageAttachments(accountID, folder, messageUID string) error {
	safeFolder := filepath.Base(folder)
	dir := filepath.Join(s.basePath, accountID, safeFolder, messageUID)
	if err := os.RemoveAll(dir); err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete message attachments: %w", err)
	}

	return nil
}

// Exists checks if an attachment exists
func (s *FileAttachmentStorage) Exists(accountID, folder, messageUID, attachmentID string) bool {
	path := s.Path(accountID, folder, messageUID, attachmentID)
	_, err := os.Stat(path)
	return err == nil
}

// Path returns the file path for an attachment (for direct file serving)
func (s *FileAttachmentStorage) Path(accountID, folder, messageUID, attachmentID string) string {
	safeFolder := filepath.Base(folder)
	return filepath.Join(s.basePath, accountID, safeFolder, messageUID, attachmentID)
}

// Cleanup removes all attachments (use with caution)
// Returns the number of files removed
func (s *FileAttachmentStorage) Cleanup() int {
	removed := 0

	err := filepath.Walk(s.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}

		if !info.IsDir() {
			if err := os.Remove(path); err == nil {
				removed++
			}
		}

		return nil
	})

	if err != nil {
		return removed
	}

	return removed
}

// GetStats returns storage statistics
func (s *FileAttachmentStorage) GetStats() StorageStats {
	stats := StorageStats{}

	filepath.Walk(s.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !info.IsDir() {
			stats.Count++
			stats.TotalSize += info.Size()
		}

		return nil
	})

	return stats
}

// Shutdown gracefully stops the storage backend (no-op for file storage)
func (s *FileAttachmentStorage) Shutdown() {
	// No-op for file-based storage
}
