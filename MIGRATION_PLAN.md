# Migration Plan: go-mail and go-imap/v2

## Executive Summary

This document outlines the plan to migrate the webmail_engine from custom IMAP/SMTP implementations to the standardized libraries:
- **github.com/emersion/go-imap/v2** - For IMAP operations
- **github.com/go-gomail/gomail** - For SMTP operations (Note: gomail is unmaintained, alternative recommended)

**Important Note**: `go-gomail/gomail` is no longer maintained. We recommend using:
- **github.com/wneessen/go-mail** (actively maintained fork with modern features) OR
- **github.com/emersion/go-smtp** (from the same author as go-imap)

This plan will use **go-mail** (wneessen) for SMTP as it's production-ready and well-maintained.

---

## Current State Analysis

### Files Using Custom IMAP Implementation
1. `internal/pool/imap_client.go` - Core IMAP client (~550 lines)
2. `internal/pool/connection_pool.go` - Connection management
3. `internal/service/message_service.go` - IMAP operations integration
4. `internal/service/sync_manager.go` - Background sync using IMAP

### Files Using Custom SMTP Implementation
1. `internal/pool/smtp_client.go` - Core SMTP client (~650 lines)
2. `internal/service/send_service.go` - Send service integration

### Supporting Files
1. `internal/mimeparser/mime_parser.go` - MIME parsing (can be simplified)
2. `internal/models/types.go` - Data models (will need minor updates)

---

## Migration Phases

### Phase 1: Preparation (Week 1)

#### 1.1 Update Dependencies
```bash
go get github.com/emersion/go-imap/v2@latest
go get github.com/emersion/go-imap/v2/imapclient@latest
go get github.com/wneessen/go-mail@latest
go mod tidy
```

#### 1.2 Create Adapter Interfaces
Create wrapper interfaces to minimize changes to service layer:
- `internal/pool/imap_adapter.go` - IMAP abstraction layer
- `internal/pool/smtp_adapter.go` - SMTP abstraction layer

#### 1.3 Update go.mod
```go
require (
    github.com/emersion/go-imap/v2 v2.0.0
    github.com/emersion/go-imap/v2/imapclient v2.0.0
    github.com/wneessen/go-mail v0.5.0
    // ... existing dependencies
)
```

---

### Phase 2: IMAP Migration (Week 2-3)

#### 2.1 Create IMAP Adapter Layer

**File: `internal/pool/imap_adapter.go`**
```go
package pool

import (
    "context"
    "time"
    
    "github.com/emersion/go-imap/v2"
    "github.com/emersion/go-imap/v2/imapclient"
)

// IMAPAdapter wraps go-imap/v2 to match our interface
type IMAPAdapter struct {
    client *imapclient.Client
    conn   *Connection
}

// ConnectIMAPv2 establishes connection using go-imap/v2
func ConnectIMAPv2(ctx context.Context, config IMAPConfig) (*IMAPAdapter, error) {
    // Implementation using go-imap/v2
}

// ListFolders lists all folders
func (a *IMAPAdapter) ListFolders() ([]FolderInfo, error) {
    // Use go-imap/v2 LIST command
}

// SelectFolder selects a folder
func (a *IMAPAdapter) SelectFolder(folder string) (*FolderInfo, error) {
    // Use go-imap/v2 SELECT command
}

// FetchMessages fetches message envelopes
func (a *IMAPAdapter) FetchMessages(uids []uint32, includeBody bool) ([]MessageEnvelope, error) {
    // Use go-imap/v2 FETCH with ENVELOPE
}

// FetchMessageRaw fetches raw message content
func (a *IMAPAdapter) FetchMessageRaw(uid uint32) ([]byte, error) {
    // Use go-imap/v2 FETCH with RFC822
}

// Search performs IMAP search
func (a *IMAPAdapter) Search(criteria string) ([]uint32, error) {
    // Use go-imap/v2 UID SEARCH
}

// Idle starts IMAP IDLE mode
func (a *IMAPAdapter) Idle(ctx context.Context, handler func(event string, data []byte)) error {
    // Use go-imap/v2 IDLE extension
}
```

#### 2.2 Key Migration Points for IMAP

| Current Implementation | go-imap/v2 Equivalent |
|------------------------|----------------------|
| Custom `ConnectIMAP()` | `imapclient.DialTLS()` or `imapclient.DialStartTLS()` |
| Manual `LIST` parsing | `client.List()` with `ListOptions` |
| Manual `SELECT` parsing | `client.Select()` returns `SelectData` |
| Custom `FETCH` parser | `client.Fetch()` with `FetchOptions` |
| Manual `UID SEARCH` | `client.UidSearch()` |
| Custom IDLE loop | `client.Idle()` with callback |
| Manual TLS handling | Built-in TLS support |

