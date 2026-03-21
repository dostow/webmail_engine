package mimeparser

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jhillyerd/enmime"
	"webmail_engine/internal/models"
)

// MIMEParser handles parsing of MIME email messages
type MIMEParser struct {
	maxInlineSize int64
	tempStorage   string
}

// ParseResult contains the parsed message data
type ParseResult struct {
	Message     *models.Message
	Attachments []ParsedAttachment
	Links       []string
	Contacts    []models.Contact
}

// ParsedAttachment represents a parsed attachment
type ParsedAttachment struct {
	ID          string
	PartID      string
	Filename    string
	ContentType string
	Data        []byte
	Size        int64
	Disposition string
	ContentID   string
	Checksum    string
}

// ParseConfig holds parsing configuration
type ParseConfig struct {
	IncludeHeaders     bool
	IncludeBody        bool
	IncludeAttachments bool
	MaxInlineSize      int64
	Format             string // minimal, standard, verbose
	ExtractLinks       bool
	ExtractContacts    bool
	MaskSensitiveData  bool
}

// DefaultParseConfig returns default parsing configuration
func DefaultParseConfig() ParseConfig {
	return ParseConfig{
		IncludeHeaders:     true,
		IncludeBody:        true,
		IncludeAttachments: true,
		MaxInlineSize:      1024 * 1024, // 1MB
		Format:             "standard",
		ExtractLinks:       true,
		ExtractContacts:    true,
		MaskSensitiveData:  true,
	}
}

// NewMIMEParser creates a new MIME parser
func NewMIMEParser(tempStorage string) *MIMEParser {
	return &MIMEParser{
		maxInlineSize: 1024 * 1024, // 1MB default
		tempStorage:   tempStorage,
	}
}

// Parse parses raw MIME data into structured format
func (p *MIMEParser) Parse(rawData []byte, config ParseConfig) (*ParseResult, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(rawData))
	if err != nil {
		return nil, fmt.Errorf("failed to read envelope: %w", err)
	}

	date, _ := env.Date()
	if date.IsZero() {
		// Fallback to manual date parsing from headers
		if dateStr := env.GetHeader("Date"); dateStr != "" {
			if d, err := mail.ParseDate(dateStr); err == nil {
				date = d
			}
		}
	}

	result := &ParseResult{
		Message: &models.Message{
			MessageID:   env.GetHeader("Message-Id"),
			Subject:     env.GetHeader("Subject"),
			Date:        date,
			Headers:     p.parseHeaders(env),
			InReplyTo:   env.GetHeader("In-Reply-To"),
			References:  env.GetHeaderValues("References"),
			ContentType: models.ContentType(env.Root.ContentType),
			Size:        int64(len(rawData)),
			ThreadID:    p.extractThreadID(env),
		},
		Attachments: []ParsedAttachment{},
		Links:       []string{},
		Contacts:    []models.Contact{},
	}

	// Parse body
	if config.IncludeBody {
		text := env.Text
		html := env.HTML

		// Heuristic: if text starts with headers, it might be a parsing failure
		if strings.HasPrefix(strings.TrimSpace(text), "Return-Path:") || 
		   strings.HasPrefix(strings.TrimSpace(text), "Delivered-To:") {
			// Try to find the first double newline to separate headers from body
			if idx := strings.Index(text, "\r\n\r\n"); idx != -1 {
				text = text[idx+4:]
			} else if idx := strings.Index(text, "\n\n"); idx != -1 {
				text = text[idx+2:]
			}
		}

		// If text is still empty but HTML exists, provide a notice
		if text == "" && html != "" {
			text = "(HTML-only message)"
		}

		result.Message.Body = &models.MessageBody{
			Text:      text,
			HTML:      html,
			PlainText: text,
		}
	}

	// Map addresses
	from, _ := env.AddressList("From")
	result.Message.From = p.toContact(from)
	to, _ := env.AddressList("To")
	result.Message.To = p.toContacts(to)
	cc, _ := env.AddressList("Cc")
	result.Message.Cc = p.toContacts(cc)
	bcc, _ := env.AddressList("Bcc")
	result.Message.Bcc = p.toContacts(bcc)
	replyTo, _ := env.AddressList("Reply-To")
	result.Message.ReplyTo = p.toContacts(replyTo)

	// Extract contacts from all headers if requested
	if config.ExtractContacts {
		result.Contacts = p.extractContacts(env)
	}

	// Handle attachments and inlines
	if config.IncludeAttachments {
		for _, att := range env.Attachments {
			p.addAttachment(att, "attachment", result, config)
		}
		for _, inline := range env.Inlines {
			p.addAttachment(inline, "inline", result, config)
		}
	}

	// Thread ID
	result.Message.ThreadID = p.extractThreadID(env)

	// Extract links from body
	if config.ExtractLinks && result.Message.Body != nil {
		content := result.Message.Body.Text
		if content == "" {
			content = result.Message.Body.HTML
		}
		result.Links = p.extractLinks(content)
	}

	// Store raw headers if verbose
	if config.Format == "verbose" {
		var rawHeaders strings.Builder
		for _, key := range env.GetHeaderKeys() {
			for _, value := range env.GetHeaderValues(key) {
				rawHeaders.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
			}
		}
		result.Message.RawHeaders = rawHeaders.String()
	}

	return result, nil
}

