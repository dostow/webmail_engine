package pool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"webmail_engine/internal/models"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Type aliases for imapclient types used in sorting
type (
	SortKey       = imapclient.SortKey
	SortCriterion = imapclient.SortCriterion
	SortOptions   = imapclient.SortOptions
)

// SortKey constants
const (
	SortKeyArrival SortKey = imapclient.SortKeyArrival
	SortKeyCc      SortKey = imapclient.SortKeyCc
	SortKeyDate    SortKey = imapclient.SortKeyDate
	SortKeyFrom    SortKey = imapclient.SortKeyFrom
	SortKeySize    SortKey = imapclient.SortKeySize
	SortKeySubject SortKey = imapclient.SortKeySubject
	SortKeyTo      SortKey = imapclient.SortKeyTo
)

// IMAPAdapter wraps go-imap/v2 to match our interface
type IMAPAdapter struct {
	client         *imapclient.Client
	conn           net.Conn
	mu             sync.Mutex
	selectedBox    *imap.SelectData
	invalidateFunc func() // Callback to invalidate this session in the pool
}

// ConnectIMAPv2 establishes connection using go-imap/v2
func ConnectIMAPv2(ctx context.Context, config IMAPConfig) (*IMAPAdapter, error) {
	host := fmt.Sprintf("%s:%d", config.Host, config.Port)

	var client *imapclient.Client
	var err error

	// Connect based on encryption type
	switch config.Encryption {
	case models.EncryptionSSL, models.EncryptionTLS:
		// Direct TLS connection
		client, err = imapclient.DialTLS(host, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to IMAP server: %w", err)
		}

	case models.EncryptionStartTLS:
		// Start with plain connection, then upgrade
		client, err = imapclient.DialStartTLS(host, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to IMAP server: %w", err)
		}

	case models.EncryptionNone:
		// Plain connection - not recommended for production
		client, err = imapclient.DialInsecure(host, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to IMAP server: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported encryption type: %v", config.Encryption)
	}

	adapter := &IMAPAdapter{
		client: client,
	}

	// Authenticate
	if err := adapter.authenticate(config.Username, config.Password); err != nil {
		client.Close()
		return nil, err
	}

	// Enable QRESYNC if supported (must be done before SELECT/EXAMINE)
	if adapter.HasQResync() {
		if err := adapter.enableQResync(); err != nil {
			log.Printf("Warning: failed to enable QRESYNC: %v", err)
		} else {
			log.Printf("QRESYNC enabled successfully")
		}
	}

	return adapter, nil
}

// authenticate performs IMAP LOGIN
func (a *IMAPAdapter) authenticate(username, password string) error {
	if err := a.client.Login(username, password).Wait(); err != nil {
		// Check for authentication-specific errors
		if isAuthenticationError(err) {
			return fmt.Errorf("authentication failed: %w", models.ErrMailServerAuthFailed)
		}
		return fmt.Errorf("authentication failed: %w", err)
	}
	return nil
}

// isAuthenticationError checks if an error is an authentication failure
func isAuthenticationError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Common IMAP authentication failure indicators
	authIndicators := []string{
		"authentication failed",
		"LOGIN failed",
		"AUTHENTICATE failed",
		"invalid credentials",
		"bad credentials",
		"username or password",
		"NO [AUTHENTICATIONFAILED]",
		"NO [UNAVAILABLE]", // Temporary auth failure (e.g., server-side issue)
		"NO [AUTHORIZATIONFAILED]",
		"rejected",
	}
	for _, indicator := range authIndicators {
		if strings.Contains(strings.ToUpper(errStr), strings.ToUpper(indicator)) {
			return true
		}
	}
	return false
}

// isConnectionError checks if an error is a network/connection error that indicates
// the connection is dead and should be invalidated
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// Check for net.OpError which wraps network errors
	var netOpErr *net.OpError
	if errors.As(err, &netOpErr) {
		return true
	}

	// Check for net.Error (includes timeout and temporary errors)
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// Check for EOF (connection closed by remote)
	if errors.Is(err, io.EOF) {
		return true
	}

	// Check for closed network connection errors
	// These are wrapped in various ways, so we need string matching as fallback
	errStr := err.Error()
	if strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "closed network connection") {
		return true
	}

	return false
}

