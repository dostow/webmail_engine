package pool

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"webmail_engine/internal/models"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// IMAPAdapter wraps go-imap/v2 to match our interface
type IMAPAdapter struct {
	client      *imapclient.Client
	conn        net.Conn
	mu          sync.Mutex
	selectedBox *imap.SelectData
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

// Search performs an IMAP search
func (a *IMAPAdapter) Search(criteria string) ([]uint32, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Parse search criteria
	searchCriteria, err := parseSearchCriteria(criteria)
	if err != nil {
		return nil, fmt.Errorf("invalid search criteria: %w", err)
	}

	// Use UID SEARCH
	searchData, err := a.client.UIDSearch(&searchCriteria, nil).Wait()
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

// HasSortCapability checks if the server supports SORT extension (RFC 5256)
func (a *IMAPAdapter) HasSortCapability() bool {
	return a.client.Caps().Has(imap.CapSort)
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

	// Parse criteria - simplified parser for common cases
	parts := strings.Fields(criteria)
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
					Value: parts[i],
				})
			}
		case "TO":
			if i+1 < len(parts) {
				i++
				searchCriteria.Header = append(searchCriteria.Header, imap.SearchCriteriaHeaderField{
					Key:   "To",
					Value: parts[i],
				})
			}
		case "SUBJECT":
			if i+1 < len(parts) {
				i++
				searchCriteria.Header = append(searchCriteria.Header, imap.SearchCriteriaHeaderField{
					Key:   "Subject",
					Value: parts[i],
				})
			}
		case "BODY":
			if i+1 < len(parts) {
				i++
				searchCriteria.Body = append(searchCriteria.Body, parts[i])
			}
		case "SINCE":
			if i+1 < len(parts) {
				i++
				if date, err := time.Parse("02-Jan-2006", parts[i]); err == nil {
					searchCriteria.Since = date
				}
			}
		case "BEFORE":
			if i+1 < len(parts) {
				i++
				if date, err := time.Parse("02-Jan-2006", parts[i]); err == nil {
					searchCriteria.Before = date
				}
			}
		}
	}

	return searchCriteria, nil
}

// GetClient returns the underlying IMAP client
func (a *IMAPAdapter) GetClient() *imapclient.Client {
	return a.client
}

// HasCapability checks if the server supports a capability
func (a *IMAPAdapter) HasCapability(cap string) bool {
	return a.client.Caps().Has(imap.Cap(cap))
}

// SelectedFolder returns the currently selected folder
func (a *IMAPAdapter) SelectedFolder() *imap.SelectData {
	return a.selectedBox
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
