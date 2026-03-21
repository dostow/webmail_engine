package pool

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"webmail_engine/internal/models"
)

// IMAPClient represents an IMAP client connection
type IMAPClient struct {
	conn           *Connection
	mu             sync.Mutex
	seqNum         uint32
	capabilities   []string
	selectedFolder string
	uidValidity    uint32
	highestModSeq  uint64
	qresyncEnabled bool
}

// IMAPConfig represents IMAP server configuration
type IMAPConfig struct {
	Host        string
	Port        int
	Username    string
	Password    string
	Encryption  models.EncryptionType
	ProxyConfig *models.ProxySettings
}

// FolderInfo represents IMAP folder information
type FolderInfo struct {
	Name          string
	Delimiter     string
	Attributes    []string
	Messages      int
	Recent        int
	Unseen        int
	UIDNext       uint32
	UIDValidity   uint32
	HighestModSeq uint64
}

// FolderStatus represents the current status of a folder for sync purposes
type FolderStatus struct {
	Messages    uint32
	UIDNext     uint32
	UIDValidity uint32
	Recent      uint32
}

// MessageEnvelope represents IMAP message envelope
type MessageEnvelope struct {
	UID          uint32
	Flags        []string
	InternalDate time.Time
	From         []models.Contact
	To           []models.Contact
	Subject      string
	Date         time.Time
	MessageID    string
	Size         int64
}

// NewIMAPClient creates a new IMAP client
func NewIMAPClient(conn *Connection) *IMAPClient {
	return &IMAPClient{
		conn: conn,
	}
}

// ConnectIMAP establishes an IMAP connection
func ConnectIMAP(ctx context.Context, config IMAPConfig) (*IMAPClient, error) {
	// Create server config
	host := fmt.Sprintf("%s:%d", config.Host, config.Port)

	var dialer net.Dialer
	dialer.Timeout = 30 * time.Second

	netConn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to IMAP server: %w", err)
	}

	var tlsConn *tls.Conn
	if config.Encryption == models.EncryptionSSL || config.Encryption == models.EncryptionTLS {
		tlsConfig := &tls.Config{
			ServerName: config.Host,
			MinVersion: tls.VersionTLS12,
		}
		tlsConn = tls.Client(netConn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			netConn.Close()
			return nil, fmt.Errorf("TLS handshake failed: %w", err)
		}
	}

	conn := &Connection{
		ID:         fmt.Sprintf("imap-%d", time.Now().UnixNano()),
		AccountID:  config.Username,
		ConnType:   models.ProtocolIMAP,
		NetConn:    netConn,
		TLSConn:    tlsConn,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		InUse:      true,
	}

	client := &IMAPClient{
		conn: conn,
	}

	// Read greeting
	if err := client.readGreeting(); err != nil {
		conn.Close()
		return nil, err
	}

	// Authenticate
	if err := client.authenticate(config.Username, config.Password); err != nil {
		conn.Close()
		return nil, err
	}

	// Refresh capabilities after authentication
	if err := client.refreshCapabilities(); err != nil {
		log.Printf("Warning: failed to refresh capabilities: %v", err)
	}

	// Enable QRESYNC if supported (must be done before SELECT/EXAMINE)
	if client.HasQResync() {
		if err := client.EnableQResync(); err != nil {
			log.Printf("Warning: failed to enable QRESYNC: %v", err)
		} else {
			log.Printf("QRESYNC enabled successfully")
		}
	}

	return client, nil
}

// Capabilities returns the IMAP capabilities advertised by the server
func (c *IMAPClient) Capabilities() []string {
	return c.capabilities
}

// HasCapability checks if the server supports a specific capability
func (c *IMAPClient) HasCapability(cap string) bool {
	for _, c2 := range c.capabilities {
		if strings.EqualFold(c2, cap) {
			return true
		}
	}
	return false
}

// HasQResync checks if the server supports QRESYNC extension
func (c *IMAPClient) HasQResync() bool {
	for _, cap := range c.capabilities {
		if strings.EqualFold(cap, "QRESYNC") {
			return true
		}
	}
	return false
}

