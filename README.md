# 2026 Agentic Webmail Engine

A headless, stateful API gateway in Go that bridges IMAP/SMTP protocols with AI agents. Features persistent connections, fair-use scheduling, and high-performance data processing.

## Features

- **Stateful Connection Pooling**: Master-Worker pattern with Go for efficient IMAP/SMTP connection management
- **Redis Metadata Cache**: Fast caching for envelopes, Thread-IDs, and message metadata
- **MIME Parsing**: Streaming MIME parser for JSON transformation
- **Fair-Use Scheduling**: Token Bucket algorithm per session to prevent provider throttling
- **SMTP Client with Pooling**: Efficient outbound email handling
- **Webhook Events**: Real-time email event processing with signature verification
- **Proxy Support**: SOCKS5/HTTP proxy configuration for network routing
- **Security**: AES-256-GCM encryption for credentials, signed URLs for attachments

## Performance Targets

- 10k concurrent sessions per 16GB RAM instance
- <15ms cache latency
- <200ms IMAP IDLE to webhook

## Project Structure

```
webmail_engine/
├── cmd/
│   └── main.go              # Application entry point
├── internal/
│   ├── api/                 # HTTP API handlers
│   ├── cache/               # Redis cache layer
│   ├── config/              # Configuration management
│   ├── mimeparser/          # MIME parsing and JSON transformation
│   ├── models/              # Data models and types
│   ├── pool/                # Connection pooling (IMAP/SMTP)
│   ├── scheduler/           # Fair-use scheduler
│   ├── service/             # Business logic services
│   ├── storage/             # Attachment storage
│   ├── webhook/             # Webhook event handling
│   └── proxy/               # Proxy configuration
├── pkg/
│   ├── imap/                # IMAP client library
│   └── smtp/                # SMTP client library
├── temp/                    # Temporary storage
├── config.example.json      # Example configuration
├── go.mod                   # Go module definition
└── README.md                # This file
```

## Quick Start

### Prerequisites

- Go 1.21 or later
- Redis 7.0 or later (optional but recommended)

### Installation

1. Clone the repository:
```bash
cd webmail_engine
```

2. Install dependencies:
```bash
go mod tidy
```

3. Create configuration:
```bash
cp config.example.json config.json
```

4. Edit `config.json` and set your encryption key (32 bytes):
```json
{
  "security": {
    "encryption_key": "your-32-byte-encryption-key-here!!"
  }
}
```

5. Run the server:
```bash
go run cmd/main.go -config config.json
```

6. Open the web interface:
```
http://localhost:8080
```

### Environment Variables

Alternatively, configure via environment variables:

```bash
export SERVER_PORT=8080
export REDIS_HOST=localhost
export REDIS_PORT=6379
export ENCRYPTION_KEY="your-32-byte-encryption-key-here!!"
export WEBHOOK_SECRET="your-webhook-secret"
export LOG_LEVEL=info

go run cmd/main.go
```

## API Endpoints

### Account Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/v1/accounts` | Add new email account |
| GET | `/v1/accounts` | List all accounts |
| GET | `/v1/accounts/{id}` | Get account details |
| PUT | `/v1/accounts/{id}` | Update account |
| DELETE | `/v1/accounts/{id}` | Remove account |
| GET | `/v1/health/accounts/{id}` | Get account status |

### Message Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/accounts/{id}/messages` | List messages |
| GET | `/v1/accounts/{id}/search` | Search messages |
| POST | `/v1/accounts/{id}/send` | Send email |

### System Health

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/health` | System health status |
| GET | `/health` | Basic health check |

### Webhooks

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/v1/webhooks` | Process webhook events |

## API Examples

### Add Email Account

```bash
curl -X POST http://localhost:8080/v1/accounts \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "auth_type": "password",
    "password": "your-password",
    "imap_host": "imap.example.com",
    "imap_port": 993,
    "imap_encryption": "ssl",
    "smtp_host": "smtp.example.com",
    "smtp_port": 587,
    "smtp_encryption": "starttls",
    "connection_limit": 5
  }'
```

### List Messages