#### 2.3 Update Message Service

**File: `internal/service/message_service.go`**

Changes needed:
```go
// Old
client, err := pool.ConnectIMAP(imapCtx, imapConfig)

// New
client, err := pool.ConnectIMAPv2(imapCtx, imapConfig)

// Old - manual envelope parsing
envelopes, err := client.FetchMessages(uids, false)

// New - using go-imap/v2 FetchOptions
fetchOptions := &imap.FetchOptions{
    Envelope: true,
    Flags:    true,
    InternalDate: true,
    RFC822Size: true,
}
messages, err := client.Fetch(uids, fetchOptions)
```

#### 2.4 Update Sync Manager

**File: `internal/service/sync_manager.go`**

Similar changes to message_service.go for IMAP operations.

---

### Phase 3: SMTP Migration (Week 3-4)

#### 3.1 Create SMTP Adapter Layer

**File: `internal/pool/smtp_adapter.go`**
```go
package pool

import (
    "context"
    
    "github.com/wneessen/go-mail"
)

// SMTPAdapter wraps go-mail for SMTP operations
type SMTPAdapter struct {
    client *mail.Client
    config SMTPConfig
}

// ConnectSMTPv2 establishes SMTP connection using go-mail
func ConnectSMTPv2(ctx context.Context, config SMTPConfig) (*SMTPAdapter, error) {
    // Create client with options
    client, err := mail.NewClient(
        config.Host,
        mail.WithPort(config.Port),
        mail.WithSMTPAuth(mail.SMTPAuthPlain),
        mail.WithUsername(config.Username),
        mail.WithPassword(config.Password),
    )
    if err != nil {
        return nil, err
    }
    
    // Set TLS/SSL
    switch config.Encryption {
    case models.EncryptionSSL, models.EncryptionTLS:
        client.SetTLSPolicy(mail.TLSMandatory)
    case models.EncryptionStartTLS:
        client.SetTLSPolicy(mail.TLSOpportunistic)
    case models.EncryptionNone:
        client.SetTLSPolicy(mail.NoTLS)
    }
    
    return &SMTPAdapter{client: client, config: config}, nil
}

// Send sends an email message
func (a *SMTPAdapter) Send(msg EmailMessage) (*SendResult, error) {
    // Create message
    m := mail.NewMsg()
    m.FromFormat(msg.From.Name, msg.From.Address)
    
    // Add recipients
    for _, to := range msg.To {
        m.ToFormat(to.Name, to.Address)
    }
    for _, cc := range msg.Cc {
        m.CcFormat(cc.Name, cc.Address)
    }
    
    m.Subject(msg.Subject)
    
    // Add body
    if msg.HTMLBody != "" && msg.TextBody != "" {
        m.SetBodyString(mail.TypeTextPlain, msg.TextBody)
        m.AddAlternativeString(mail.TypeTextHTML, msg.HTMLBody)
    } else if msg.HTMLBody != "" {
        m.SetBodyString(mail.TypeTextHTML, msg.HTMLBody)
    } else {
        m.SetBodyString(mail.TypeTextPlain, msg.TextBody)
    }
    
    // Add attachments
    for _, att := range msg.Attachments {
        m.AttachReader(att.Filename, bytes.NewReader(att.Data), mail.WithFileContentType(att.ContentType))
    }
    
    // Send
    if err := a.client.DialAndSend(m); err != nil {
        return nil, err
    }
    
    return &SendResult{
        MessageID: m.GetMsgID(),
        Status:    "sent",
        SentAt:    time.Now(),
    }, nil
}
```

#### 3.2 Key Migration Points for SMTP

| Current Implementation | go-mail Equivalent |
|------------------------|-------------------|
| Custom `ConnectSMTP()` | `mail.NewClient()` with options |
| Manual `EHLO` handling | Automatic in go-mail |
| Manual `STARTTLS` | `SetTLSPolicy()` |
| Manual `AUTH PLAIN` | `WithSMTPAuth()` |
| Custom message builder | `mail.NewMsg()` with fluent API |
| Manual MIME construction | Automatic MIME handling |
| Base64 encoding | Automatic |

#### 3.3 Update Send Service

**File: `internal/service/send_service.go`**