// HasCondStore checks if the server supports CONDSTORE extension
func (c *IMAPClient) HasCondStore() bool {
	for _, cap := range c.capabilities {
		if strings.EqualFold(cap, "CONDSTORE") || strings.EqualFold(cap, "QRESYNC") {
			return true
		}
	}
	return false
}

// HasSort checks if the server supports SORT extension
func (c *IMAPClient) HasSort() bool {
	for _, cap := range c.capabilities {
		if strings.EqualFold(cap, "SORT") {
			return true
		}
	}
	return false
}

// HasSearchRes checks if the server supports SEARCHRES extension
func (c *IMAPClient) HasSearchRes() bool {
	for _, cap := range c.capabilities {
		if strings.EqualFold(cap, "SEARCHRES") {
			return true
		}
	}
	return false
}

// IsQResyncEnabled returns true if QRESYNC has been enabled for this session
func (c *IMAPClient) IsQResyncEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.qresyncEnabled
}

// EnableQResync sends the ENABLE QRESYNC command to the server
// Must be called after authentication and before SELECT/EXAMINE
// Returns nil if server doesn't support QRESYNC (no-op)
func (c *IMAPClient) EnableQResync() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.HasQResync() {
		return nil // Server doesn't support it, no-op
	}

	// Send ENABLE QRESYNC command per RFC 7162
	response, err := c.sendCommand("ENABLE QRESYNC")
	if err != nil {
		return fmt.Errorf("failed to enable QRESYNC: %w", err)
	}

	// Check for ENABLED response
	if strings.Contains(response, "ENABLED") {
		c.qresyncEnabled = true
		return nil
	}

	// Some servers may not return ENABLED but still enable it
	// If no error, assume success
	c.qresyncEnabled = true
	return nil
}

// GetHighestModSeq returns the highest modification sequence for the selected folder
func (c *IMAPClient) GetHighestModSeq() uint64 {
	return c.highestModSeq
}

// GetUIDValidity returns the UID validity value for the selected folder
func (c *IMAPClient) GetUIDValidity() uint32 {
	return c.uidValidity
}

// refreshCapabilities fetches and caches the server capabilities
func (c *IMAPClient) refreshCapabilities() error {
	response, err := c.sendCommand("CAPABILITY")
	if err != nil {
		return err
	}

	c.capabilities = parseCapabilityResponse(response)
	return nil
}

// Close closes the IMAP connection
func (c *IMAPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.NetConn.Close()
		return nil
	}
	return nil
}

// ListFolders lists all folders
func (c *IMAPClient) ListFolders() ([]FolderInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Send LIST command
	response, err := c.sendCommand("LIST \"\" \"*\"")
	if err != nil {
		return nil, err
	}

	var folders []FolderInfo

	// Parse LIST responses
	for _, line := range strings.Split(response, "\n") {
		if strings.Contains(line, "LIST") {
			folder := parseListResponse(line)
			if folder.Name != "" {
				folders = append(folders, folder)
			}
		}
	}

	return folders, nil
}

// SelectFolder selects a folder for operations
func (c *IMAPClient) SelectFolder(folder string) (*FolderInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Use SELECT with QRESYNC if supported to get HIGHESTMODSEQ
	var response string
	var err error

	if c.HasQResync() {
		// Request QRESYNC information
		response, err = c.sendCommand(fmt.Sprintf("SELECT \"%s\"", folder))
	} else {
		response, err = c.sendCommand(fmt.Sprintf("SELECT \"%s\"", folder))
	}

	if err != nil {
		return nil, err
	}

	info := parseSelectResponse(response)
	info.Name = folder
	c.selectedFolder = folder

	// Capture UIDVALIDITY and HIGHESTMODSEQ from response
	c.uidValidity = info.UIDValidity
	c.highestModSeq = info.HighestModSeq

	return &info, nil
}