```bash
curl http://localhost:8080/v1/accounts/{account_id}/messages?folder=INBOX&limit=50
```

### Search Messages

```bash
curl -X POST http://localhost:8080/v1/accounts/{account_id}/search \
  -H "Content-Type: application/json" \
  -d '{
    "keywords": ["important"],
    "from": "sender@example.com",
    "since": "2026-01-01T00:00:00Z",
    "limit": 100
  }'
```

### Send Email

```bash
curl -X POST http://localhost:8080/v1/accounts/{account_id}/send \
  -H "Content-Type: application/json" \
  -d '{
    "to": [{"name": "Recipient", "address": "recipient@example.com"}],
    "subject": "Test Email",
    "text_body": "Hello from Webmail Engine!",
    "html_body": "<p>Hello from <b>Webmail Engine</b>!</p>"
  }'
```

## Fair-Use Scheduling

The engine uses a token bucket algorithm to prevent email provider throttling:

- **Default bucket size**: 100 tokens
- **Refill rate**: 10 tokens/minute
- **Operation costs**:
  - FETCH: 1 token
  - LIST: 1 token
  - RETRIEVE: 2 tokens
  - SEND: 3 tokens
  - ATTACHMENT: 3 tokens
  - SEARCH: 5 tokens

When tokens are exhausted, operations are queued or return 429 (Too Many Requests).

## Configuration

See `config.example.json` for all configuration options:

- **Server**: HTTP server settings, TLS configuration
- **Redis**: Connection pool, timeouts, retries
- **Pool**: IMAP/SMTP connection limits, timeouts
- **Scheduler**: Fair-use policy defaults
- **Security**: Encryption keys, rate limiting
- **Storage**: Temporary file paths, cleanup intervals
- **Webhook**: Event processing settings

### Environment Variable Expansion

The configuration file supports environment variable expansion using the syntax:

- `${VAR_NAME}` - Expands to the value of `VAR_NAME`
- `${VAR_NAME:-default}` - Uses `default` if `VAR_NAME` is not set or empty

Example `config.json`:

```json
{
  "store": {
    "type": "sql",
    "sql": {
      "driver": "postgres",
      "dsn": "${DATABASE_URL}"
    }
  },
  "security": {
    "encryption_key": "${ENCRYPTION_KEY}",
    "webhook_secret": "${WEBHOOK_SECRET:-fallback-secret}"
  }
}
```

Copy `.env.example` to `.env` and set your environment variables:

```bash
cp .env.example .env
# Edit .env with your values
```

Then source before running:

```bash
export $(cat .env | xargs)
go run cmd/main.go -config config.json
```

## Security Considerations

1. **Encryption Key**: Must be exactly 32 bytes for AES-256-GCM
2. **Credentials**: Stored encrypted, never exposed in API responses
3. **Webhooks**: Signature verification using HMAC-SHA256
4. **Attachments**: Time-limited signed URLs with configurable expiry
5. **Rate Limiting**: Configurable request limits per time window

## Development

### Running Tests

```bash
go test ./...
```

### Building

```bash
go build -o webmail_engine ./cmd/main.go
```

### Docker (Optional)

```dockerfile
FROM golang:1.21-alpine
WORKDIR /app
COPY . .
RUN go build -o webmail_engine ./cmd/main.go
CMD ["./webmail_engine"]
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      HTTP API Layer                         │
├─────────────────────────────────────────────────────────────┤
│                      Service Layer                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │   Account   │  │   Message   │  │       Send          │ │
│  │   Service   │  │   Service   │  │      Service        │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
├─────────────────────────────────────────────────────────────┤
│                   Infrastructure Layer                      │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │ Connection  │  │    Cache    │  │    Fair-Use         │ │
│  │    Pool     │  │   (Redis)   │  │    Scheduler        │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
├─────────────────────────────────────────────────────────────┤
│                    Protocol Layer                           │
│  ┌─────────────────────────┐  ┌─────────────────────────┐  │
│  │      IMAP Client        │  │      SMTP Client        │  │
│  └─────────────────────────┘  └─────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## License

MIT License