// toContact converts first mail.Address to models.Contact
func (p *MIMEParser) toContact(addrs []*mail.Address) models.Contact {
	if len(addrs) == 0 {
		return models.Contact{}
	}
	return models.Contact{
		Name:    addrs[0].Name,
		Address: addrs[0].Address,
	}
}

// toContacts converts []*mail.Address to []models.Contact
func (p *MIMEParser) toContacts(addrs []*mail.Address) []models.Contact {
	contacts := make([]models.Contact, 0, len(addrs))
	for _, addr := range addrs {
		contacts = append(contacts, models.Contact{
			Name:    addr.Name,
			Address: addr.Address,
		})
	}
	return contacts
}

// addAttachment adds an enmime.Part as an attachment to ParseResult
func (p *MIMEParser) addAttachment(part *enmime.Part, disposition string, result *ParseResult, config ParseConfig) {
	attachment := ParsedAttachment{
		ID:          p.generateAttachmentID(),
		PartID:      part.PartID,
		Filename:    part.FileName,
		ContentType: part.ContentType,
		Data:        part.Content,
		Size:        int64(len(part.Content)),
		Disposition: disposition,
		ContentID:   part.ContentID,
		Checksum:    p.calculateChecksum(part.Content),
	}
	
	result.Attachments = append(result.Attachments, attachment)

	// Add to message if size limit allows
	if int64(len(part.Content)) <= config.MaxInlineSize {
		if result.Message.Attachments == nil {
			result.Message.Attachments = []models.Attachment{}
		}
		result.Message.Attachments = append(result.Message.Attachments, models.Attachment{
			ID:          attachment.ID,
			PartID:      attachment.PartID,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			Size:        attachment.Size,
			Disposition: attachment.Disposition,
			ContentID:   attachment.ContentID,
			Checksum:    attachment.Checksum,
		})
	}
}

// parseHeaders extracts headers from enmime.Envelope to map[string]string
func (p *MIMEParser) parseHeaders(env *enmime.Envelope) map[string]string {
	result := make(map[string]string)
	for _, key := range env.GetHeaderKeys() {
		values := env.GetHeaderValues(key)
		if len(values) > 0 {
			result[key] = strings.Join(values, ", ")
		}
	}
	return result
}

// extractLinks extracts URLs from text content
func (p *MIMEParser) extractLinks(content string) []string {
	urlRegex := regexp.MustCompile(`https?://[^\s<>"{}|^` + "`" + `[\]]+`)
	matches := urlRegex.FindAllString(content, -1)
	
	seen := make(map[string]bool)
	var links []string
	
	for _, match := range matches {
		match = strings.TrimRight(match, ".,;:!?")
		if !seen[match] {
			seen[match] = true
			links = append(links, match)
		}
	}
	
	return links
}