// GetFolderStatus gets the current status of a folder including UID validity information
func (c *IMAPClient) GetFolderStatus(folder string) (*FolderStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Use EXAMINE to get folder status without modifying flags
	response, err := c.sendCommand(fmt.Sprintf("EXAMINE \"%s\"", folder))
	if err != nil {
		return nil, err
	}

	status := parseFolderStatusResponse(response)
	return &status, nil
}

// FetchMessages fetches message metadata
func (c *IMAPClient) FetchMessages(uids []uint32, includeBody bool) ([]MessageEnvelope, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	uidSet := buildUIDSet(uids)
	command := fmt.Sprintf("UID FETCH %s (UID FLAGS INTERNALDATE ENVELOPE RFC822.SIZE)", uidSet)

	response, err := c.sendCommand(command)
	if err != nil {
		return nil, err
	}

	return parseFetchResponse(response)
}

// FetchMessagesWithModSeq fetches message metadata with CONDSTORE QRESYNC support
// Returns messages that have changed since the specified modseq, plus the highest modseq seen
func (c *IMAPClient) FetchMessagesWithModSeq(uids []uint32, knownModSeq uint64) ([]MessageEnvelope, uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	uidSet := buildUIDSet(uids)
	var command string

	// Use CHANGEDSINCE modifier if CONDSTORE/QRESYNC is available and modseq is provided
	if c.HasCondStore() && knownModSeq > 0 {
		command = fmt.Sprintf("UID FETCH %s (UID FLAGS INTERNALDATE ENVELOPE RFC822.SIZE) (CHANGEDSINCE %d)", uidSet, knownModSeq)
	} else {
		command = fmt.Sprintf("UID FETCH %s (UID FLAGS INTERNALDATE ENVELOPE RFC822.SIZE)", uidSet)
	}

	response, err := c.sendCommand(command)
	if err != nil {
		return nil, 0, err
	}

	envelopes, highestModSeq := parseFetchResponseWithModSeq(response)
	return envelopes, highestModSeq, nil
}

// FetchMessageRaw fetches raw message content
func (c *IMAPClient) FetchMessageRaw(uid uint32) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	command := fmt.Sprintf("UID FETCH %d RFC822", uid)
	response, err := c.sendCommand(command)
	if err != nil {
		return nil, err
	}

	return extractRFC822(response)
}

// FetchMessageRawWithFlags fetches raw message content along with its flags
func (c *IMAPClient) FetchMessageRawWithFlags(uid uint32) ([]byte, []string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	command := fmt.Sprintf("UID FETCH %d (FLAGS RFC822)", uid)
	response, err := c.sendCommand(command)
	if err != nil {
		return nil, nil, err
	}

	data, err := extractRFC822(response)
	if err != nil {
		return nil, nil, err
	}

	// Extract flags - response might contain multiple lines, look for the FETCH line
	var flags []string
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		if strings.Contains(line, "FETCH") && strings.Contains(line, "FLAGS") {
			flags = extractFlags(line)
			break
		}
	}

	return data, flags, nil
}

// Search performs an IMAP search
func (c *IMAPClient) Search(criteria string) ([]uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	command := fmt.Sprintf("UID SEARCH %s", criteria)
	response, err := c.sendCommand(command)
	if err != nil {
		return nil, err
	}

	return parseSearchResponse(response)
}

// SortMessages performs server-side sorting using IMAP UID SORT command (RFC 5256)
// Returns sorted UIDs based on sort criteria and search criteria
// searchCriteria can be "ALL", "UNSEEN", "FROM name", etc.
func (c *IMAPClient) SortMessages(sortBy models.SortField, sortOrder models.SortOrder, searchCriteria string) ([]uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.HasSort() {
		return nil, fmt.Errorf("server does not support SORT extension")
	}

	// Build sort key
	sortKey := c.buildSortKey(sortBy, sortOrder)

	// Default to ALL if no search criteria
	if searchCriteria == "" {
		searchCriteria = "ALL"
	}

	// Build UID SORT command per RFC 5256
	// Format: UID SORT <sort-keys> <charset> <search-criteria>
	command := fmt.Sprintf("UID SORT %s UTF-8 %s", sortKey, searchCriteria)

	response, err := c.sendCommand(command)
	if err != nil {
		return nil, fmt.Errorf("SORT command failed: %w", err)
	}

	return parseSearchResponse(response)
}

