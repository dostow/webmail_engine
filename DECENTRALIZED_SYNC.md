# Decentralized Sync Architecture

This document describes the decentralized synchronization architecture for the webmail engine.

## Overview

The webmail engine is split into three independent components that can be deployed and scaled separately:

```
┌─────────────────┐      ┌──────────────────┐      ┌─────────────────┐
│   Sync Worker   │      │  Processor       │      │  Webmail Engine │
│   (IMAP fetch)  │─────▶│  Worker          │─────▶│  (API Server)   │
│                 │      │  (Body fetch)    │      │                 │
└─────────────────┘      └──────────────────┘      └─────────────────┘
         │                        │                        │
         └────────────────────────┼────────────────────────┘
                                  │
                    ┌─────────────▼─────────────┐
                    │   External Queue (Redis)  │
                    │   or Memory (testing)     │
                    └───────────────────────────┘
```

## Components

### 1. Webmail Engine (API Server)

**Purpose**: Serve HTTP API requests, read from cache/database

**Command**: `go run cmd/main.go`

**Responsibilities**:
- Handle user requests (list messages, read emails, send emails)
- Serve cached message data
- Manage account authentication
- Handle webhooks

**Does NOT do**:
- IMAP synchronization
- Envelope processing

**Configuration**: Uses main `config.json` or environment variables

### 2. Sync Worker

**Purpose**: Fetch email envelopes from IMAP servers and enqueue for processing

**Command**: `go run cmd/sync_worker/main.go`

**Responsibilities**:
- Connect to IMAP servers
- Fetch email envelopes (headers only, not bodies)
- Determine processing priority based on flags/folder
- Enqueue envelopes to external queue
- Track sync state per folder

**Configuration**:
```bash
# Using command line flags
./sync_worker \
  -queue=redis \
  -redis=redis://localhost:6379 \
  -db=data/webmail.db

# Or using config file
./sync_worker -config=config/sync_worker.json
```

**Config file** (`config/sync_worker.json`):
```json
{
  "worker_type": "sync",
  "worker_id": "sync-worker-1",
  "queue": {
    "type": "redis",
    "redis_url": "redis://localhost:6379",
    "high_priority": "envelope_high",
    "normal_priority": "envelope_normal",
    "low_priority": "envelope_low"
  },
  "store": {
    "type": "sqlite",
    "sqlite": {
      "path": "data/webmail.db"
    }
  },
  "logging": {
    "level": "info",
    "format": "text"
  },
  "shutdown_timeout": "30s"
}
```

### 3. Processor Worker

**Purpose**: Process envelopes from queue, fetch full message bodies, extract data

**Command**: `go run cmd/processor_worker/main.go`

**Responsibilities**:
- Dequeue envelopes from external queue
- Fetch full message content (including bodies)
- Extract links and attachments
- Update message cache
- Trigger webhooks for new messages

**Configuration**:
```bash
# Using command line flags
./processor_worker \
  -queue=redis \
  -redis=redis://localhost:6379 \
  -db=data/webmail.db \
  -attachments=data/attachments \
  -concurrency=4

# Or using config file
./processor_worker -config=config/processor_worker.json
```

**Config file** (`config/processor_worker.json`):
```json
{
  "worker_type": "processor",
  "worker_id": "processor-worker-1",
  "queue": {
    "type": "redis",
    "redis_url": "redis://localhost:6379",
    "high_priority": "envelope_high",
    "normal_priority": "envelope_normal",
    "low_priority": "envelope_low"
  },
  "store": {
    "type": "sqlite",
    "sqlite": {
      "path": "data/webmail.db"
    }
  },
  "processor_config": {
    "concurrency": 4,
    "batch_size": 20,
    "poll_interval": "5s",
    "enable_body_fetch": true,
    "enable_link_extraction": true,
    "enable_attachment_processing": true,
    "cleanup_interval": "1h",
    "cleanup_age": "24h"
  },
  "shutdown_timeout": "30s"
}
```

## Deployment Scenarios

### Development (Single Machine, Memory Queue)

**Option A: Combined Command (Simplest)**
```bash
# Single process runs both sync and processor
./memory_worker -db=data/webmail.db -concurrency=4
```

**Option B: Separate Commands**
```bash
# Terminal 1: Start API server
go run cmd/main.go -dev

# Terminal 2: Start combined memory worker
go run cmd/memory_worker/main.go -db=data/webmail.db

# Or separate workers (note: they won't share queue across processes)
go run cmd/sync_worker/main.go -queue=memory
go run cmd/processor_worker/main.go -queue=memory
```

### Production (Distributed, Redis Queue)