// isRetryableError checks if an error is worth retrying
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Network timeouts
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// Connection errors
	if strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") {
		return true
	}

	return false
}

// withRetry executes an IMAP operation with retry logic for transient errors
// This is a generic helper function that retries operations on retryable errors
func withRetry[T any](
	operation func() (T, error),
	maxRetries int,
	operationName string,
	invalidateFunc func(),
) (T, error) {
	var lastErr error
	var zero T

	for attempt := 0; attempt < maxRetries; attempt++ {
		result, err := operation()
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			return zero, err
		}

		// Invalidate connection if it's a connection error
		if isConnectionError(err) && invalidateFunc != nil {
			invalidateFunc()
		}

		// Wait before retry (exponential backoff)
		backoff := time.Duration(100*(attempt+1)) * time.Millisecond
		log.Printf("IMAP %s failed (attempt %d/%d): %v, retrying in %v", operationName, attempt+1, maxRetries, err, backoff)
		time.Sleep(backoff)
	}

	return zero, fmt.Errorf("%s failed after %d retries: %w", operationName, maxRetries, lastErr)
}

// SetInvalidateFunc sets the callback function to invalidate this session
func (a *IMAPAdapter) SetInvalidateFunc(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.invalidateFunc = fn
}

// invalidate calls the invalidate callback if set
func (a *IMAPAdapter) invalidate() {
	a.mu.Lock()
	fn := a.invalidateFunc
	a.mu.Unlock()

	if fn != nil {
		fn()
	}
}

// Close closes the IMAP connection
func (a *IMAPAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.client != nil {
		a.client.Close()
	}
	if a.conn != nil {
		a.conn.Close()
	}
	return nil
}

// ListFolders lists all folders
func (a *IMAPAdapter) ListFolders() ([]FolderInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var folders []FolderInfo

	// Use LIST command with Collect for simplicity
	mailboxes, err := a.client.List("", "*", nil).Collect()
	if err != nil {
		if isConnectionError(err) {
			a.mu.Unlock()
			a.invalidate()
			a.mu.Lock()
		}
		return nil, err
	}

	for _, mbox := range mailboxes {
		folder := FolderInfo{
			Name:       mbox.Mailbox,
			Delimiter:  string(mbox.Delim),
			Attributes: make([]string, len(mbox.Attrs)),
		}

		for i, attr := range mbox.Attrs {
			folder.Attributes[i] = string(attr)
		}

		folders = append(folders, folder)
	}

	return folders, nil
}

// SelectFolder selects a folder for operations
func (a *IMAPAdapter) SelectFolder(folder string) (*FolderInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Use SELECT command
	selectedMbox, err := a.client.Select(folder, nil).Wait()
	if err != nil {
		if isConnectionError(err) {
			a.mu.Unlock()
			a.invalidate()
			a.mu.Lock()
		}
		return nil, fmt.Errorf("failed to select folder: %w", err)
	}

	a.selectedBox = selectedMbox

	info := &FolderInfo{
		Name:        folder,
		Messages:    int(selectedMbox.NumMessages),
		Recent:      int(selectedMbox.NumRecent),
		UIDNext:     uint32(selectedMbox.UIDNext),
		UIDValidity: uint32(selectedMbox.UIDValidity),
	}

	// Get unseen count from List status if available
	if selectedMbox.List != nil && selectedMbox.List.Status != nil && selectedMbox.List.Status.NumUnseen != nil {
		info.Unseen = int(*selectedMbox.List.Status.NumUnseen)
	}

	return info, nil
}

