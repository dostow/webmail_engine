package pool

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"webmail_engine/internal/models"
)

// IMAPClient represents an IMAP client connection
type IMAPClient struct {
	conn       *Connection
	mu         sync.Mutex
	seqNum     uint32
	capabilities []string
	selectedFolder string
	uidValidity  uint32
	highestModSeq uint64
}

// IMAPConfig represents IMAP server configuration
type IMAPConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption models.EncryptionType
	ProxyConfig *models.ProxySettings
}

// FolderInfo represents IMAP folder information
type FolderInfo struct {
	Name        string
	Delimiter   string
	Attributes  []string
	Messages    int
	Recent      int
	Unseen      int
	UIDNext     uint32
	UIDValidity uint32
	HighestModSeq uint64
}

// MessageEnvelope represents IMAP message envelope
type MessageEnvelope struct {
	UID         uint32
	Flags       []string
	InternalDate time.Time
	From        []models.Contact
	To          []models.Contact
	Subject     string
	Date        time.Time
	MessageID   string
	Size        int64
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
		}
	}
	
	if currentEnvelope != nil {
		envelopes = append(envelopes, *currentEnvelope)
	}
	
	return envelopes, nil
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
