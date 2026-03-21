# IMAP Sorting Optimization - Implementation Plan

## Problem Statement

Current implementation fetches **all UIDs** for every account on every message list request. For users with 1000s of email accounts and large mailboxes (50,000+ messages), this causes:

- **Memory pressure**: 50M UIDs × 4 bytes = 200MB+ just for UID storage
- **Network overhead**: Transferring all UIDs on every request
- **Timeout risk**: SORT operations on large mailboxes exceed 8s timeout
- **Poor scalability**: Linear growth with mailbox size

---

## Solution Overview: Hybrid Sorting Strategy

```
┌─────────────────────────────────────────────────────────────────┐
│                    Message List Request                         │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  1. Check UID Cache (with UIDVALIDITY + MODSEQ invalidation)   │
│     └─ HIT → Use cached UIDs (skip SORT entirely)              │
└─────────────────────────────────────────────────────────────────┘
                              │ MISS
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  2. Get Folder Message Count                                    │
│     └─ < 10,000 messages → Use Server SORT                      │
│     └─ ≥ 10,000 messages → Use Date-Range Filter + Client Sort  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  3. Apply Pagination BEFORE Fetching Envelopes                  │
│     └─ Extract only page UIDs (e.g., 50 UIDs for page 1)        │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  4. Fetch Envelopes for Page UIDs Only                          │
│     └─ FetchMessages(pageUIDs) - NOT all UIDs                   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  5. Background Preload Next 2 Pages                             │
│     └─ Non-blocking prefetch for instant scroll                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## Implementation Phases

### Phase 1: UID List Caching with Smart Invalidation

**Goal**: Eliminate repeated SORT operations for the same folder.

#### 1.1 Update UID List Cache Structure

**File**: `internal/messagecache/uid_list_cache.go`

**Changes**:
```go
type UIDListMetadata struct {
    UIDs           []uint32    `json:"uids"`
    MessageCount   int         `json:"message_count"`
    HighestModSeq  uint64      `json:"highest_modseq"`
    CachedAt       time.Time   `json:"cached_at"`
    QResyncCapable bool        `json:"qresync_capable"`
    SortOrder      string      `json:"sort_order"`  // NEW: Track sort order
    SortField      string      `json:"sort_field"`  // NEW: Track sort field
}

// Add TTL configuration
const (
    UIDListTTL      = 10 * time.Minute  // Increased from 5 min
    UIDListShortTTL = 2 * time.Minute   // For large mailboxes
)
```

#### 1.2 Smart Cache Invalidation

**File**: `internal/service/message_service.go`

**Changes**:
```go
// isCacheValid checks if cached UID list is still valid
func (s *MessageService) isCacheValid(cached *messagecache.UIDListMetadata, currentModSeq uint64, currentCount int) bool {
    if cached == nil {
        return false
    }

    // QRESYNC support: Check modseq only
    if cached.QResyncCapable {
        return currentModSeq <= cached.HighestModSeq
    }

    // Non-QRESYNC: Check for significant count changes (>10%)
    countDiff := abs(currentCount - cached.MessageCount)
    if countDiff > cached.MessageCount/10 {
        return false  // Significant change, invalidate
    }

    // Check cache age (max 10 minutes)
    if time.Since(cached.CachedAt) > messagecache.UIDListTTL {
        return false
    }

    return true
}
```

#### 1.3 Cache-First Logic

**File**: `internal/service/message_service.go`

**Changes** in `GetMessageList()`:
```go
// Try cache first with smart invalidation
uidCacheKey := s.uidListCache.BuildKey(accountID, folder, uidValidity)
cachedMetadata, err := s.uidListCache.Get(ctx, uidCacheKey)