// GetFolderStatus gets the current status of a folder including UID validity information
func (a *IMAPAdapter) GetFolderStatus(folder string) (*FolderStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Use Select command to get folder status
	// Note: Select is used instead of Examine (not available in go-imap v2)
	// This doesn't modify message flags when just reading status
	selectedMbox, err := a.client.Select(folder, nil).Wait()
	if err != nil {
		if isConnectionError(err) {
			a.mu.Unlock()
			a.invalidate()
			a.mu.Lock()
		}
		return nil, fmt.Errorf("failed to select folder: %w", err)
	}

	status := &FolderStatus{
		Messages:    uint32(selectedMbox.NumMessages),
		Recent:      uint32(selectedMbox.NumRecent),
		UIDNext:     uint32(selectedMbox.UIDNext),
		UIDValidity: uint32(selectedMbox.UIDValidity),
	}

	return status, nil
}

// FetchMessages fetches message envelopes
func (a *IMAPAdapter) FetchMessages(uids []uint32, includeBody bool) ([]MessageEnvelope, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(uids) == 0 {
		return []MessageEnvelope{}, nil
	}

	// Convert to imap.UIDSet
	uidSet := make(imap.UIDSet, 0)
	for _, uid := range uids {
		uidSet = append(uidSet, imap.UIDRange{Start: imap.UID(uid), Stop: imap.UID(uid)})
	}

	// Create fetch options
	fetchOptions := &imap.FetchOptions{
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		RFC822Size:   true,
	}

	// Include body parts if requested
	if includeBody {
		fetchOptions.BodySection = []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierText},
		}
	}

	var envelopes []MessageEnvelope

	// Use FETCH with UID set
	messages, err := a.client.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		if isConnectionError(err) {
			a.mu.Unlock()
			a.invalidate()
			a.mu.Lock()
		}
		return nil, err
	}

	for _, msg := range messages {
		envelope := MessageEnvelope{
			UID:   uint32(msg.UID),
			Flags: make([]string, len(msg.Flags)),
			Size:  msg.RFC822Size,
			Date:  msg.InternalDate,
		}

		for i, flag := range msg.Flags {
			envelope.Flags[i] = string(flag)
		}

		if msg.Envelope != nil {
			env := msg.Envelope
			envelope.Subject = env.Subject
			envelope.MessageID = env.MessageID
			if !env.Date.IsZero() {
				envelope.Date = env.Date
			}

			// Parse From addresses
			for _, addr := range env.From {
				envelope.From = append(envelope.From, models.Contact{
					Name:    addr.Name,
					Address: addr.Addr(),
				})
			}

			// Parse To addresses
			for _, addr := range env.To {
				envelope.To = append(envelope.To, models.Contact{
					Name:    addr.Name,
					Address: addr.Addr(),
				})
			}
		}

		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

// FetchMessageRaw fetches raw message content
func (a *IMAPAdapter) FetchMessageRaw(uid uint32) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Fetch RFC822 (entire message)
	uidSet := imap.UIDSet{imap.UIDRange{Start: imap.UID(uid), Stop: imap.UID(uid)}}
	fetchOptions := &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierNone},
		},
	}

	messages, err := a.client.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		if isConnectionError(err) {
			a.mu.Unlock()
			a.invalidate()
			a.mu.Lock()
		}
		return nil, err
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("message not found")
	}

	msg := messages[0]

	// Find body section data using FindBodySection
	bodySection := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierNone}
	bodyBytes := msg.FindBodySection(bodySection)
	if len(bodyBytes) > 0 {
		return bodyBytes, nil
	}

	return nil, fmt.Errorf("message content not found")
}

// FetchMessageRawWithFlags fetches raw message content along with its flags
func (a *IMAPAdapter) FetchMessageRawWithFlags(uid uint32) ([]byte, []string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Fetch RFC822 (entire message) and FLAGS
	uidSet := imap.UIDSet{imap.UIDRange{Start: imap.UID(uid), Stop: imap.UID(uid)}}
	fetchOptions := &imap.FetchOptions{
		Flags: true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierNone},
		},
	}

	messages, err := a.client.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		if isConnectionError(err) {
			a.mu.Unlock()
			a.invalidate()
			a.mu.Lock()
		}
		return nil, nil, err
	}

	if len(messages) == 0 {
		return nil, nil, fmt.Errorf("message not found")
	}

	msg := messages[0]
	log.Printf("[DEBUG] IMAP Fetch raw with flags: UID=%d, hasFlags=%v, numFlags=%d, bodyLen=%d", 
		msg.UID, msg.Flags != nil, len(msg.Flags), len(msg.FindBodySection(&imap.FetchItemBodySection{Specifier: imap.PartSpecifierNone})))

	// Extract flags
	flags := make([]string, len(msg.Flags))
	for i, f := range msg.Flags {
		flags[i] = string(f)
	}

	// Find body section data using FindBodySection
	bodySection := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierNone}
	bodyBytes := msg.FindBodySection(bodySection)
	if len(bodyBytes) > 0 {
		return bodyBytes, flags, nil
	}

	return nil, nil, fmt.Errorf("message content not found")
}