```bash
# Machine 1: API Server (webmail engine)
./webmail_engine --config=config/api.json

# Machine 2-4: Sync Workers (can run multiple for scale)
./sync_worker --config=config/sync_worker.json

# Machine 5-8: Processor Workers (can run multiple for scale)
./processor_worker --config=config/processor_worker.json
```

### Docker Deployment

```yaml
# docker-compose.yml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  api:
    build: .
    command: ["./webmail_engine", "--config=/app/config/api.json"]
    volumes:
      - ./data:/app/data
      - ./config:/app/config
    ports:
      - "8080:8080"
    depends_on:
      - redis

  sync-worker:
    build: .
    command: ["./sync_worker", "--config=/app/config/sync_worker.json"]
    volumes:
      - ./data:/app/data
      - ./config:/app/config
    depends_on:
      - redis
    deploy:
      replicas: 2

  processor-worker:
    build: .
    command: ["./processor_worker", "--config=/app/config/processor_worker.json"]
    volumes:
      - ./data:/app/data
      - ./attachments:/app/attachments
    depends_on:
      - redis
    deploy:
      replicas: 4
```

## Queue Priorities

Envelopes are processed in priority order:

| Priority | Condition |
|----------|-----------|
| **High** | UNSEEN or FLAGGED messages in INBOX |
| **Normal** | Regular INBOX messages |
| **Low** | Archive, Sent, Trash, other folders |

## Scaling Recommendations

### Sync Worker
- Scale based on number of accounts and IMAP polling frequency
- Each worker can handle multiple accounts
- Watch for IMAP server rate limits

### Processor Worker
- Scale based on message volume and processing complexity
- More concurrency = more IMAP connections needed
- Monitor attachment storage I/O

### API Server
- Scale based on user traffic
- Stateless - can scale horizontally behind load balancer
- Cache hit rate affects database load

## Monitoring

### Key Metrics

**Sync Worker**:
- Envelopes fetched per minute
- Sync errors by account
- Queue depth (pending envelopes)

**Processor Worker**:
- Messages processed per minute
- Processing time (avg, p95, p99)
- Failed processing rate
- Attachment storage usage

**API Server**:
- Request latency
- Cache hit rate
- Active connections

### Health Checks

```bash
# API Server health
curl http://localhost:8080/health

# Check queue depth (Redis)
redis-cli LLEN envelope_high
redis-cli LLEN envelope_normal
redis-cli LLEN envelope_low
```

## Queue Types

### Memory Queue (Single-Process Testing)
- **Type**: `memory`
- **Use Case**: Development and integrated testing where sync and processor run in the same process
- **Implementation**: Channel-based producer-consumer pattern
- **Benefit**: No polling - workers block on channels waiting for envelopes

**Option 1: Separate Commands (Shared Queue)**
```bash
# Terminal 1: Start sync worker
./sync_worker -queue=memory

# Terminal 2: Start processor worker (must share queue instance)
./processor_worker -queue=memory
```

**Option 2: Combined Command (Recommended for Development)**
```bash
# Single command runs both sync and processor together
./memory_worker -db=data/webmail.db -concurrency=4 -sync-interval=60
```

```go
// Example: Using memory queue in code
queue := envelopequeue.NewMemoryEnvelopeQueue(envelopequeue.DefaultMemoryEnvelopeQueueConfig())

// Start sync worker (producer)
syncWorker, _ := worker.NewSyncWorker(cfg, &worker.SyncWorkerOptions{
    Queue: queue,
})
syncWorker.Start()

// Start processor worker (consumer)
processorWorker, _ := worker.NewProcessorWorker(cfg, &worker.ProcessorWorkerOptions{
    Queue: queue,
})
processorWorker.Start()
```

### Redis Queue (Production)
- **Type**: `redis`
- **Use Case**: Production deployments, distributed workers across multiple machines
- **Implementation**: Uses Machinery library with Redis backend
- **Benefit**: Cross-process communication, persistent queue, horizontal scaling

```bash
./sync_worker -queue=redis -redis=redis://localhost:6379
./processor_worker -queue=redis -redis=redis://localhost:6379
```

## Queue Comparison

| Feature | Memory | Redis |
|---------|--------|-------|
| Cross-process | ❌ | ✅ |
| Persistent | ❌ | ✅ |
| Polling | No | No |
| Best For | Testing/Dev | Production |

## Migration from Coupled Architecture

If migrating from the coupled architecture:

1. **Deploy workers alongside existing system**
   - Start sync worker in parallel
   - Monitor queue depth

2. **Disable internal sync in API server**
   - The refactored `cmd/main.go` has no sync

3. **Scale workers based on load**
   - Add more sync workers for more accounts
   - Add more processors for faster processing

4. **Monitor and tune**
   - Adjust concurrency based on IMAP server limits
   - Tune queue priorities based on business needs