// buildSortKey builds the sort key string for IMAP SORT command
func (c *IMAPClient) buildSortKey(sortBy models.SortField, sortOrder models.SortOrder) string {
	direction := ""
	if sortOrder == models.SortOrderDesc {
		direction = "REVERSE "
	}

	switch sortBy {
	case models.SortByDate:
		return direction + "DATE"
	case models.SortByFrom:
		return direction + "FROM"
	case models.SortBySubject:
		return direction + "SUBJECT"
	case models.SortByTo:
		return direction + "TO"
	case models.SortBySize:
		return direction + "SIZE"
	case models.SortByHasAttachments:
		// No direct SORT extension for attachments, fall back to DATE
		return direction + "DATE"
	default:
		return direction + "DATE"
	}
}

// SendCommand sends a raw IMAP command and returns the response
func (c *IMAPClient) SendCommand(command string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.sendCommand(command)
}

// Idle starts IMAP IDLE mode for real-time updates
func (c *IMAPClient) Idle(ctx context.Context, handler func(event string, data []byte)) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Send IDLE command
	if err := c.sendCommandNoResponse("IDLE"); err != nil {
		return err
	}

	// Read responses in a loop
	go func() {
		reader := bufio.NewReader(c.conn.NetConn)
		for {
			select {
			case <-ctx.Done():
				c.sendCommandNoResponse("DONE")
				return
			default:
				line, err := reader.ReadBytes('\n')
				if err != nil {
					return
				}

				lineStr := strings.TrimSpace(string(line))
				if strings.HasPrefix(lineStr, "* ") {
					event := strings.TrimPrefix(lineStr, "* ")
					handler(event, line)
				}
			}
		}
	}()

	return nil
}

// authenticate performs IMAP LOGIN
func (c *IMAPClient) authenticate(username, password string) error {
	command := fmt.Sprintf("LOGIN \"%s\" \"%s\"", username, password)
	_, err := c.sendCommand(command)
	return err
}

// readGreeting reads the initial server greeting
func (c *IMAPClient) readGreeting() error {
	reader := bufio.NewReader(c.getReader())
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read greeting: %w", err)
	}

	if !strings.HasPrefix(line, "* ") {
		return fmt.Errorf("invalid greeting: %s", line)
	}

	return nil
}

// sendCommand sends a command and reads response
func (c *IMAPClient) sendCommand(command string) (string, error) {
	c.seqNum++
	tag := fmt.Sprintf("A%04d", c.seqNum)

	writer := bufio.NewWriter(c.getWriter())
	_, err := fmt.Fprintf(writer, "%s %s\r\n", tag, command)
	if err != nil {
		return "", err
	}
	writer.Flush()

	// Read response
	reader := bufio.NewReader(c.getReader())
	var response strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return response.String(), err
		}

		response.WriteString(line)

		// Check for tagged response
		if strings.HasPrefix(line, tag) {
			if strings.Contains(line, "OK") {
				return response.String(), nil
			} else if strings.Contains(line, "NO") || strings.Contains(line, "BAD") {
				return "", fmt.Errorf("IMAP error: %s", strings.TrimSpace(line))
			}
		}
	}
}

// sendCommandNoResponse sends a command without waiting for complete response
func (c *IMAPClient) sendCommandNoResponse(command string) error {
	c.seqNum++
	tag := fmt.Sprintf("A%04d", c.seqNum)

	writer := bufio.NewWriter(c.getWriter())
	_, err := fmt.Fprintf(writer, "%s %s\r\n", tag, command)
	if err != nil {
		return err
	}
	return writer.Flush()
}

// getReader returns the appropriate reader
func (c *IMAPClient) getReader() io.Reader {
	if c.conn.TLSConn != nil {
		return c.conn.TLSConn
	}
	return c.conn.NetConn
}

// getWriter returns the appropriate writer
func (c *IMAPClient) getWriter() io.Writer {
	if c.conn.TLSConn != nil {
		return c.conn.TLSConn
	}
	return c.conn.NetConn
}

