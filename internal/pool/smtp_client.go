package pool

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"webmail_engine/internal/models"
)

// SMTPClient represents an SMTP client connection
type SMTPClient struct {
	conn       *Connection
	mu         sync.Mutex
	seqNum     uint32
	extensions map[string]string
	authTypes  []string
	maxSize    int
}

// SMTPConfig represents SMTP server configuration
type SMTPConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption models.EncryptionType
	ProxyConfig *models.ProxySettings
}

// EmailMessage represents an email to send
type EmailMessage struct {
	From        models.Contact
	To          []models.Contact
	Cc          []models.Contact
	Bcc         []models.Contact
	ReplyTo     []models.Contact
	Subject     string
	TextBody    string
	HTMLBody    string
	Headers     map[string]string
	Attachments []AttachmentData
}

// AttachmentData represents attachment content
type AttachmentData struct {
	Filename    string
	ContentType string
	Data        []byte
	Disposition string // inline, attachment
}

// SendResult represents the result of sending an email
type SendResult struct {
	MessageID   string
	Status      string
	Response    string
	SentAt      time.Time
	RetryCount  int
}

// NewSMTPClient creates a new SMTP client
func NewSMTPClient(conn *Connection) *SMTPClient {
	return &SMTPClient{
		conn: conn,
	}
}

// ConnectSMTP establishes an SMTP connection
func ConnectSMTP(ctx context.Context, config SMTPConfig) (*SMTPClient, error) {
	host := fmt.Sprintf("%s:%d", config.Host, config.Port)
	
	var dialer net.Dialer
	dialer.Timeout = 30 * time.Second
	
	netConn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SMTP server: %w", err)
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
		ID:         fmt.Sprintf("smtp-%d", time.Now().UnixNano()),
		AccountID:  config.Username,
		ConnType:   models.ProtocolSMTP,
		NetConn:    netConn,
		TLSConn:    tlsConn,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		InUse:      true,
	}
	
	client := &SMTPClient{
		conn: conn,
	}
	
	// Read greeting
	if err := client.readGreeting(); err != nil {
		conn.Close()
		return nil, err
	}
	
	// EHLO and get extensions
	if err := client.ehlo(); err != nil {
		conn.Close()
		return nil, err
	}
	
	// Start TLS if using STARTTLS
	if config.Encryption == models.EncryptionStartTLS {
		if err := client.startTLS(config.Host); err != nil {
			conn.Close()
			return nil, err
		}
	}
	
	// Authenticate if credentials provided
	if config.Username != "" && config.Password != "" {
		if err := client.authenticate(config.Username, config.Password); err != nil {
			conn.Close()
			return nil, err
		}
	}
	
	return client, nil
}

// Close closes the SMTP connection
func (c *SMTPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.conn != nil {
		c.sendCommand("QUIT")
		c.conn.NetConn.Close()
		return nil
	}
	return nil
}

// Send sends an email message
func (c *SMTPClient) Send(msg EmailMessage) (*SendResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Build message
	messageData, err := c.buildMessage(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to build message: %w", err)
	}
	
	// MAIL FROM
	fromCmd := fmt.Sprintf("MAIL FROM:<%s>", msg.From.Address)
	if _, err := c.sendCommand(fromCmd); err != nil {
		return nil, fmt.Errorf("MAIL FROM failed: %w", err)
	}
	
	// RCPT TO
	allRecipients := append(append(append([]models.Contact{}, msg.To...), msg.Cc...), msg.Bcc...)
	for _, rcpt := range allRecipients {
		rcptCmd := fmt.Sprintf("RCPT TO:<%s>", rcpt.Address)
		if _, err := c.sendCommand(rcptCmd); err != nil {
			return nil, fmt.Errorf("RCPT TO failed for %s: %w", rcpt.Address, err)
		}
	}
	
	// DATA
	if _, err := c.sendCommand("DATA"); err != nil {
		return nil, fmt.Errorf("DATA command failed: %w", err)
	}
	
	// Send message data
	writer := c.getWriter()
	_, err = writer.Write(messageData)
	if err != nil {
		return nil, fmt.Errorf("failed to write message data: %w", err)
	}
	
	// End with CRLF.CRLF
	_, err = writer.Write([]byte("\r\n.\r\n"))
	if err != nil {
		return nil, fmt.Errorf("failed to end message data: %w", err)
	}
	
	// Read response
	response, err := c.readResponse()
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	// Check for success (250 or 251)
	code := response.Code
	if code != 250 && code != 251 {
		return nil, fmt.Errorf("server rejected message: %d %s", code, response.Msg)
	}
	
	// Extract message ID from response if available
	messageID := extractMessageID(response.Msg)
	if messageID == "" {
		messageID = fmt.Sprintf("local-%d", time.Now().UnixNano())
	}
	
	return &SendResult{
		MessageID: messageID,
		Status:    "sent",
		Response:  response.Msg,
		SentAt:    time.Now(),
	}, nil
}