// FetchPart fetches a specific body part of a message
func (a *IMAPAdapter) FetchPart(uid uint32, partID string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if partID == "" {
		return nil, fmt.Errorf("part ID is empty")
	}

	uidSet := imap.UIDSet{imap.UIDRange{Start: imap.UID(uid), Stop: imap.UID(uid)}}
	
	// Create body section specifier for the specific part
	// Note: go-imap/v2 uses a string-based part ID in PartSpecifier
	fetchOptions := &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifier(partID)},
		},
	}

	messages, err := a.client.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		if isConnectionError(err) {
			a.mu.Unlock()
			a.invalidate()
			a.mu.Lock()
		}
		return nil, err
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("message not found")
	}

	msg := messages[0]
	
	// Find the specific body section
	bodySection := &imap.FetchItemBodySection{Specifier: imap.PartSpecifier(partID)}
	partData := msg.FindBodySection(bodySection)
	if len(partData) > 0 {
		return partData, nil
	}

	return nil, fmt.Errorf("part %s not found in message %d", partID, uid)
}

// Search performs an IMAP search
func (a *IMAPAdapter) Search(criteria string) ([]uint32, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Parse search criteria
	searchCriteria, err := parseSearchCriteria(criteria)
	if err != nil {
		return nil, fmt.Errorf("invalid search criteria: %w", err)
	}

	// Use UID SEARCH with retry logic and timeout
	searchData, err := withRetry(
		func() (*imap.SearchData, error) {
			// UIDSearch().Wait() can hang indefinitely on large mailboxes with BODY search
			// Wrap in timeout to prevent hangs (30 second timeout for search operations)
			return searchWithTimeout(a.client, &searchCriteria, 30*time.Second)
		},
		2, // maxRetries
		"SEARCH",
		a.invalidate, // Pass invalidate function
	)
	if err != nil {
		return nil, err
	}

	// Convert to uint32 slice using AllUIDs()
	uids := searchData.AllUIDs()
	result := make([]uint32, len(uids))
	for i, uid := range uids {
		result[i] = uint32(uid)
	}

	return result, nil
}

// searchWithTimeout executes UIDSearch with a timeout to prevent hangs on large mailboxes
func searchWithTimeout(client *imapclient.Client, criteria *imap.SearchCriteria, timeout time.Duration) (*imap.SearchData, error) {
	done := make(chan struct{})
	var searchData *imap.SearchData
	var err error

	go func() {
		searchData, err = client.UIDSearch(criteria, nil).Wait()
		close(done)
	}()

	select {
	case <-done:
		return searchData, err
	case <-time.After(timeout):
		// Note: The goroutine will continue running in the background until the server responds
		// This is acceptable as the IMAP connection will be invalidated and reconnected
		return nil, fmt.Errorf("search timeout after %v (large mailbox BODY search may take longer)", timeout)
	}
}

// sortWithTimeout executes UIDSort with a timeout to prevent hangs on large mailboxes
func sortWithTimeout(client *imapclient.Client, options *SortOptions, timeout time.Duration) ([]uint32, error) {
	done := make(chan struct{})
	var result []uint32
	var err error

	go func() {
		result, err = client.UIDSort(options).Wait()
		close(done)
	}()

	select {
	case <-done:
		return result, err
	case <-time.After(timeout):
		// Note: The goroutine will continue running in the background until the server responds
		return nil, fmt.Errorf("sort timeout after %v (large mailbox SORT may take longer)", timeout)
	}
}