// Helper functions for parsing IMAP responses

func parseListResponse(line string) FolderInfo {
	var folder FolderInfo

	// Parse: * LIST (\HasNoChildren) "/" "INBOX"
	parts := strings.Split(line, " ")
	if len(parts) >= 4 {
		// Extract attributes
		for i, part := range parts {
			if strings.HasPrefix(part, "(") {
				attrs := strings.Trim(part, "()")
				folder.Attributes = strings.Split(attrs, " ")
			}
			if i > 0 && !strings.HasPrefix(parts[i-1], "(") && !strings.HasSuffix(parts[i-1], ")") {
				if delimiter, err := strconv.Unquote(part); err == nil && len(delimiter) == 1 {
					folder.Delimiter = delimiter
				}
			}
			if name, err := strconv.Unquote(part); err == nil {
				folder.Name = name
			}
		}
	}

	return folder
}

func parseSelectResponse(response string) FolderInfo {
	var info FolderInfo

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, "EXISTS") {
			if n, err := extractNumber(line); err == nil {
				info.Messages = n
			}
		} else if strings.Contains(line, "RECENT") {
			if n, err := extractNumber(line); err == nil {
				info.Recent = n
			}
		} else if strings.Contains(line, "UNSEEN") {
			if n, err := extractNumber(line); err == nil {
				info.Unseen = n
			}
		} else if strings.Contains(line, "UIDNEXT") {
			if n, err := extractNumber(line); err == nil {
				info.UIDNext = uint32(n)
			}
		} else if strings.Contains(line, "UIDVALIDITY") {
			if n, err := extractNumber(line); err == nil {
				info.UIDValidity = uint32(n)
			}
		} else if strings.Contains(line, "HIGHESTMODSEQ") {
			// Parse HIGHESTMODSEQ value (64-bit number)
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "HIGHESTMODSEQ" && i+1 < len(parts) {
					// Remove parentheses if present
					modSeqStr := strings.Trim(parts[i+1], "()")
					if modSeq, err := strconv.ParseUint(modSeqStr, 10, 64); err == nil {
						info.HighestModSeq = modSeq
					}
					break
				}
			}
		}
	}

	return info
}

// parseFolderStatusResponse parses EXAMINE response to extract folder status
func parseFolderStatusResponse(response string) FolderStatus {
	var status FolderStatus

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, "EXISTS") {
			if n, err := extractNumber(line); err == nil {
				status.Messages = uint32(n)
			}
		} else if strings.Contains(line, "RECENT") {
			if n, err := extractNumber(line); err == nil {
				status.Recent = uint32(n)
			}
		} else if strings.Contains(line, "UIDNEXT") {
			if n, err := extractNumber(line); err == nil {
				status.UIDNext = uint32(n)
			}
		} else if strings.Contains(line, "UIDVALIDITY") {
			if n, err := extractNumber(line); err == nil {
				status.UIDValidity = uint32(n)
			}
		}
	}

	return status
}

func parseFetchResponse(response string) ([]MessageEnvelope, error) {
	var envelopes []MessageEnvelope

	// Simplified parsing - production would need full IMAP response parser
	lines := strings.Split(response, "\n")
	var currentEnvelope *MessageEnvelope

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, "FETCH") {
			if currentEnvelope != nil {
				envelopes = append(envelopes, *currentEnvelope)
			}
			currentEnvelope = &MessageEnvelope{}
		}

		if currentEnvelope != nil {
			if strings.Contains(line, "UID") {
				if uid, err := extractNumber(line); err == nil {
					currentEnvelope.UID = uint32(uid)
				}
			}
			if strings.Contains(line, "RFC822.SIZE") {
				if size, err := extractNumber(line); err == nil {
					currentEnvelope.Size = int64(size)
				}
			}
			if strings.Contains(line, "FLAGS") {
				currentEnvelope.Flags = extractFlags(line)
			}
		}
	}

	if currentEnvelope != nil {
		envelopes = append(envelopes, *currentEnvelope)
	}

	return envelopes, nil
}

