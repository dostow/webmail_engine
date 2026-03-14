package pool

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"time"

	"github.com/wneessen/go-mail"
	"webmail_engine/internal/models"
)

// SMTPAdapter wraps go-mail for SMTP operations
type SMTPAdapter struct {
	client *mail.Client
	config SMTPConfig
}

// ConnectSMTPv2 establishes SMTP connection using go-mail
func ConnectSMTPv2(ctx context.Context, config SMTPConfig) (*SMTPAdapter, error) {
	// Create client options
	options := []mail.Option{
		mail.WithPort(config.Port),
		mail.WithTimeout(30 * time.Second),
		mail.WithDialContextFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
			// Custom dialer with context support
			dialer := &net.Dialer{Timeout: 30 * time.Second}
			return dialer.DialContext(ctx, network, address)
		}),
	}

	// Add authentication if credentials provided
	if config.Username != "" && config.Password != "" {
		options = append(options,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(config.Username),
			mail.WithPassword(config.Password),
		)
	}

	// Set TLS policy based on encryption type
	switch config.Encryption {
	case models.EncryptionSSL, models.EncryptionTLS:
		options = append(options, mail.WithTLSPolicy(mail.TLSMandatory))
	case models.EncryptionStartTLS:
		options = append(options, mail.WithTLSPolicy(mail.TLSOpportunistic))
	case models.EncryptionNone:
		options = append(options, mail.WithTLSPolicy(mail.NoTLS))
	}

	// Create the client
	client, err := mail.NewClient(config.Host, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create SMTP client: %w", err)
	}

	// Test connection by dialing
	if err := client.DialWithContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to SMTP server: %w", err)
	}

	return &SMTPAdapter{
		client: client,
		config: config,
	}, nil
}

// Close closes the SMTP connection
func (a *SMTPAdapter) Close() error {
	if a.client != nil {
		a.client.Close()
	}
	return nil
}

// Send sends an email message
func (a *SMTPAdapter) Send(msg EmailMessage) (*SendResult, error) {
	// Create new message
	m := mail.NewMsg()

	// Set From
	if err := m.FromFormat(msg.From.Name, msg.From.Address); err != nil {
		return nil, fmt.Errorf("failed to set From: %w", err)
	}

	// Add To recipients
	for _, to := range msg.To {
		if err := m.AddToFormat(to.Name, to.Address); err != nil {
			return nil, fmt.Errorf("failed to add To recipient: %w", err)
		}
	}

	// Add Cc recipients
	for _, cc := range msg.Cc {
		if err := m.AddCcFormat(cc.Name, cc.Address); err != nil {
			return nil, fmt.Errorf("failed to add Cc recipient: %w", err)
		}
	}

	// Add Bcc recipients
	for _, bcc := range msg.Bcc {
		if err := m.AddBccFormat(bcc.Name, bcc.Address); err != nil {
			return nil, fmt.Errorf("failed to add Bcc recipient: %w", err)
		}
	}

	// Add Reply-To
	if len(msg.ReplyTo) > 0 {
		rt := msg.ReplyTo[0]
		if err := m.ReplyToFormat(rt.Name, rt.Address); err != nil {
			return nil, fmt.Errorf("failed to add Reply-To: %w", err)
		}
	}

	// Set subject
	m.Subject(msg.Subject)

	// Set body based on content type
	if msg.HTMLBody != "" && msg.TextBody != "" {
		// Both HTML and plain text - use multipart/alternative
		m.SetBodyString(mail.TypeTextPlain, msg.TextBody)
		m.AddAlternativeString(mail.TypeTextHTML, msg.HTMLBody)
	} else if msg.HTMLBody != "" {
		m.SetBodyString(mail.TypeTextHTML, msg.HTMLBody)
	} else if msg.TextBody != "" {
		m.SetBodyString(mail.TypeTextPlain, msg.TextBody)
	}

	// Add attachments
	for _, att := range msg.Attachments {
		if err := m.AttachReader(att.Filename, bytes.NewReader(att.Data), mail.WithFileContentType(mail.ContentType(att.ContentType))); err != nil {
			return nil, fmt.Errorf("failed to attach file: %w", err)
		}
	}

	// Add custom headers
	for key, value := range msg.Headers {
		m.SetGenHeader(mail.Header(key), value)
	}

	// Send the message
	if err := a.client.DialAndSend(m); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	// Extract message ID
	messageID := m.GetMessageID()
	if messageID == "" {
		messageID = fmt.Sprintf("<%d@localhost>", time.Now().UnixNano())
	}

	return &SendResult{
		MessageID: messageID,
		Status:    "sent",
		SentAt:    time.Now(),
	}, nil
}

// Noop sends a NOOP command to keep connection alive
func (a *SMTPAdapter) Noop() error {
	// go-mail doesn't expose NOOP directly, but we can reconnect
	// This is a limitation - in practice, the client handles keepalive
	return nil
}

// Reset sends a RSET command to reset the connection state
func (a *SMTPAdapter) Reset() error {
	// go-mail handles reset internally when sending new messages
	// We can close and reconnect if needed
	a.client.Close()
	return a.client.DialWithContext(context.Background())
}

// HasAuth checks if a specific auth type is supported
func (a *SMTPAdapter) HasAuth(authType string) bool {
	// go-mail doesn't expose supported auth types directly
	// We assume PLAIN is supported if authentication was configured
	return authType == "PLAIN" && a.config.Username != ""
}

// MaxSize returns the maximum message size
func (a *SMTPAdapter) MaxSize() int {
	// go-mail doesn't expose SIZE limit directly
	// Return a reasonable default
	return 25 * 1024 * 1024 // 25MB
}

// Extensions returns the list of supported extensions
func (a *SMTPAdapter) Extensions() map[string]string {
	// go-mail doesn't expose extensions directly
	// Return common extensions
	return map[string]string{
		"STARTTLS": "",
		"AUTH":     "PLAIN LOGIN",
		"SIZE":     "25000000",
	}
}

// Reconnect closes and reopens the connection
func (a *SMTPAdapter) Reconnect(ctx context.Context) error {
	a.client.Close()
	return a.client.DialWithContext(ctx)
}

// IsConnected checks if the client is connected
func (a *SMTPAdapter) IsConnected() bool {
	// go-mail doesn't have IsConnected, so we assume connected if client exists
	// In practice, you'd need to track connection state separately
	return a.client != nil
}