// HasSortCapability checks if the server supports SORT extension (RFC 5256)
func (a *IMAPAdapter) HasSortCapability() bool {
	return a.client.Caps().Has(imap.CapSort)
}

// HasSort checks if the server supports SORT extension (alias for HasSortCapability)
func (a *IMAPAdapter) HasSort() bool {
	return a.HasSortCapability()
}

// HasSearchRes checks if the server supports SEARCHRES extension (RFC 5182)
func (a *IMAPAdapter) HasSearchRes() bool {
	return a.client.Caps().Has(imap.CapSearchRes)
}

// SortMessages performs server-side sorting using IMAP UID SORT command (RFC 5256)
// Returns sorted UIDs based on sort criteria and search criteria
func (a *IMAPAdapter) SortMessages(sortBy models.SortField, sortOrder models.SortOrder, searchCriteria string) ([]uint32, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.HasSort() {
		return nil, fmt.Errorf("server does not support SORT extension")
	}

	// Parse search criteria
	searchCrit, err := parseSearchCriteria(searchCriteria)
	if err != nil {
		return nil, fmt.Errorf("invalid search criteria: %w", err)
	}

	// Build sort criteria - always use ascending order from server for consistency
	sortKey := buildSortKey(sortBy, models.SortOrderAsc)
	sortCriteria := []SortCriterion{sortKey}

	// Use UID SORT command
	sortOptions := &SortOptions{
		SearchCriteria: &searchCrit,
		SortCriteria:   sortCriteria,
	}

	// Get sorted UIDs with retry logic and timeout
	sortedUIDs, err := withRetry(
		func() ([]uint32, error) {
			// UIDSort().Wait() can hang on large mailboxes, wrap in timeout
			return sortWithTimeout(a.client, sortOptions, 30*time.Second)
		},
		2, // maxRetries
		"SORT",
		a.invalidate, // Pass invalidate function
	)
	if err != nil {
		return nil, err
	}

	// Reverse client-side if descending order requested
	// This ensures consistent behavior across all IMAP servers
	if sortOrder == models.SortOrderDesc {
		for i, j := 0, len(sortedUIDs)-1; i < j; i, j = i+1, j-1 {
			sortedUIDs[i], sortedUIDs[j] = sortedUIDs[j], sortedUIDs[i]
		}
	}

	return sortedUIDs, nil
}

// FetchMessagesWithModSeq fetches message metadata with CONDSTORE/QRESYNC support
// Returns messages that have changed since the specified modseq
func (a *IMAPAdapter) FetchMessagesWithModSeq(uids []uint32, knownModSeq uint64) ([]MessageEnvelope, uint64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(uids) == 0 {
		return []MessageEnvelope{}, 0, nil
	}

	// Convert to imap.UIDSet
	uidSet := make(imap.UIDSet, 0)
	for _, uid := range uids {
		uidSet = append(uidSet, imap.UIDRange{Start: imap.UID(uid), Stop: imap.UID(uid)})
	}

	// Create fetch options with CHANGEDSINCE if modseq provided
	fetchOptions := &imap.FetchOptions{
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		RFC822Size:   true,
	}

	if knownModSeq > 0 {
		fetchOptions.ChangedSince = knownModSeq
	}

	var envelopes []MessageEnvelope
	var highestModSeq uint64

	// Use FETCH with UID set
	messages, err := a.client.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return nil, 0, err
	}

	for _, msg := range messages {
		envelope := MessageEnvelope{
			UID:   uint32(msg.UID),
			Flags: make([]string, len(msg.Flags)),
			Size:  msg.RFC822Size,
			Date:  msg.InternalDate,
		}

		for i, flag := range msg.Flags {
			envelope.Flags[i] = string(flag)
		}

		if msg.Envelope != nil {
			env := msg.Envelope
			envelope.Subject = env.Subject
			envelope.MessageID = env.MessageID
			if !env.Date.IsZero() {
				envelope.Date = env.Date
			}

			// Parse From addresses
			for _, addr := range env.From {
				envelope.From = append(envelope.From, models.Contact{
					Name:    addr.Name,
					Address: addr.Addr(),
				})
			}

			// Parse To addresses
			for _, addr := range env.To {
				envelope.To = append(envelope.To, models.Contact{
					Name:    addr.Name,
					Address: addr.Addr(),
				})
			}
		}

		// Track highest modseq
		if msg.ModSeq > highestModSeq {
			highestModSeq = msg.ModSeq
		}

		envelopes = append(envelopes, envelope)
	}

	return envelopes, highestModSeq, nil
}