if err == nil && cachedMetadata != nil {
    if s.isCacheValid(cachedMetadata, highestModSeq, currentMessageCount) {
        log.Printf("UID cache HIT: %d UIDs (modseq=%d)", len(cachedMetadata.UIDs), cachedMetadata.HighestModSeq)
        allUIDs = cachedMetadata.UIDs
        goto UseUIDs  // Skip SORT entirely
    }
    log.Printf("UID cache INVALID: modseq changed (%d -> %d) or count changed (%d -> %d)",
        cachedMetadata.HighestModSeq, highestModSeq, cachedMetadata.MessageCount, currentMessageCount)
}
```

---

### Phase 2: Mailbox Size Threshold with Date-Range Filtering

**Goal**: Avoid expensive SORT on large mailboxes by pre-filtering by date.

#### 2.1 Configuration Constants

**File**: `internal/service/message_service.go`

**Add**:
```go
// Sorting strategy thresholds
const (
    // Use server-side SORT below this count
    sortThreshold = 10000
    
    // For large mailboxes, filter to recent messages
    largeMailboxRecentDays = 90  // Show last 90 days by default
    
    // Maximum UIDs to fetch in single SORT operation
    maxSortUIDs = 50000  // Abort SORT if mailbox exceeds this
)
```

#### 2.2 Date-Range Search for Large Mailboxes

**File**: `internal/service/message_service.go`

**Add new function**:
```go
// searchByDateRange performs date-filtered search for large mailboxes
// Returns UIDs from recent messages only (configurable time window)
func (s *MessageService) searchByDateRange(
    ctx context.Context,
    client pool.IMAPClient,
    folder string,
    sortBy models.SortField,
    sortOrder models.SortOrder,
    days int,
) ([]uint32, error) {
    // Calculate date range
    sinceDate := time.Now().AddDate(0, 0, -days)
    searchCriteria := fmt.Sprintf("SINCE %s", sinceDate.Format("02-Jan-2006"))
    
    log.Printf("Date-range search: %s (last %d days)", searchCriteria, days)
    
    // Perform SEARCH (not SORT) - much faster on large mailboxes
    uids, err := client.Search(searchCriteria)
    if err != nil {
        return nil, fmt.Errorf("date-range search failed: %w", err)
    }
    
    log.Printf("Date-range search returned %d UIDs (from ~%d total)", len(uids), days)
    
    // Client-side sort on reduced set
    if len(uids) > 0 {
        // Fetch envelopes for sorting
        envelopes, err := client.FetchMessages(uids, false)
        if err != nil {
            return uids, nil  // Return unsorted on error
        }
        
        // Sort by date (or other field)
        sortedEnvelopes := s.sortEnvelopes(envelopes, sortBy, sortOrder)
        
        // Extract sorted UIDs
        sortedUIDs := make([]uint32, len(sortedEnvelopes))
        for i, env := range sortedEnvelopes {
            sortedUIDs[i] = env.UID
        }
        
        return sortedUIDs, nil
    }
    
    return uids, nil
}