// parseFetchResponseWithModSeq parses FETCH response and extracts highest modseq
// Returns envelopes and the highest modification sequence seen
func parseFetchResponseWithModSeq(response string) ([]MessageEnvelope, uint64) {
	var envelopes []MessageEnvelope
	var highestModSeq uint64

	lines := strings.Split(response, "\n")
	var currentEnvelope *MessageEnvelope

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.Contains(line, "FETCH") {
			if currentEnvelope != nil {
				envelopes = append(envelopes, *currentEnvelope)
			}
			currentEnvelope = &MessageEnvelope{}
		}

		if currentEnvelope != nil {
			if strings.Contains(line, "UID") {
				if uid, err := extractNumber(line); err == nil {
					currentEnvelope.UID = uint32(uid)
				}
			}
			if strings.Contains(line, "RFC822.SIZE") {
				if size, err := extractNumber(line); err == nil {
					currentEnvelope.Size = int64(size)
				}
			}
			if strings.Contains(line, "FLAGS") {
				currentEnvelope.Flags = extractFlags(line)
			}
		}

		// Extract MODSEQ from response: MODSEQ (12345)
		if strings.Contains(line, "MODSEQ") {
			if modSeq := extractModSeq(line); modSeq > highestModSeq {
				highestModSeq = modSeq
			}
		}
	}

	if currentEnvelope != nil {
		envelopes = append(envelopes, *currentEnvelope)
	}

	return envelopes, highestModSeq
}

// extractModSeq extracts modification sequence from a line
func extractModSeq(line string) uint64 {
	// Look for MODSEQ (12345) pattern
	startIdx := strings.Index(line, "MODSEQ")
	if startIdx == -1 {
		return 0
	}

	// Find the number after MODSEQ
	rest := line[startIdx+6:]
	rest = strings.TrimSpace(rest)

	// Remove parentheses if present
	rest = strings.Trim(rest, "()")

	// Extract digits
	var digits strings.Builder
	for _, r := range rest {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		} else if digits.Len() > 0 {
			break
		}
	}

	if digits.Len() == 0 {
		return 0
	}

	modSeq, err := strconv.ParseUint(digits.String(), 10, 64)
	if err != nil {
		return 0
	}

	return modSeq
}

func parseSearchResponse(response string) ([]uint32, error) {
	var uids []uint32

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "* SEARCH") {
			parts := strings.Fields(line)
			for _, part := range parts[2:] {
				if uid, err := strconv.ParseUint(part, 10, 32); err == nil {
					uids = append(uids, uint32(uid))
				}
			}
		}
	}

	return uids, nil
}

// parseCapabilityResponse parses CAPABILITY response
func parseCapabilityResponse(response string) []string {
	var capabilities []string

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "* CAPABILITY") {
			parts := strings.Fields(line)
			for _, part := range parts[2:] {
				capabilities = append(capabilities, strings.TrimSpace(part))
			}
		}
	}

	return capabilities
}

// UIDFetchVanished fetches messages that have been expunged since a given mod-sequence
// Requires QRESYNC support (RFC 7162)
func (c *IMAPClient) UIDFetchVanished(sinceModSeq uint64) ([]uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.HasQResync() {
		return nil, fmt.Errorf("QRESYNC not supported by server")
	}

	command := fmt.Sprintf("UID FETCH 1:* (FLAGS) (CHANGEDSINCE %d VANISHED)", sinceModSeq)
	response, err := c.sendCommand(command)
	if err != nil {
		return nil, err
	}

	return parseVanishedResponse(response)
}