// Idle starts IMAP IDLE mode for real-time updates
func (a *IMAPAdapter) Idle(ctx context.Context, handler func(event string, data []byte)) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Create client with unilateral data handler
	idleCmd, err := a.client.Idle()
	if err != nil {
		return fmt.Errorf("failed to start IDLE: %w", err)
	}
	defer idleCmd.Close()

	done := make(chan error, 1)
	go func() {
		done <- idleCmd.Wait()
	}()

	// Wait for context cancellation or IDLE to complete
	select {
	case <-ctx.Done():
		if err := idleCmd.Close(); err != nil {
			return fmt.Errorf("failed to stop IDLE: %w", err)
		}
		return <-done
	case err := <-done:
		return err
	}
}

// parseSearchCriteria converts string criteria to imap.SearchCriteria
func parseSearchCriteria(criteria string) (imap.SearchCriteria, error) {
	criteria = strings.TrimSpace(criteria)

	// Handle special case
	if criteria == "ALL" {
		return imap.SearchCriteria{}, nil
	}

	var searchCriteria imap.SearchCriteria

	// Parse criteria - handle quoted strings properly
	parts := tokenizeCriteria(criteria)
	for i := 0; i < len(parts); i++ {
		switch strings.ToUpper(parts[i]) {
		case "UNSEEN":
			searchCriteria.NotFlag = append(searchCriteria.NotFlag, imap.FlagSeen)
		case "SEEN":
			searchCriteria.Flag = append(searchCriteria.Flag, imap.FlagSeen)
		case "FROM":
			if i+1 < len(parts) {
				i++
				searchCriteria.Header = append(searchCriteria.Header, imap.SearchCriteriaHeaderField{
					Key:   "From",
					Value: unquote(parts[i]),
				})
			}
		case "TO":
			if i+1 < len(parts) {
				i++
				searchCriteria.Header = append(searchCriteria.Header, imap.SearchCriteriaHeaderField{
					Key:   "To",
					Value: unquote(parts[i]),
				})
			}
		case "SUBJECT":
			if i+1 < len(parts) {
				i++
				searchCriteria.Header = append(searchCriteria.Header, imap.SearchCriteriaHeaderField{
					Key:   "Subject",
					Value: unquote(parts[i]),
				})
			}
		case "BODY":
			if i+1 < len(parts) {
				i++
				searchCriteria.Body = append(searchCriteria.Body, unquote(parts[i]))
			}
		case "SINCE":
			if i+1 < len(parts) {
				i++
				if date, err := time.Parse("02-Jan-2006", unquote(parts[i])); err == nil {
					searchCriteria.Since = date
				}
			}
		case "BEFORE":
			if i+1 < len(parts) {
				i++
				if date, err := time.Parse("02-Jan-2006", unquote(parts[i])); err == nil {
					searchCriteria.Before = date
				}
			}
		}
	}

	return searchCriteria, nil
}