// sortEnvelopes sorts message envelopes client-side
func (s *MessageService) sortEnvelopes(
    envelopes []pool.MessageEnvelope,
    sortBy models.SortField,
    sortOrder models.SortOrder,
) []pool.MessageEnvelope {
    sorted := make([]pool.MessageEnvelope, len(envelopes))
    copy(sorted, envelopes)
    
    sort.Slice(sorted, func(i, j int) bool {
        var less bool
        switch sortBy {
        case models.SortByDate, "":
            less = sorted[i].Date.Before(sorted[j].Date)
        case models.SortByFrom:
            less = sorted[i].From[0].Address < sorted[j].From[0].Address
        case models.SortBySubject:
            less = sorted[i].Subject < sorted[j].Subject
        case models.SortBySize:
            less = sorted[i].Size < sorted[j].Size
        default:
            less = sorted[i].Date.Before(sorted[j].Date)
        }
        
        if sortOrder == models.SortOrderDesc {
            return !less
        }
        return less
    })
    
    return sorted
}
```

#### 2.3 Updated SORT Strategy Logic

**File**: `internal/service/message_service.go`

**Replace existing SORT logic** in `GetMessageList()`:
```go
// Determine sorting strategy based on mailbox size
var sortStrategy string
switch {
case currentMessageCount < sortThreshold:
    sortStrategy = "server_sort"
    log.Printf("Using server-side SORT (%d messages < threshold %d)", currentMessageCount, sortThreshold)
    
    allUIDs, err = client.SortMessages(sortBy, sortOrder, "ALL")
    if err != nil {
        log.Printf("Server SORT failed: %v, falling back to SEARCH", err)
        // Fallback logic (Phase 4)
    }
    
case currentMessageCount <= maxSortUIDs:
    sortStrategy = "date_range"
    log.Printf("Using date-range filter (%d messages >= threshold %d)", currentMessageCount, sortThreshold)
    
    allUIDs, err = s.searchByDateRange(ctx, client, folder, sortBy, sortOrder, largeMailboxRecentDays)
    if err != nil {
        log.Printf("Date-range search failed: %v", err)
        // Fallback to plain SEARCH
        allUIDs, err = client.Search("ALL")
    }
    
default:
    sortStrategy = "limited_search"
    log.Printf("Mailbox too large for SORT (%d > %d), using limited SEARCH", currentMessageCount, maxSortUIDs)
    
    // For extremely large mailboxes, just get recent messages
    allUIDs, err = client.Search("ALL")
    if err == nil && len(allUIDs) > maxSortUIDs {
        // Take last N UIDs (most recent)
        allUIDs = allUIDs[len(allUIDs)-maxSortUIDs:]
    }
}
```

---

### Phase 3: Pagination Before Envelope Fetch

**Goal**: Never fetch more envelopes than needed for display.

#### 3.1 Extract Page UIDs First

**File**: `internal/service/message_service.go`

**Changes** in `GetMessageList()` after `UseUIDs:` label:
```go
UseUIDs:
    // Use actual UID count as total
    totalCount := len(allUIDs)
    
    // Apply pagination to UIDs FIRST (before fetching envelopes)
    pageSize := limit
    startIndex := cursorData.Page * pageSize
    
    // Calculate end index
    endIndex := startIndex + pageSize
    if endIndex > len(allUIDs) {
        endIndex = len(allUIDs)
    }
    
    // Handle cursor beyond available messages
    if startIndex >= len(allUIDs) {
        // Return empty page with metadata
        return s.emptyMessageList(totalCount, limit, cursorData.Page, folder, uidValidity), nil
    }
    
    // Extract ONLY page UIDs (e.g., 50 UIDs, not 50,000)
    pageUIDs := allUIDs[startIndex:endIndex]
    
    log.Printf("Pagination: page=%d, range=[%d:%d], pageUIDs=%d items (of %d total)",
        cursorData.Page, startIndex, endIndex, len(pageUIDs), totalCount)
    
    // Fetch envelopes for page UIDs ONLY
    var allEnvelopes []pool.MessageEnvelope
    if len(pageUIDs) > 0 {
        // Sort UIDs ascending for IMAP FETCH (required by protocol)
        sortedPageUIDs := make([]uint32, len(pageUIDs))
        copy(sortedPageUIDs, pageUIDs)
        sort.Slice(sortedPageUIDs, func(i, j int) bool { return sortedPageUIDs[i] < sortedPageUIDs[j] })
        
        log.Printf("Fetching envelopes for page UIDs: %d to %d", sortedPageUIDs[0], sortedPageUIDs[len(sortedPageUIDs)-1])
        allEnvelopes, err = client.FetchMessages(sortedPageUIDs, false)
        if err != nil {
            log.Printf("Failed to fetch message envelopes: %v", err)
            return nil, fmt.Errorf("failed to fetch messages: %w", err)
        }
        
        log.Printf("Fetched %d envelopes for page", len(allEnvelopes))
        
        // Re-order envelopes to match original pageUIDs order (preserves sort)
        envelopeMap := make(map[uint32]pool.MessageEnvelope, len(allEnvelopes))
        for _, env := range allEnvelopes {
            envelopeMap[env.UID] = env
        }
        
        orderedEnvelopes := make([]pool.MessageEnvelope, 0, len(pageUIDs))
        for _, uid := range pageUIDs {
            if env, ok := envelopeMap[uid]; ok {
                orderedEnvelopes = append(orderedEnvelopes, env)
            }
        }
        allEnvelopes = orderedEnvelopes
    }
    
    // Convert to MessageSummary
    messages := s.convertToMessageSummary(allEnvelopes, folder)