```go
// Old
smtpClient, err := pool.ConnectSMTP(ctx, smtpConfig)
result, err := smtpClient.Send(emailMsg)

// New
smtpClient, err := pool.ConnectSMTPv2(ctx, smtpConfig)
result, err := smtpClient.Send(emailMsg)
```

---

### Phase 4: MIME Parser Simplification (Week 4)

#### 4.1 Leverage go-mail MIME Handling

The current `mimeparser/mime_parser.go` can be simplified since go-mail handles MIME parsing:

**File: `internal/mimeparser/mime_parser.go`**

```go
// Use go-mail's built-in MIME parsing
import "github.com/wneessen/go-mail"

func ParseMessage(rawData []byte) (*ParseResult, error) {
    msg := mail.NewMsg()
    if err := msg.Read(rawData); err != nil {
        return nil, err
    }
    
    // Extract parts using go-mail API
    result := &ParseResult{
        Message: &models.Message{
            Subject: msg.Subject(),
            From:    parseAddress(msg.From()),
            To:      parseAddresses(msg.To()),
            Date:    msg.Date(),
        },
    }
    
    // Get body parts
    result.Message.Body = &models.MessageBody{
        Text: msg.GetBodyString(mail.TypeTextPlain),
        HTML: msg.GetBodyString(mail.TypeTextHTML),
    }
    
    // Get attachments
    for _, att := range msg.Attachments() {
        result.Attachments = append(result.Attachments, ParsedAttachment{
            Filename:    att.Name(),
            ContentType: att.ContentType(),
            Data:        att.Content(),
            Size:        int64(len(att.Content())),
        })
    }
    
    return result, nil
}
```

---

### Phase 5: Testing & Validation (Week 5)

#### 5.1 Unit Tests

Create comprehensive tests for:
- `internal/pool/imap_adapter_test.go`
- `internal/pool/smtp_adapter_test.go`
- Updated service tests

#### 5.2 Integration Tests

Test with real email providers:
- Gmail
- Outlook/Hotmail
- Yahoo Mail
- Generic IMAP/SMTP servers

#### 5.3 Performance Benchmarks

Compare performance metrics:
- Connection establishment time
- Message fetch latency
- Memory usage
- Concurrent connection handling

---

### Phase 6: Cleanup & Documentation (Week 6)

#### 6.1 Remove Old Code

Delete files:
- `internal/pool/imap_client.go` (old implementation)
- `internal/pool/smtp_client.go` (old implementation)

#### 6.2 Update Documentation

Update:
- `README.md` - Library references
- `IMPLEMENTATION_SUMMARY.md` - Architecture changes
- API documentation if needed

#### 6.3 Code Review

- Security review of new implementations
- Performance optimization
- Error handling improvements

---

## Detailed Implementation Guide

### IMAP Migration Details

#### Connection Establishment

**Old Implementation:**
```go
netConn, err := dialer.DialContext(ctx, "tcp", host)
tlsConn := tls.Client(netConn, tlsConfig)
// Manual greeting reading
// Manual authentication
```

**New Implementation (go-imap/v2):**
```go
// For SSL/TLS
client, err := imapclient.DialTLS(ctx, host, &imapclient.Options{
    TLSConfig: tlsConfig,
})

// For STARTTLS
client, err := imapclient.DialStartTLS(ctx, host, &imapclient.Options{
    TLSConfig: tlsConfig,
})

// Authenticate
if err := client.Login(ctx, username, password); err != nil {
    return nil, err
}
```

#### Folder Listing

**Old Implementation:**
```go
response, err := c.sendCommand("LIST \"\" \"*\"")
folders := parseListResponse(response) // Manual parsing
```

**New Implementation:**
```go
var folders []FolderInfo
listChan := client.List("", "*", nil)
for list := range listChan {
    if list.Err != nil {
        return nil, list.Err
    }
    folders = append(folders, FolderInfo{
        Name:       list.Data.Mailbox,
        Delimiter:  list.Data.Delimiter,
        Attributes: list.Data.Attributes,
    })
}
```

#### Message Fetching

**Old Implementation:**
```go
command := fmt.Sprintf("UID FETCH %s (UID FLAGS INTERNALDATE ENVELOPE RFC822.SIZE)", uidSet)
response, err := c.sendCommand(command)
envelopes := parseFetchResponse(response) // Manual parsing
```

