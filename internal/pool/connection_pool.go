package pool

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"webmail_engine/internal/models"
)

// Connection represents a pooled IMAP or SMTP connection
type Connection struct {
	ID         string
	AccountID  string
	ConnType   models.ConnectionProtocol
	NetConn    net.Conn
	TLSConn    *tls.Conn
	CreatedAt  time.Time
	LastUsedAt time.Time
	InUse      bool
	ErrorCount int
}

// Close closes the connection
func (c *Connection) Close() error {
	if c.TLSConn != nil {
		c.TLSConn.Close()
	}
	if c.NetConn != nil {
		c.NetConn.Close()
	}
	return nil
}

// ConnectionPool manages a pool of IMAP/SMTP connections
type ConnectionPool struct {
	mu              sync.RWMutex
	connections     map[string]*Connection
	accountConnections map[string][]string // accountID -> connection IDs
	maxConnections  int
	idleTimeout     time.Duration
	dialTimeout     time.Duration
	metrics         PoolMetrics
}

// PoolMetrics tracks connection pool statistics
type PoolMetrics struct {
	TotalCreated   int64
	TotalClosed    int64
	ActiveCount    int64
	IdleCount      int64
	WaitCount      int64
	ErrorCount     int64
	LastMetricTime time.Time
}

// PoolConfig represents connection pool configuration
type PoolConfig struct {
	MaxConnections int
	IdleTimeout    time.Duration
	DialTimeout    time.Duration
}

// DefaultPoolConfig returns default pool configuration
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConnections: 100,
		IdleTimeout:    5 * time.Minute,
		DialTimeout:    30 * time.Second,
	}
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(config PoolConfig) *ConnectionPool {
	return &ConnectionPool{
		connections:        make(map[string]*Connection),
		accountConnections: make(map[string][]string),
		maxConnections:     config.MaxConnections,
		idleTimeout:        config.IdleTimeout,
		dialTimeout:        config.DialTimeout,
		metrics: PoolMetrics{
			LastMetricTime: time.Now(),
		},
	}
}

// Acquire gets or creates a connection for an account
func (p *ConnectionPool) Acquire(ctx context.Context, accountID string, connType models.ConnectionProtocol, config models.ServerConfig, proxyConfig *models.ProxySettings) (*Connection, error) {
	p.mu.Lock()
	
	// Check if we're at capacity
	if len(p.connections) >= p.maxConnections {
		p.metrics.WaitCount++
		p.mu.Unlock()
		
		// Try to clean up idle connections
		p.cleanupIdle()
		
		p.mu.Lock()
		if len(p.connections) >= p.maxConnections {
			p.metrics.ErrorCount++
			p.mu.Unlock()
			return nil, models.NewCapacityError()
		}
	}
	
	// Try to get an existing idle connection for this account
	connID := p.findIdleConnection(accountID, connType)
	if connID != "" {
		conn := p.connections[connID]
		conn.InUse = true
		conn.LastUsedAt = time.Now()
		p.mu.Unlock()
		return conn, nil
	}
	
	p.mu.Unlock()
	
	// Create a new connection
	conn, err := p.createConnection(ctx, accountID, connType, config, proxyConfig)
	if err != nil {
		p.mu.Lock()
		p.metrics.ErrorCount++
		p.mu.Unlock()
		return nil, err
	}
	
	p.mu.Lock()
	p.connections[conn.ID] = conn
	p.accountConnections[accountID] = append(p.accountConnections[accountID], conn.ID)
	p.metrics.TotalCreated++
	p.metrics.ActiveCount++
	p.mu.Unlock()
	
	return conn, nil
}

// Release returns a connection to the pool
func (p *ConnectionPool) Release(connID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	conn, exists := p.connections[connID]
	if !exists {
		return
	}
	
	conn.InUse = false
	conn.LastUsedAt = time.Now()
	p.metrics.ActiveCount--
	p.metrics.IdleCount++
}