```

#### 3.2 Remove Redundant Client-Side Sort

Since we're now using server-side SORT or date-range filtered results, and we're only fetching page envelopes, we need to ensure the sort order is preserved:

```go
// Note: No client-side sort needed here because:
// 1. Server SORT already sorted the UIDs
// 2. Date-range search does client-side sort before returning UIDs
// 3. We're only fetching envelopes for already-sorted UIDs
// The sort order is preserved by maintaining UID order during fetch
```

---

### Phase 4: Connection Error Handling with Retry

**Goal**: Gracefully handle SORT timeouts and connection failures.

#### 4.1 Enhanced Fallback with Fresh Connection

**File**: `internal/service/message_service.go`

**Update SORT error handling**:
```go
allUIDs, err = client.SortMessages(sortBy, sortOrder, "ALL")
sortDuration := time.Since(sortStart)

if err != nil {
    log.Printf("Server SORT failed after %v: %v", sortDuration, err)
    
    // Categorize error
    switch {
    case isConnectionErrorForService(err):
        log.Printf("Connection dead during SORT, getting fresh connection")
        
        // Release bad session
        release()
        
        // Get fresh session
        imapCtx2, cancel2 := context.WithTimeout(ctx, 30*time.Second)
        defer cancel2()
        
        client, release, err = s.sessions.Acquire(imapCtx2, accountID, imapConfig)
        if err != nil {
            return nil, fmt.Errorf("failed to get fresh connection: %w", err)
        }
        defer release()
        
        // Re-select folder
        _, err = client.SelectFolder(folder)
        if err != nil {
            return nil, fmt.Errorf("failed to select folder: %w", err)
        }
        
        // Retry with SEARCH (not SORT - connection was unstable)
        log.Printf("Retrying with SEARCH on fresh connection")
        allUIDs, err = client.Search("ALL")
        
    case isTimeoutError(err):
        log.Printf("SORT timeout, falling back to date-range search")
        
        // Don't retry SORT - use date-range instead
        allUIDs, err = s.searchByDateRange(ctx, client, folder, sortBy, sortOrder, largeMailboxRecentDays)
        
    default:
        log.Printf("SORT error, falling back to SEARCH")
        allUIDs, err = client.Search("ALL")
    }
    
    if err != nil {
        return nil, fmt.Errorf("search failed: %w", err)
    }
    log.Printf("Fallback succeeded: %d UIDs", len(allUIDs))
} else {
    log.Printf("SORT completed in %v: %d UIDs", sortDuration, len(allUIDs))
    if sortDuration > 5*time.Second {
        log.Printf("WARN: SORT took >5s, consider date-range filtering for this mailbox")
    }
}
```

#### 4.2 Add Timeout Error Detection

**File**: `internal/service/message_service.go`

**Add helper function**:
```go
// isTimeoutError checks if an error is a timeout
func isTimeoutError(err error) bool {
    if err == nil {
        return false
    }
    
    errStr := err.Error()
    return strings.Contains(errStr, "timeout") ||
           strings.Contains(errStr, "i/o timeout") ||
           strings.Contains(errStr, "context deadline exceeded")
}
```

---

### Phase 5: Background Preload for Smooth Scrolling

**Goal**: Pre-fetch next pages while user views current page.

#### 5.1 Strategic Envelope Preload

**File**: `internal/service/message_service.go`

**Add preload function**:
```go
// preloadNextPages preloads envelopes for the next 2 pages in background
// This ensures instant display when user scrolls
func (s *MessageService) preloadNextPages(
    ctx context.Context,
    accountID string,
    allUIDs []uint32,
    currentPage int,
    pageSize int,
    folder string,
) {
    if s.cache == nil || len(allUIDs) == 0 {
        return
    }
    
    // Calculate UIDs for next 2 pages
    nextPageStart := (currentPage + 1) * pageSize
    if nextPageStart >= len(allUIDs) {
        return  // No more pages
    }
    
    nextPageEnd := nextPageStart + pageSize
    if nextPageEnd > len(allUIDs) {
        nextPageEnd = len(allUIDs)
    }
    
    page2Start := nextPageEnd
    if page2Start >= len(allUIDs) {
        return  // Only one more page
    }
    
    page2End := page2Start + pageSize
    if page2End > len(allUIDs) {
        page2End = len(allUIDs)
    }
    
    // Combine UIDs for both pages
    preloadUIDs := append(allUIDs[nextPageStart:nextPageEnd], allUIDs[page2Start:page2End]...)
    
    if len(preloadUIDs) == 0 {
        return
    }
    
    // Preload in background (non-blocking)
    go func() {
        // Create background context with timeout
        bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        
        // Get IMAP session
        imapConfig := pool.IMAPConfig{
            // ... get from account
        }
        
        client, release, err := s.sessions.Acquire(bgCtx, accountID, imapConfig)
        if err != nil {
            log.Printf("Preload: failed to get session: %v", err)
            return
        }
        defer release()
        
        // Select folder
        _, err = client.SelectFolder(folder)
        if err != nil {
            log.Printf("Preload: failed to select folder: %v", err)
            return
        }
        
        // Sort UIDs for FETCH (ascending order required)
        sortedUIDs := make([]uint32, len(preloadUIDs))
        copy(sortedUIDs, preloadUIDs)
        sort.Slice(sortedUIDs, func(i, j int) bool { return sortedUIDs[i] < sortedUIDs[j] })
        
        // Fetch envelopes
        envelopes, err := client.FetchMessages(sortedUIDs, false)
        if err != nil {
            log.Printf("Preload: failed to fetch envelopes: %v", err)
            return
        }
        
        // Cache envelopes
        if err := s.cache.SetEnvelopes(bgCtx, accountID, envelopes); err != nil {
            log.Printf("Preload: failed to cache envelopes: %v", err)
            return
        }
        
        log.Printf("Preload: cached %d envelopes for pages %d-%d", 
            len(envelopes), currentPage+2, currentPage+3)
    }()
}
```

#### 5.2 Call Preload After Returning Response

**File**: `internal/service/message_service.go`

**At end of `GetMessageList()`**:
```go
// Background preload for next pages
go s.preloadNextPages(ctx, accountID, allUIDs, cursorData.Page, pageSize, folder)