// Noop sends a NOOP command to keep connection alive
func (c *SMTPClient) Noop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	_, err := c.sendCommand("NOOP")
	return err
}

// Reset sends a RSET command to reset the connection state
func (c *SMTPClient) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	_, err := c.sendCommand("RSET")
	return err
}

// readGreeting reads the initial server greeting
func (c *SMTPClient) readGreeting() error {
	response, err := c.readResponse()
	if err != nil {
		return fmt.Errorf("failed to read greeting: %w", err)
	}
	
	// Greeting should be 220
	if response.Code != 220 {
		return fmt.Errorf("unexpected greeting: %d %s", response.Code, response.Msg)
	}
	
	return nil
}

// ehlo sends EHLO and collects extensions
func (c *SMTPClient) ehlo() error {
	response, err := c.sendCommand("EHLO localhost")
	if err != nil {
		return err
	}
	
	c.extensions = make(map[string]string)
	c.authTypes = []string{}
	
	// Parse extensions from multi-line response
	lines := strings.Split(response.Msg, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, " ", 2)
		
		if len(parts) > 0 {
			ext := parts[0]
			params := ""
			if len(parts) > 1 {
				params = parts[1]
			}
			
			c.extensions[ext] = params
			
			// Parse AUTH mechanisms
			if ext == "AUTH" {
				c.authTypes = strings.Fields(params)
			}
			
			// Parse SIZE limit
			if ext == "SIZE" {
				fmt.Sscanf(params, "%d", &c.maxSize)
			}
		}
	}
	
	return nil
}

// startTLS initiates TLS using STARTTLS
func (c *SMTPClient) startTLS(host string) error {
	if _, err := c.sendCommand("STARTTLS"); err != nil {
		return fmt.Errorf("STARTTLS not supported: %w", err)
	}
	
	// Upgrade to TLS
	tlsConfig := &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}
	
	tlsConn := tls.Client(c.conn.NetConn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}
	
	c.conn.TLSConn = tlsConn
	
	// Re-issue EHLO over TLS
	return c.ehlo()
}

// authenticate performs SMTP authentication
func (c *SMTPClient) authenticate(username, password string) error {
	// Check for PLAIN auth support
	hasPlain := false
	for _, auth := range c.authTypes {
		if auth == "PLAIN" {
			hasPlain = true
			break
		}
	}
	
	if !hasPlain {
		return fmt.Errorf("no supported authentication methods")
	}
	
	// Send AUTH PLAIN
	authData := fmt.Sprintf("\000%s\000%s", username, password)
	encoded := base64.StdEncoding.EncodeToString([]byte(authData))
	
	response, err := c.sendCommand("AUTH PLAIN " + encoded)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	
	// Should be 235 Authentication successful
	if response.Code != 235 {
		return fmt.Errorf("authentication failed: %d %s", response.Code, response.Msg)
	}
	
	return nil
}