**New Implementation:**
```go
fetchOptions := &imap.FetchOptions{
    Envelope:     true,
    Flags:        true,
    InternalDate: true,
    RFC822Size:   true,
}

var envelopes []MessageEnvelope
fetchChan := client.UidFetch(uids, fetchOptions)
for fetch := range fetchChan {
    if fetch.Err != nil {
        return nil, fetch.Err
    }
    envelope := MessageEnvelope{
        UID:          fetch.Message.Uid,
        Flags:        fetch.Message.Flags,
        InternalDate: fetch.Message.InternalDate,
        Size:         int64(fetch.Message.Size),
    }
    if fetch.Message.Envelope != nil {
        envelope.Subject = fetch.Message.Envelope.Subject
        envelope.From = parseAddresses(fetch.Message.Envelope.From)
        envelope.To = parseAddresses(fetch.Message.Envelope.To)
    }
    envelopes = append(envelopes, envelope)
}
```

#### IDLE Mode

**Old Implementation:**
```go
// Manual IDLE command sending
// Custom goroutine for reading updates
go func() {
    reader := bufio.NewReader(c.conn.NetConn)
    for {
        line, err := reader.ReadBytes('\n')
        // Manual parsing
    }
}()
```

**New Implementation:**
```go
idleDone := make(chan struct{})
go func() {
    defer close(idleDone)
    if err := client.Idle(context.Background(), func(update imapclient.Update) {
        switch u := update.(type) {
        case *imapclient.MessageUpdate:
            handler("message", u.Message)
        case *imapclient.StateUpdate:
            handler("state", u.State)
        }
    }); err != nil {
        log.Printf("IDLE error: %v", err)
    }
}()

// To stop: close(idleDone)
```

---

### SMTP Migration Details

#### Client Creation

**Old Implementation:**
```go
netConn, err := dialer.DialContext(ctx, "tcp", host)
// Manual EHLO
// Manual STARTTLS
// Manual AUTH
```

**New Implementation (go-mail):**
```go
client, err := mail.NewClient(
    config.Host,
    mail.WithPort(config.Port),
    mail.WithSMTPAuth(mail.SMTPAuthPlain),
    mail.WithUsername(config.Username),
    mail.WithPassword(config.Password),
    mail.WithTLSPolicy(mail.TLSOpportunistic),
    mail.WithTimeout(30 * time.Second),
)
if err != nil {
    return nil, err
}
```

#### Message Construction

**Old Implementation:**
```go
var builder strings.Builder
builder.WriteString(fmt.Sprintf("From: %s <%s>\r\n", msg.From.Name, msg.From.Address))
builder.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(toAddresses, ", ")))
// ... manual header construction
// ... manual MIME boundary handling
// ... manual base64 encoding
```

**New Implementation:**
```go
m := mail.NewMsg()
m.FromFormat(msg.From.Name, msg.From.Address)

for _, to := range msg.To {
    m.ToFormat(to.Name, to.Address)
}

m.Subject(msg.Subject)

// Set body with automatic MIME handling
if msg.HTMLBody != "" && msg.TextBody != "" {
    m.SetBodyString(mail.TypeTextPlain, msg.TextBody)
    m.AddAlternativeString(mail.TypeTextHTML, msg.HTMLBody)
} else if msg.HTMLBody != "" {
    m.SetBodyString(mail.TypeTextHTML, msg.HTMLBody)
} else {
    m.SetBodyString(mail.TypeTextPlain, msg.TextBody)
}

// Add attachments with automatic encoding
for _, att := range msg.Attachments {
    m.AttachReader(att.Filename, bytes.NewReader(att.Data),
        mail.WithFileContentType(att.ContentType))
}
```

#### Sending

**Old Implementation:**
```go
// Manual MAIL FROM
// Manual RCPT TO
// Manual DATA command
// Manual message transmission
// Manual response parsing
```

**New Implementation:**
```go
if err := client.DialAndSend(m); err != nil {
    return nil, err
}
```

---

## Risk Assessment & Mitigation

### High Priority Risks

1. **API Incompatibility**
   - **Risk**: go-imap/v2 has breaking changes from v1
   - **Mitigation**: Use adapter pattern, comprehensive testing

2. **Performance Regression**
   - **Risk**: New libraries may have different performance characteristics
   - **Mitigation**: Benchmark before/after, optimize hot paths

3. **Feature Gaps**
   - **Risk**: Some custom features may not have direct equivalents
   - **Mitigation**: Implement custom extensions where needed

### Medium Priority Risks

4. **Error Handling Differences**
   - **Risk**: Different error types and handling patterns
   - **Mitigation**: Create error mapping layer

5. **Connection Pooling**
   - **Risk**: go-imap/v2 has different connection management
   - **Mitigation**: Adapt pooling strategy to library patterns

---

## Testing Strategy

### Unit Tests