log.Printf("Fetched %d messages from folder %s for account %s (UID validity: %d, strategy: %s)", 
    len(messages), folder, accountID, uidValidity, sortStrategy)
return messageList, nil
```

---

### Phase 6: Observability and Metrics

**Goal**: Track sorting strategy effectiveness and performance.

#### 6.1 Add Sorting Metrics

**File**: `internal/service/message_service.go`

**Add logging with metrics**:
```go
// Log sorting strategy and performance
log.Printf("SORT_STATS: account=%s folder=%s strategy=%s message_count=%d duration_ms=%d uids_returned=%d",
    accountID, folder, sortStrategy, currentMessageCount, sortDuration.Milliseconds(), len(allUIDs))

// Track cache hit rate
if cacheHit {
    log.Printf("CACHE_STATS: account=%s folder=%s cache=hit modseq=%d", accountID, folder, highestModSeq)
} else {
    log.Printf("CACHE_STATS: account=%s folder=%s cache=miss reason=%s", accountID, folder, cacheMissReason)
}
```

#### 6.2 Add Configuration for Tuning

**File**: `internal/config/config.go`

**Add sorting configuration**:
```go
type SortingConfig struct {
    Threshold            int           `json:"threshold"`             // Use date-range above this count
    DateRangeDays        int           `json:"date_range_days"`       // Days to include for large mailboxes
    MaxSortUIDs          int           `json:"max_sort_uids"`         // Abort SORT above this
    UIDListCacheTTL      time.Duration `json:"uid_list_cache_ttl"`    // Cache TTL for UID lists
    EnablePreload        bool          `json:"enable_preload"`        // Enable background preload
}