// buildMessage builds the raw email message
func (c *SMTPClient) buildMessage(msg EmailMessage) ([]byte, error) {
	var builder strings.Builder
	
	// Headers
	builder.WriteString(fmt.Sprintf("From: %s <%s>\r\n", msg.From.Name, msg.From.Address))
	
	// To header
	toAddresses := make([]string, len(msg.To))
	for i, to := range msg.To {
		toAddresses[i] = fmt.Sprintf("%s <%s>", to.Name, to.Address)
	}
	builder.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(toAddresses, ", ")))
	
	// Cc header
	if len(msg.Cc) > 0 {
		ccAddresses := make([]string, len(msg.Cc))
		for i, cc := range msg.Cc {
			ccAddresses[i] = fmt.Sprintf("%s <%s>", cc.Name, cc.Address)
		}
		builder.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(ccAddresses, ", ")))
	}
	
	// Reply-To header
	if len(msg.ReplyTo) > 0 {
		replyToAddresses := make([]string, len(msg.ReplyTo))
		for i, rt := range msg.ReplyTo {
			replyToAddresses[i] = fmt.Sprintf("%s <%s>", rt.Name, rt.Address)
		}
		builder.WriteString(fmt.Sprintf("Reply-To: %s\r\n", strings.Join(replyToAddresses, ", ")))
	}
	
	// Subject header
	builder.WriteString(fmt.Sprintf("Subject: %s\r\n", msg.Subject))
	
	// Date header
	builder.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	
	// Message-ID header
	messageID := fmt.Sprintf("<%d@localhost>", time.Now().UnixNano())
	builder.WriteString(fmt.Sprintf("Message-ID: %s\r\n", messageID))
	
	// Custom headers
	for key, value := range msg.Headers {
		builder.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}
	
	// MIME headers for multipart message
	if len(msg.Attachments) > 0 || (msg.TextBody != "" && msg.HTMLBody != "") {
		boundary := fmt.Sprintf("----=_Part_%d", time.Now().UnixNano())
		builder.WriteString("MIME-Version: 1.0\r\n")
		builder.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
		builder.WriteString("\r\n")
		
		// Body part
		builder.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		
		if msg.TextBody != "" && msg.HTMLBody != "" {
			// Nested multipart for text and HTML
			innerBoundary := fmt.Sprintf("----=_InnerPart_%d", time.Now().UnixNano())
			builder.WriteString("Content-Type: multipart/alternative; boundary=\"")
			builder.WriteString(innerBoundary)
			builder.WriteString("\"\r\n\r\n")
			
			// Text part
			builder.WriteString(fmt.Sprintf("--%s\r\n", innerBoundary))
			builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
			builder.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
			builder.WriteString(msg.TextBody)
			builder.WriteString("\r\n")
			
			// HTML part
			builder.WriteString(fmt.Sprintf("--%s\r\n", innerBoundary))
			builder.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
			builder.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
			builder.WriteString(msg.HTMLBody)
			builder.WriteString("\r\n")
			
			builder.WriteString(fmt.Sprintf("--%s--\r\n", innerBoundary))
		} else if msg.TextBody != "" {
			builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
			builder.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
			builder.WriteString(msg.TextBody)
			builder.WriteString("\r\n")
		} else if msg.HTMLBody != "" {
			builder.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
			builder.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
			builder.WriteString(msg.HTMLBody)
			builder.WriteString("\r\n")
		}
		
		// Attachments
		for _, att := range msg.Attachments {
			builder.WriteString(fmt.Sprintf("--%s\r\n", boundary))
			builder.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", att.ContentType, att.Filename))
			builder.WriteString("Content-Transfer-Encoding: base64\r\n")
			builder.WriteString(fmt.Sprintf("Content-Disposition: %s; filename=\"%s\"\r\n", att.Disposition, att.Filename))
			builder.WriteString("\r\n")
			
			// Base64 encode attachment
			encoded := base64.StdEncoding.EncodeToString(att.Data)
			// Split into lines
			for i := 0; i < len(encoded); i += 76 {
				end := i + 76
				if end > len(encoded) {
					end = len(encoded)
				}
				builder.WriteString(encoded[i:end])
				builder.WriteString("\r\n")
			}
		}
		
		builder.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		// Simple text message
		builder.WriteString("MIME-Version: 1.0\r\n")
		builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		builder.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
		
		if msg.TextBody != "" {
			builder.WriteString(msg.TextBody)
		} else if msg.HTMLBody != "" {
			builder.WriteString(msg.HTMLBody)
		}
		
		builder.WriteString("\r\n")
	}
	
	return []byte(builder.String()), nil
}

// sendCommand sends a command and reads response
func (c *SMTPClient) sendCommand(command string) (*SMTPResponse, error) {
	c.seqNum++
	
	writer := c.getWriter()
	_, err := fmt.Fprintf(writer, "%s\r\n", command)
	if err != nil {
		return nil, err
	}
	
	return c.readResponse()
}

// SMTPResponse represents an SMTP server response
type SMTPResponse struct {
	Code int
	Msg  string
}

// readResponse reads a server response
func (c *SMTPClient) readResponse() (*SMTPResponse, error) {
	reader := c.getReader()
	tp := textproto.NewReader(bufio.NewReader(reader))
	code, msg, err := tp.ReadResponse(250)
	if err != nil {
		return nil, err
	}
	return &SMTPResponse{
		Code: code,
		Msg:  strings.TrimSpace(msg),
	}, nil
}

// getReader returns the appropriate reader
func (c *SMTPClient) getReader() io.Reader {
	if c.conn.TLSConn != nil {
		return c.conn.TLSConn
	}
	return c.conn.NetConn
}

// getWriter returns the appropriate writer
func (c *SMTPClient) getWriter() io.Writer {
	if c.conn.TLSConn != nil {
		return c.conn.TLSConn
	}
	return c.conn.NetConn
}

// extractMessageID extracts message ID from server response
func extractMessageID(response string) string {
	// Look for message ID in response
	// Common formats: "<message-id>", "Message-ID: <message-id>"
	start := strings.Index(response, "<")
	end := strings.LastIndex(response, ">")
	
	if start != -1 && end != -1 && end > start {
		return response[start+1 : end]
	}
	
	return ""
}

// HasAuth checks if a specific auth type is supported
func (c *SMTPClient) HasAuth(authType string) bool {
	for _, auth := range c.authTypes {
		if auth == authType {
			return true
		}
	}
	return false
}

// MaxSize returns the maximum message size
func (c *SMTPClient) MaxSize() int {
	return c.maxSize
}

// Extensions returns the list of supported extensions
func (c *SMTPClient) Extensions() map[string]string {
	return c.extensions
}
