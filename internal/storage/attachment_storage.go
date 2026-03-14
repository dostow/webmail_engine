package storage

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AttachmentStorage handles temporary attachment storage
type AttachmentStorage struct {
	basePath    string
	mu          sync.RWMutex
	attachments map[string]*AttachmentInfo
	cleanupChan chan struct{}
}

// AttachmentInfo stores attachment metadata
type AttachmentInfo struct {
	ID        string
	Path      string
	Checksum  string
	Size      int64
	CreatedAt time.Time
	ExpiresAt time.Time
	Downloads int
}

// NewAttachmentStorage creates a new attachment storage
func NewAttachmentStorage(basePath string) *AttachmentStorage {
	if basePath == "" {
		basePath = "./temp/attachments"
	}
	
	storage := &AttachmentStorage{
		basePath:    basePath,
		attachments: make(map[string]*AttachmentInfo),
		cleanupChan: make(chan struct{}),
	}
	
	// Ensure base path exists
	os.MkdirAll(basePath, 0755)
	
	// Start cleanup goroutine
	go storage.startCleanup()
	
	return storage
}

// Store stores attachment data and returns the path
func (s *AttachmentStorage) Store(data []byte, checksum string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Generate ID from checksum
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(checksum+time.Now().String())))[:16]
	
	// Create subdirectory based on date
	datePath := time.Now().Format("2006/01/02")
	dirPath := filepath.Join(s.basePath, datePath)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", err
	}
	
	// Create file
	filename := fmt.Sprintf("%s.dat", id)
	filePath := filepath.Join(dirPath, filename)
	
	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	
	// Write data
	_, err = file.Write(data)
	if err != nil {
		os.Remove(filePath)
		return "", err
	}
	
	// Store metadata
	info := &AttachmentInfo{
		ID:        id,
		Path:      filePath,
		Checksum:  checksum,
		Size:      int64(len(data)),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Downloads: 0,
	}
	
	s.attachments[id] = info
	
	return filePath, nil
}

// Get retrieves attachment data
func (s *AttachmentStorage) Get(id string) ([]byte, error) {
	s.mu.RLock()
	info, exists := s.attachments[id]
	s.mu.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("attachment not found")
	}
	
	// Check expiry
	if time.Now().After(info.ExpiresAt) {
		return nil, fmt.Errorf("attachment expired")
	}
	
	// Read file
	data, err := os.ReadFile(info.Path)
	if err != nil {
		return nil, err
	}
	
	// Update download count
	s.mu.Lock()
	info.Downloads++
	s.mu.Unlock()
	
	return data, nil
}

// GetReader returns a reader for streaming attachment
func (s *AttachmentStorage) GetReader(id string) (io.ReadCloser, error) {
	s.mu.RLock()
	info, exists := s.attachments[id]
	s.mu.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("attachment not found")
	}
	
	// Check expiry
	if time.Now().After(info.ExpiresAt) {
		return nil, fmt.Errorf("attachment expired")
	}
	
	// Open file
	file, err := os.Open(info.Path)
	if err != nil {
		return nil, err
	}
	
	// Update download count
	s.mu.Lock()
	info.Downloads++
	s.mu.Unlock()
	
	return file, nil
}

// Delete removes an attachment
func (s *AttachmentStorage) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	info, exists := s.attachments[id]
	if !exists {
		return fmt.Errorf("attachment not found")
	}
	
	// Remove file
	if err := os.Remove(info.Path); err != nil {
		return err
	}
	
	delete(s.attachments, id)
	
	return nil
}

// GetInfo returns attachment info
func (s *AttachmentStorage) GetInfo(id string) (*AttachmentInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	info, exists := s.attachments[id]
	if !exists {
		return nil, fmt.Errorf("attachment not found")
	}
	
	return info, nil
}

// Cleanup removes expired attachments
func (s *AttachmentStorage) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	now := time.Now()
	removed := 0
	
	for id, info := range s.attachments {
		if now.After(info.ExpiresAt) {
			os.Remove(info.Path)
			delete(s.attachments, id)
			removed++
		}
	}
	
	return removed
}

// startCleanup runs periodic cleanup
func (s *AttachmentStorage) startCleanup() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			s.Cleanup()
		case <-s.cleanupChan:
			return
		}
	}
}

// Shutdown stops the storage
func (s *AttachmentStorage) Shutdown() {
	close(s.cleanupChan)
}

// GetStats returns storage statistics
func (s *AttachmentStorage) GetStats() (int, int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	count := len(s.attachments)
	var totalSize int64
	
	for _, info := range s.attachments {
		totalSize += info.Size
	}
	
	return count, totalSize
}