// extractContacts extracts contacts from headers
func (p *MIMEParser) extractContacts(env *enmime.Envelope) []models.Contact {
	var contacts []models.Contact
	headers := []string{"From", "To", "Cc", "Reply-To", "Sender", "Resent-From", "Resent-To"}
	
	for _, h := range headers {
		addrs, err := env.AddressList(h)
		if err == nil {
			for _, addr := range addrs {
				contacts = append(contacts, models.Contact{
					Name:    addr.Name,
					Address: addr.Address,
				})
			}
		}
	}
	
	return contacts
}

// extractThreadID extracts or generates a thread ID
func (p *MIMEParser) extractThreadID(env *enmime.Envelope) string {
	if inReplyTo := env.GetHeader("In-Reply-To"); inReplyTo != "" {
		return inReplyTo
	}
	
	if references := env.GetHeader("References"); references != "" {
		parts := strings.Fields(references)
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	
	if messageID := env.GetHeader("Message-ID"); messageID != "" {
		return messageID
	}
	
	return fmt.Sprintf("thread-%d", time.Now().UnixNano())
}

// generateAttachmentID generates a unique attachment ID
func (p *MIMEParser) generateAttachmentID() string {
	return fmt.Sprintf("att-%d", time.Now().UnixNano())
}

// calculateChecksum calculates SHA256 checksum
func (p *MIMEParser) calculateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)
}

// StreamParse parses MIME data in streaming fashion using enmime
func (p *MIMEParser) StreamParse(reader io.Reader, config ParseConfig, handler func(part *ParseResult) error) error {
	env, err := enmime.ReadEnvelope(reader)
	if err != nil {
		return err
	}
	
	date, _ := env.Date()
	// Send header info first
	result := &ParseResult{
		Message: &models.Message{
			Headers: p.parseHeaders(env),
			Subject: env.GetHeader("Subject"),
			Date:    date,
		},
	}
	if err := handler(result); err != nil {
		return err
	}
	
	// Process body
	if config.IncludeBody {
		bodyResult := &ParseResult{
			Message: &models.Message{
				Body: &models.MessageBody{
					Text: env.Text,
					HTML: env.HTML,
				},
			},
		}
		if err := handler(bodyResult); err != nil {
			return err
		}
	}
	
	// Process attachments
	if config.IncludeAttachments {
		for _, att := range env.Attachments {
			attResult := &ParseResult{Message: &models.Message{}}
			p.addAttachment(att, "attachment", attResult, config)
			if err := handler(attResult); err != nil {
				return err
			}
		}
		for _, inline := range env.Inlines {
			inlineResult := &ParseResult{Message: &models.Message{}}
			p.addAttachment(inline, "inline", inlineResult, config)
			if err := handler(inlineResult); err != nil {
				return err
			}
		}
	}
	
	return nil
}

// GenerateSignedURL generates a signed URL for attachment access
func GenerateSignedURL(attachmentID, baseURL, secret string, expiry time.Duration) (string, time.Time) {
	expiryTime := time.Now().Add(expiry)
	signature := generateSignature(attachmentID, expiryTime.Unix(), secret)
	
	params := url.Values{}
	params.Set("sig", signature)
	params.Set("exp", fmt.Sprintf("%d", expiryTime.Unix()))
	
	u, _ := url.Parse(baseURL)
	u.Path = fmt.Sprintf("/attachments/%s", attachmentID)
	u.RawQuery = params.Encode()
	
	return u.String(), expiryTime
}

func generateSignature(attachmentID string, expiry int64, secret string) string {
	data := fmt.Sprintf("%s:%d:%s", attachmentID, expiry, secret)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash)[:32]
}

// ValidateSignature validates a signed URL signature
func ValidateSignature(attachmentID string, expiry int64, signature, secret string) bool {
	if time.Now().Unix() > expiry {
		return false
	}
	expectedSig := generateSignature(attachmentID, expiry, secret)
	return signature == expectedSig
}