// parseVanishedResponse parses VANISHED response to get expunged UIDs
func parseVanishedResponse(response string) ([]uint32, error) {
	var vanished []uint32

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "VANISHED") {
			// Parse: * VANISHED (EARLIER) 41,43:116,118
			// Find the UID list part
			startIdx := strings.Index(line, "(")
			if startIdx == -1 {
				continue
			}

			// Find the content after parentheses
			content := line[startIdx:]
			parts := strings.FieldsFunc(content, func(r rune) bool {
				return r == '(' || r == ')' || r == ' '
			})

			for _, part := range parts {
				if part == "EARLIER" {
					continue
				}
				// Parse UID ranges (e.g., "43:116")
				if strings.Contains(part, ":") {
					rangeParts := strings.Split(part, ":")
					if len(rangeParts) == 2 {
						start, err1 := strconv.ParseUint(rangeParts[0], 10, 32)
						end, err2 := strconv.ParseUint(rangeParts[1], 10, 32)
						if err1 == nil && err2 == nil {
							for uid := start; uid <= end; uid++ {
								vanished = append(vanished, uint32(uid))
							}
						}
					}
				} else {
					if uid, err := strconv.ParseUint(part, 10, 32); err == nil {
						vanished = append(vanished, uint32(uid))
					}
				}
			}
		}
	}

	return vanished, nil
}

func extractRFC822(response string) ([]byte, error) {
	// Find the literal data between braces
	start := strings.Index(response, "{")
	end := strings.Index(response, "}")

	if start == -1 || end == -1 {
		return nil, fmt.Errorf("invalid RFC822 response")
	}

	// Extract size
	sizeStr := response[start+1 : end]
	size, err := strconv.Atoi(sizeStr)
	if err != nil {
		return nil, err
	}

	// Extract data (simplified - production would handle literals properly)
	dataStart := end + 2 // Skip } and \r\n
	if dataStart >= len(response) {
		return nil, fmt.Errorf("invalid RFC822 response")
	}

	data := []byte(response[dataStart:])
	if len(data) > size {
		data = data[:size]
	}

	return data, nil
}

func buildUIDSet(uids []uint32) string {
	if len(uids) == 0 {
		return ""
	}

	if len(uids) == 1 {
		return strconv.Itoa(int(uids[0]))
	}

	// Build range if consecutive
	var sets []string
	start := uids[0]
	end := uids[0]

	for i := 1; i < len(uids); i++ {
		if uids[i] == end+1 {
			end = uids[i]
		} else {
			if start == end {
				sets = append(sets, strconv.Itoa(int(start)))
			} else {
				sets = append(sets, fmt.Sprintf("%d:%d", start, end))
			}
			start = uids[i]
			end = uids[i]
		}
	}

	if start == end {
		sets = append(sets, strconv.Itoa(int(start)))
	} else {
		sets = append(sets, fmt.Sprintf("%d:%d", start, end))
	}

	return strings.Join(sets, ",")
}

func extractNumber(line string) (int, error) {
	parts := strings.Fields(line)
	for _, part := range parts {
		if n, err := strconv.Atoi(part); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("no number found in line")
}

func extractFlags(line string) []string {
	start := strings.Index(line, "FLAGS (")
	if start == -1 {
		// Try without parentheses for some servers
		start = strings.Index(line, "FLAGS")
		if start == -1 {
			return nil
		}
		// Basic parsing for space separated flags after FLAGS
		parts := strings.Fields(line[start+5:])
		var flags []string
		for _, p := range parts {
			if strings.HasPrefix(p, "\\") {
				flags = append(flags, p)
			} else if strings.HasPrefix(p, "(") || strings.HasPrefix(p, ")") {
				continue
			} else {
				break
			}
		}
		return flags
	}
	start += 7
	end := strings.Index(line[start:], ")")
	if end == -1 {
		return nil
	}
	flagsStr := line[start : start+end]
	return strings.Fields(flagsStr)
}

// StreamMessage streams message content in chunks
func (c *IMAPClient) StreamMessage(uid uint32, chunkSize int, handler func(chunk []byte) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	command := fmt.Sprintf("UID FETCH %d RFC822", uid)
	if err := c.sendCommandNoResponse(command); err != nil {
		return err
	}

	reader := bufio.NewReader(c.getReader())
	var buffer bytes.Buffer

	for {
		chunk := make([]byte, chunkSize)
		n, err := reader.Read(chunk)
		if n > 0 {
			buffer.Write(chunk[:n])
			if err := handler(chunk[:n]); err != nil {
				return err
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	return nil
}