```go
// Example: IMAP adapter test
func TestIMAPAdapter_ListFolders(t *testing.T) {
    // Mock IMAP server
    server := imaptest.NewServer()
    defer server.Close()
    
    config := IMAPConfig{
        Host:     server.Host(),
        Port:     server.Port(),
        Username: "test",
        Password: "test",
    }
    
    client, err := ConnectIMAPv2(context.Background(), config)
    require.NoError(t, err)
    defer client.Close()
    
    folders, err := client.ListFolders()
    require.NoError(t, err)
    assert.NotEmpty(t, folders)
}
```

### Integration Tests

```go
// Example: Real IMAP server test (requires credentials)
func TestIMAPIntegration_Gmail(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }
    
    config := IMAPConfig{
        Host:       "imap.gmail.com",
        Port:       993,
        Username:   os.Getenv("TEST_EMAIL"),
        Password:   os.Getenv("TEST_PASSWORD"),
        Encryption: models.EncryptionSSL,
    }
    
    client, err := ConnectIMAPv2(context.Background(), config)
    require.NoError(t, err)
    defer client.Close()
    
    folders, err := client.ListFolders()
    require.NoError(t, err)
    t.Logf("Found %d folders", len(folders))
}
```

---

## Rollback Plan

If migration encounters critical issues:

1. **Keep Old Code**: Don't delete old implementations until Phase 6
2. **Feature Flags**: Use config flag to switch between old/new implementations
3. **Gradual Rollout**: Migrate non-critical features first
4. **Monitoring**: Enhanced logging during migration period

```go
// Example feature flag
if config.UseNewIMAPLibrary {
    client, err = pool.ConnectIMAPv2(ctx, config)
} else {
    client, err = pool.ConnectIMAP(ctx, config)
}
```

---

## Success Criteria

### Functional
- [ ] All existing API endpoints work correctly
- [ ] Message fetching works for all supported providers
- [ ] Email sending works with all authentication methods
- [ ] Background sync operates correctly
- [ ] IDLE mode receives real-time updates

### Performance
- [ ] Connection establishment < 500ms
- [ ] Message list fetch < 200ms for 50 messages
- [ ] Email send completion < 2s
- [ ] Memory usage within 10% of current implementation

### Quality
- [ ] Code coverage > 80%
- [ ] No increase in error rate
- [ ] All existing tests pass
- [ ] Documentation updated

---

## Timeline Summary

| Phase | Duration | Deliverables |
|-------|----------|--------------|
| 1. Preparation | Week 1 | Dependencies, adapter interfaces |
| 2. IMAP Migration | Week 2-3 | IMAP adapter, updated services |
| 3. SMTP Migration | Week 3-4 | SMTP adapter, updated send service |
| 4. MIME Simplification | Week 4 | Simplified MIME parser |
| 5. Testing | Week 5 | Test suite, benchmarks |
| 6. Cleanup | Week 6 | Documentation, code cleanup |

**Total Estimated Duration**: 6 weeks

---

## Appendix: Library Comparison

### go-imap/v2 (emersion)

**Pros:**
- Actively maintained
- Modern API design
- Good documentation
- Supports IMAP4rev2
- Built-in IDLE support
- Proper context support

**Cons:**
- Breaking changes from v1
- Smaller community than some alternatives

### go-mail (wneessen)

**Pros:**
- Actively maintained (fork of unmaintained gomail)
- Modern Go features (context support, etc.)
- Rich feature set (attachments, embedded files, templates)
- Good documentation
- Automatic MIME handling

**Cons:**
- Less battle-tested than original gomail
- Some advanced SMTP extensions may require custom implementation

### Alternative: go-smtp (emersion)

If go-mail doesn't meet requirements:

**Pros:**
- Same author as go-imap
- Low-level control
- Server implementation available

**Cons:**
- More manual work required
- Less convenient API than go-mail

---

## Next Steps

1. **Review and approve** this migration plan
2. **Set up development environment** with new dependencies
3. **Create adapter interfaces** (Phase 1)
4. **Begin IMAP migration** (Phase 2)
5. **Iterate based on findings** during implementation

---

## References

- [go-imap/v2 Documentation](https://pkg.go.dev/github.com/emersion/go-imap/v2)
- [go-imap/v2 Examples](https://github.com/emersion/go-imap/tree/v2/imapclient)
- [go-mail Documentation](https://pkg.go.dev/github.com/wneessen/go-mail)
- [go-mail Examples](https://github.com/wneessen/go-mail/tree/main/examples)
- [Current Implementation Summary](./IMPLEMENTATION_SUMMARY.md)