// tokenizeCriteria splits criteria into tokens, respecting quoted strings
func tokenizeCriteria(criteria string) []string {
	var tokens []string
	var current strings.Builder
	inQuotes := false

	for _, r := range criteria {
		switch r {
		case '"':
			inQuotes = !inQuotes
		case ' ':
			if inQuotes {
				current.WriteRune(r)
			} else if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// unquote removes surrounding quotes from a string
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// buildSortKey builds the sort key for IMAP SORT command
func buildSortKey(sortBy models.SortField, sortOrder models.SortOrder) SortCriterion {
	var key SortKey

	switch sortBy {
	case models.SortByDate:
		key = SortKeyDate
	case models.SortByFrom:
		key = SortKeyFrom
	case models.SortBySubject:
		key = SortKeySubject
	case models.SortByTo:
		key = SortKeyTo
	case models.SortBySize:
		key = SortKeySize
	default:
		key = SortKeyDate
	}

	// For SortOrderDesc (newest first), we need REVERSE because DATE sort is ascending by default
	// For SortOrderAsc (oldest first), we don't use REVERSE
	// Note: go-imap/v2 Reverse=true means "reverse the natural order"
	// Natural order for DATE is oldest-first, so Reverse=true gives newest-first
	reverse := false
	if sortOrder == models.SortOrderDesc {
		reverse = true
	}

	return SortCriterion{
		Key:     key,
		Reverse: reverse,
	}
}

// GetClient returns the underlying IMAP client
func (a *IMAPAdapter) GetClient() *imapclient.Client {
	return a.client
}

// GetServerCapabilities detects and returns IMAP server capabilities
func (a *IMAPAdapter) GetServerCapabilities() *models.ServerCapabilities {
	caps := &models.ServerCapabilities{
		Capabilities: make([]string, 0),
		LastChecked:  time.Now(),
	}

	// Get raw capabilities from client
	rawCaps := a.client.Caps()

	// Convert to string slice and detect features
	for cap := range rawCaps {
		capStr := string(cap)
		caps.Capabilities = append(caps.Capabilities, capStr)

		// Detect extended capabilities
		switch strings.ToUpper(capStr) {
		case "QRESYNC":
			caps.SupportsQResync = true
			caps.SupportsCondStore = true // QRESYNC implies CONDSTORE
		case "CONDSTORE":
			caps.SupportsCondStore = true
		case "SORT", "SORT=DISPLAY":
			caps.SupportsSort = true
		case "SEARCHRES":
			caps.SupportsSearchRes = true
		case "LITERAL+":
			caps.SupportsLiteralPlus = true
		case "UTF8=ACCEPT":
			caps.SupportsUTF8Accept = true
		case "UTF8=ONLY":
			caps.SupportsUTF8Only = true
		case "MOVE":
			caps.SupportsMove = true
		case "UIDPLUS":
			caps.SupportsUIDPlus = true
		case "UNSELECT":
			caps.SupportsUnselect = true
		case "IDLE":
			caps.SupportsIdle = true
		case "STARTTLS":
			caps.SupportsStartTLS = true
		}

		// Detect AUTH capabilities
		if strings.HasPrefix(strings.ToUpper(capStr), "AUTH=") {
			authType := strings.ToUpper(strings.TrimPrefix(capStr, "AUTH="))
			switch authType {
			case "PLAIN":
				caps.SupportsAuthPlain = true
			case "LOGIN":
				caps.SupportsAuthLogin = true
			case "OAUTHBEARER", "OAUTH2":
				caps.SupportsAuthOAuth2 = true
			}
		}
	}

	// Try to get server identification via ID command (RFC 2971)
	if id, err := a.client.ID(nil).Wait(); err == nil && id != nil {
		if id.Name != "" {
			caps.ServerName = id.Name
		}
		if id.Vendor != "" {
			caps.ServerVendor = id.Vendor
		}
		if id.Version != "" {
			caps.ServerVersion = id.Version
		}
	}

	return caps
}

// RefreshCapabilities fetches fresh capabilities from the server
func (a *IMAPAdapter) RefreshCapabilities() (*models.ServerCapabilities, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Send CAPABILITY command to refresh
	if _, err := a.client.Capability().Wait(); err != nil {
		return nil, fmt.Errorf("failed to refresh capabilities: %w", err)
	}

	return a.GetServerCapabilities(), nil
}

// HasCapability checks if the server supports a capability
func (a *IMAPAdapter) HasCapability(cap string) bool {
	return a.client.Caps().Has(imap.Cap(cap))
}

// SelectedFolder returns the currently selected folder
func (a *IMAPAdapter) SelectedFolder() *imap.SelectData {
	return a.selectedBox
}

// HasQResync checks if the server supports QRESYNC extension
func (a *IMAPAdapter) HasQResync() bool {
	return a.client.Caps().Has(imap.CapQResync)
}

// HasCondStore checks if the server supports CONDSTORE extension
func (a *IMAPAdapter) HasCondStore() bool {
	return a.client.Caps().Has(imap.CapCondStore) || a.HasQResync()
}

// enableQResync sends the ENABLE QRESYNC command to the server
// Must be called after authentication and before SELECT/EXAMINE
// Returns nil if server doesn't support QRESYNC (no-op)
func (a *IMAPAdapter) enableQResync() error {
	if !a.HasQResync() {
		return nil // Server doesn't support it, no-op
	}

	// Use go-imap/v2 Enable command for QRESYNC
	// The Enable command is available in go-imap/v2 for RFC 7162 support
	_, err := a.client.Enable(imap.CapQResync).Wait()
	if err != nil {
		return fmt.Errorf("failed to enable QRESYNC: %w", err)
	}

	return nil
}

// GetHighestModSeq returns the highest modification sequence for the selected folder
func (a *IMAPAdapter) GetHighestModSeq() uint64 {
	if a.selectedBox == nil {
		return 0
	}
	return a.selectedBox.HighestModSeq
}

// GetUIDValidity returns the UID validity value for the selected folder
func (a *IMAPAdapter) GetUIDValidity() uint32 {
	if a.selectedBox == nil {
		return 0
	}
	return uint32(a.selectedBox.UIDValidity)
}

// UIDFetchVanished fetches messages that have been expunged since a given mod-sequence
// Requires QRESYNC support (RFC 7162)
func (a *IMAPAdapter) UIDFetchVanished(sinceModSeq uint64) ([]uint32, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.HasQResync() {
		return nil, fmt.Errorf("QRESYNC not supported by server")
	}

	// Use UID FETCH with CHANGEDSINCE modifier
	// Note: Full VANISHED support requires handling unilateral server responses
	// This is a simplified implementation that checks for deleted messages
	uidSet := imap.UIDSet{imap.UIDRange{Start: 1, Stop: imap.UID(^uint32(0))}}
	fetchOptions := &imap.FetchOptions{
		Flags:        true,
		ChangedSince: sinceModSeq,
	}

	var vanished []uint32

	// Fetch messages that changed since the mod-sequence
	messages, err := a.client.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return nil, err
	}

	// Check for messages with Deleted flag
	for _, msg := range messages {
		for _, flag := range msg.Flags {
			if flag == imap.FlagDeleted {
				vanished = append(vanished, uint32(msg.UID))
			}
		}
	}

	return vanished, nil
}

// Store adds or removes flags from messages
func (a *IMAPAdapter) Store(uids []uint32, flags []imap.Flag, add bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(uids) == 0 {
		return nil
	}

	// Convert to imap.UIDSet
	uidSet := imap.UIDSet{imap.UIDRange{Start: imap.UID(uids[0]), Stop: imap.UID(uids[0])}}
	for _, uid := range uids[1:] {
		uidSet = append(uidSet, imap.UIDRange{Start: imap.UID(uid), Stop: imap.UID(uid)})
	}

	// Store flags
	storeFlags := imap.StoreFlags{
		Flags: flags,
	}

	if !add {
		// Remove flags (-FLAGS)
		storeFlags.Op = imap.StoreFlagsDel
	}

	// Use Store command with UID set
	cmd := a.client.Store(uidSet, &storeFlags, nil)
	_, err := cmd.Collect()
	return err
}

// Expunge permanently removes messages marked as deleted
func (a *IMAPAdapter) Expunge() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cmd := a.client.Expunge()
	_, err := cmd.Collect()
	return err
}
// StreamMessage streams message content in chunks
func (a *IMAPAdapter) StreamMessage(uid uint32, chunkSize int, handler func(chunk []byte) error) error {
	// In production, this would use true streaming from the socket.
	// For now, we fetch the message and chunk it to satisfy the interface.
	data, err := a.FetchMessageRaw(uid)
	if err != nil {
		return err
	}

	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		if err := handler(data[i:end]); err != nil {
			return err
		}
	}
	return nil
}