// Close closes a specific connection
func (p *ConnectionPool) Close(connID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	conn, exists := p.connections[connID]
	if !exists {
		return
	}
	
	p.closeConnection(conn)
	delete(p.connections, connID)
	p.metrics.TotalClosed++
	
	// Remove from account connections
	accountConns := p.accountConnections[conn.AccountID]
	for i, id := range accountConns {
		if id == connID {
			p.accountConnections[conn.AccountID] = append(accountConns[:i], accountConns[i+1:]...)
			break
		}
	}
}

// CloseAccount closes all connections for an account
func (p *ConnectionPool) CloseAccount(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	connIDs := p.accountConnections[accountID]
	for _, connID := range connIDs {
		conn, exists := p.connections[connID]
		if exists {
			p.closeConnection(conn)
			delete(p.connections, connID)
			p.metrics.TotalClosed++
		}
	}
	
	delete(p.accountConnections, accountID)
}

// GetStatus returns pool status information
func (p *ConnectionPool) GetStatus() PoolMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	metrics := p.metrics
	metrics.ActiveCount = int64(len(p.connections))
	metrics.IdleCount = metrics.ActiveCount - metrics.WaitCount
	metrics.LastMetricTime = time.Now()
	
	return metrics
}

// GetAccountConnectionCount returns the number of connections for an account
func (p *ConnectionPool) GetAccountConnectionCount(accountID string) int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	return len(p.accountConnections[accountID])
}

// findIdleConnection finds an idle connection for the account and type
func (p *ConnectionPool) findIdleConnection(accountID string, connType models.ConnectionProtocol) string {
	connIDs := p.accountConnections[accountID]
	now := time.Now()
	
	for _, connID := range connIDs {
		conn := p.connections[connID]
		if conn == nil {
			continue
		}
		
		if conn.ConnType == connType && !conn.InUse {
			// Check if connection is still valid (not timed out)
			if now.Sub(conn.LastUsedAt) < p.idleTimeout {
				return connID
			}
		}
	}
	
	return ""
}

// createConnection creates a new network connection
func (p *ConnectionPool) createConnection(ctx context.Context, accountID string, connType models.ConnectionProtocol, config models.ServerConfig, proxyConfig *models.ProxySettings) (*Connection, error) {
	connID := fmt.Sprintf("%s-%s-%d", accountID, connType, time.Now().UnixNano())
	
	var dialer net.Dialer
	dialer.Timeout = p.dialTimeout
	
	// Configure proxy if enabled
	if proxyConfig != nil && proxyConfig.Enabled {
		// Proxy configuration would be applied here
		// For now, we'll use direct connection
	}
	
	host := fmt.Sprintf("%s:%d", config.Host, config.Port)
	
	netConn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", connType, err)
	}
	
	var tlsConn *tls.Conn
	
	// Apply TLS if configured
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
		ID:         connID,
		AccountID:  accountID,
		ConnType:   connType,
		NetConn:    netConn,
		TLSConn:    tlsConn,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		InUse:      true,
	}
	
	return conn, nil
}

// closeConnection closes a connection
func (p *ConnectionPool) closeConnection(conn *Connection) {
	if conn.TLSConn != nil {
		conn.TLSConn.Close()
	}
	if conn.NetConn != nil {
		conn.NetConn.Close()
	}
}

// cleanupIdle removes idle connections that have timed out
func (p *ConnectionPool) cleanupIdle() {
	now := time.Now()
	
	for connID, conn := range p.connections {
		if !conn.InUse && now.Sub(conn.LastUsedAt) > p.idleTimeout {
			p.closeConnection(conn)
			delete(p.connections, connID)
			p.metrics.TotalClosed++
			
			// Remove from account connections
			accountConns := p.accountConnections[conn.AccountID]
			for i, id := range accountConns {
				if id == connID {
					p.accountConnections[conn.AccountID] = append(accountConns[:i], accountConns[i+1:]...)
					break
				}
			}
		}
	}
}

// StartCleanup starts periodic cleanup of idle connections
func (p *ConnectionPool) StartCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			p.cleanupIdle()
			p.mu.Unlock()
		}
	}
}