// Add to Config struct
Sorting SortingConfig `json:"sorting"`

// Add defaults
Sorting: SortingConfig{
    Threshold:       10000,
    DateRangeDays:   90,
    MaxSortUIDs:     50000,
    UIDListCacheTTL: 10 * time.Minute,
    EnablePreload:   true,
},
```

---

## Testing Strategy

### Unit Tests

1. **Test `isCacheValid()`** with various modseq/count scenarios
2. **Test `searchByDateRange()`** with mock IMAP client
3. **Test `sortEnvelopes()`** with different sort fields/orders
4. **Test `preloadNextPages()`** with mock cache

### Integration Tests

1. **Small mailbox (<1000 messages)**: Verify server SORT is used
2. **Medium mailbox (10,000 messages)**: Verify server SORT is used
3. **Large mailbox (50,000 messages)**: Verify date-range filtering is used
4. **Huge mailbox (100,000+ messages)**: Verify limited SEARCH is used
5. **Cache hit scenario**: Verify SORT is skipped on cache hit
6. **SORT timeout**: Verify fallback to date-range works
7. **Connection failure**: Verify fresh connection is obtained

### Load Tests

1. **1000 accounts × 50,000 messages**: Measure memory usage
2. **Concurrent requests**: Measure latency under load
3. **Cache warm vs cold**: Compare performance

---

## Rollout Plan

| Phase | Risk | Effort | Dependencies |
|-------|------|--------|--------------|
| Phase 1 (UID Cache) | Low | 4 hours | None |
| Phase 2 (Date-Range) | Medium | 6 hours | Phase 1 |
| Phase 3 (Pagination) | High | 8 hours | Phase 2 |
| Phase 4 (Retry) | Low | 4 hours | Phase 2 |
| Phase 5 (Preload) | Medium | 6 hours | Phase 3 |
| Phase 6 (Metrics) | Low | 4 hours | All phases |

**Total**: ~32 hours development + testing

---

## Success Metrics

| Metric | Before | Target | Measurement |
|--------|--------|--------|-------------|
| **Memory per account** | O(n) UIDs | O(page) UIDs | Heap profiling |
| **SORT timeout rate** | ~5% | <0.1% | Error logs |
| **Cache hit rate** | N/A | >80% | Cache stats |
| **P95 latency (large mailbox)** | 30s+ | <3s | Request metrics |
| **503 errors** | Present | Zero | Error tracking |

---

## Configuration Changes

Add to `config.json`:

```json
{
  "sorting": {
    "threshold": 10000,
    "date_range_days": 90,
    "max_sort_uids": 50000,
    "uid_list_cache_ttl": "10m",
    "enable_preload": true
  },
  "imap": {
    "command_timeout": "8s"
  }
}
```

---

## Conclusion

This plan implements a **hybrid sorting strategy** based on best practices from Roundcube, Yahoo/AOL, and RFC 5256:

1. **Cache-first**: Avoid repeated SORT operations
2. **Size-aware**: Use date-range filtering for large mailboxes
3. **Pagination**: Fetch only what's needed for display
4. **Progressive**: Preload next pages in background
5. **Resilient**: Handle timeouts and connection failures gracefully

The result is a **scalable, efficient sorting system** that handles 1000s of accounts with large mailboxes without memory pressure or timeout issues.
