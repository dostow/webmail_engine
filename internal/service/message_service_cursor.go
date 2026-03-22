package service

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"sort"
	"time"
	"webmail_engine/internal/models"
)

// CursorData represents pagination cursor data for stable navigation
type CursorData struct {
	Page      int              `json:"page"`
	LastUID   uint32           `json:"last_uid,omitempty"` // Last UID from previous page for stable pagination
	SortBy    models.SortField `json:"sort_by"`
	SortOrder models.SortOrder `json:"sort_order"`
	Timestamp time.Time        `json:"timestamp"`
}

// LegacyCursorData represents the old cursor format (for backward compatibility)
type LegacyCursorData struct {
	Offset int `json:"offset,omitempty"`
	Page   int `json:"page,omitempty"`
}

// encodeCursor encodes cursor data to a base64 string
func encodeCursor(data CursorData) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(jsonData), nil
}

// decodeCursor decodes a base64 cursor string to CursorData.
// It handles both the current format and legacy {offset, page} format for backward compatibility.
// If decoding fails, it returns a default cursor starting from page 0.
func decodeCursor(cursor string) (CursorData, error) {
	var data CursorData

	if cursor == "" {
		return data, nil
	}

	jsonData, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		log.Printf("Cursor base64 decode failed: %v, starting from page 0", err)
		return data, nil
	}

	// Try to decode as current format first
	err = json.Unmarshal(jsonData, &data)
	if err == nil && data.Page >= 0 {
		return data, nil
	}

	// Try legacy format {offset, page}
	var legacy LegacyCursorData
	if err := json.Unmarshal(jsonData, &legacy); err == nil {
		if legacy.Page > 0 {
			data.Page = legacy.Page - 1 // Convert 1-based to 0-based
		} else if legacy.Offset > 0 {
			data.Page = legacy.Offset / 50 // Assume default page size of 50
		}
		log.Printf("Legacy cursor format detected, converted to page %d", data.Page)
		return data, nil
	}

	// If all decoding fails, start from page 0
	log.Printf("Invalid cursor format, starting from page 0")
	return data, nil
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// adjustCursorForDeletedAnchor adjusts cursor when anchor UID was deleted
// Finds the nearest surviving UID and returns adjusted start index
func adjustCursorForDeletedAnchor(cursor CursorData, uids []uint32, pageSize int) int {
	if cursor.LastUID == 0 {
		return cursor.Page * pageSize
	}

	// Find position where anchor UID would have been
	insertPos := sort.Search(len(uids), func(i int) bool {
		return uids[i] >= cursor.LastUID
	})

	// Use the position as new start index
	// This shows messages that were "next" after the deleted one
	if insertPos >= len(uids) {
		// All remaining UIDs deleted, return to beginning
		return 0
	}

	return insertPos
}
