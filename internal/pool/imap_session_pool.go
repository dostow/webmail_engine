package pool

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// IMAPSessionPool manages authenticated IMAP connections per account.
// It keeps connections alive to avoid reconnecting and authenticating 
// for every single API request, which causes IP bans.
type IMAPSessionPool struct {
	mu            sync.Mutex
	sessions      map[string]*pooledSession
	config        SessionPoolConfig
	reuseCount    int64
	totalConnects int64
	lastFailure   map[string]time.Time
}

type pooledSession struct {
	adapter    *IMAPAdapter
	config     IMAPConfig
	mu         sync.Mutex
	inUse      int       // reference count
	lastUsed   time.Time
	createdAt  time.Time
	healthy    bool
}

// SessionPoolConfig configures the IMAP session pool
type SessionPoolConfig struct {
	MaxIdleTimeout  time.Duration // Time before closing an idle connection
	KeepAliveEvery  time.Duration // How often to send NOOP
	MaxSessionAge   time.Duration // Maximum absolute age of a connection
}

// SessionPoolStats contains statistics about the session pool
type SessionPoolStats struct {
	ActiveSessions int   `json:"active_sessions"`
	ReuseCount     int64 `json:"reuse_count"`
	TotalConnects  int64 `json:"total_connects"`
}

// DefaultSessionPoolConfig returns safe defaults for IMAP
func DefaultSessionPoolConfig() SessionPoolConfig {
	return SessionPoolConfig{
		MaxIdleTimeout: 15 * time.Minute,
		KeepAliveEvery: 4 * time.Minute,
		MaxSessionAge:  30 * time.Minute,
	}
}

// NewIMAPSessionPool creates a new session pool
func NewIMAPSessionPool(config SessionPoolConfig) *IMAPSessionPool {
	return &IMAPSessionPool{
		sessions:    make(map[string]*pooledSession),
		lastFailure: make(map[string]time.Time),
		config:      config,
	}
}

// Acquire gets an authenticated IMAP client for the given account.
// It returns a release function that MUST be called when done.
func (p *IMAPSessionPool) Acquire(ctx context.Context, accountID string, config IMAPConfig) (*IMAPAdapter, func(), error) {
	p.mu.Lock()
	
	// Check for recent failures to avoid hammer-banning the IP
	if lastFail, ok := p.lastFailure[accountID]; ok {
		if time.Since(lastFail) < 10*time.Second {
			p.mu.Unlock()
			return nil, nil, fmt.Errorf("connection throttled for %s due to recent failure (wait 10s)", accountID)
		}
	}

	session, exists := p.sessions[accountID]
	
	if exists && session.healthy {
		// Session exists and is healthy
		session.mu.Lock()
		
		if session.inUse == 0 {
			// Fast path: session is idle, reuse it immediately
			session.inUse = 1
			session.lastUsed = time.Now()
			
			p.reuseCount++
			
			session.mu.Unlock()
			p.mu.Unlock()
			
			// We return a release function for this adapter
			release := func() {
				p.Release(accountID)
			}
			return session.adapter, release, nil
		}
		
		// The connection is currently in use by another goroutine.
		// Since IMAP connections generally process one command at a time,
		// we must create a temporary unpooled connection to avoid blocking it.
		// (Alternatively, we could wait, but creating a temporary burst connection is safer).
		session.mu.Unlock()
		p.mu.Unlock()
		
		log.Printf("[IMAP Pool] Burst connection created for %s (primary session in use)", accountID)
		p.totalConnects++
		tempAdapter, err := ConnectIMAPv2(ctx, config)
		if err != nil {
			p.mu.Lock()
			p.lastFailure[accountID] = time.Now()
			p.mu.Unlock()
			return nil, nil, err
		}
		
		release := func() {
			tempAdapter.Close()
		}
		return tempAdapter, release, nil
	}
	
	p.mu.Unlock()

	// If we got here, we need to create a new pooled session
	log.Printf("[IMAP Pool] Creating new primary session for %s", accountID)
	
	p.mu.Lock()
	p.totalConnects++
	p.mu.Unlock()
	
	adapter, err := ConnectIMAPv2(ctx, config)
	if err != nil {
		p.mu.Lock()
		p.lastFailure[accountID] = time.Now()
		p.mu.Unlock()
		return nil, nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check if another goroutine created it while we were connecting
	if existing, ok := p.sessions[accountID]; ok && existing.healthy {
		// Somebody else beat us to it - use ours as a temporary/burst
		p.reuseCount++
		release := func() {
			adapter.Close()
		}
		return adapter, release, nil
	}

	now := time.Now()
	newSession := &pooledSession{
		adapter:   adapter,
		config:    config,
		inUse:     1,
		lastUsed:  now,
		createdAt: now,
		healthy:   true,
	}
	
	// Close any old unhealthy session we're replacing
	if oldSession, ok := p.sessions[accountID]; ok {
		go oldSession.adapter.Close()
	}
	
	p.sessions[accountID] = newSession

	release := func() {
		p.Release(accountID)
	}
	
	return adapter, release, nil
}

// Release marks a session as idle
func (p *IMAPSessionPool) Release(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	session, ok := p.sessions[accountID]
	if !ok {
		return
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	
	session.inUse = 0
	session.lastUsed = time.Now()
}

// Stats returns current statistics of the session pool
func (p *IMAPSessionPool) Stats() SessionPoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	return SessionPoolStats{
		ActiveSessions: len(p.sessions),
		ReuseCount:     p.reuseCount,
		TotalConnects:  p.totalConnects,
	}
}

// Invalidate removes and closes a session for an account (e.g., if auth changes)

func (p *IMAPSessionPool) Invalidate(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if session, ok := p.sessions[accountID]; ok {
		session.healthy = false
		go session.adapter.Close()
		delete(p.sessions, accountID)
	}
}

// StartMaintenance starts the background routines for keep-alive and cleanup
func (p *IMAPSessionPool) StartMaintenance(ctx context.Context) {
	keepAliveTicker := time.NewTicker(p.config.KeepAliveEvery)
	cleanupTicker := time.NewTicker(1 * time.Minute)
	
	defer keepAliveTicker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Shutdown all active connections
			p.mu.Lock()
			for _, session := range p.sessions {
				session.adapter.Close()
			}
			p.sessions = make(map[string]*pooledSession)
			p.mu.Unlock()
			return
			
		case <-keepAliveTicker.C:
			p.performKeepAlive()
			
		case <-cleanupTicker.C:
			p.performCleanup()
		}
	}
}

func (p *IMAPSessionPool) performKeepAlive() {
	p.mu.Lock()
	// Collect sessions to ping (to minimize lock holding time)
	type pingTarget struct {
		accountID string
		session   *pooledSession
	}
	var targets []pingTarget
	
	for id, session := range p.sessions {
		session.mu.Lock()
		if session.healthy && session.inUse == 0 {
			targets = append(targets, pingTarget{id, session})
			session.inUse = 1 // temporarily mark in use during ping
		}
		session.mu.Unlock()
	}
	p.mu.Unlock()

	// Perform pings outside the main map lock
	for _, target := range targets {
		// Use a simple NOOP command to keep the session alive
		cmd := target.session.adapter.client.Noop()
		if err := cmd.Wait(); err != nil {
			log.Printf("[IMAP Pool] KeepAlive failed for %s: %v", target.accountID, err)
			target.session.healthy = false
		}
		
		// Release the session
		target.session.mu.Lock()
		target.session.inUse = 0
		target.session.mu.Unlock()
	}
}

func (p *IMAPSessionPool) performCleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	now := time.Now()
	
	for id, session := range p.sessions {
		session.mu.Lock()
		
		// Unhealthy sessions are dumped
		if !session.healthy {
			session.mu.Unlock()
			go p.Invalidate(id)
			continue
		}
		
		// We only cleanup idle sessions
		if session.inUse == 0 {
			age := now.Sub(session.createdAt)
			idleTime := now.Sub(session.lastUsed)
			
			if idleTime > p.config.MaxIdleTimeout || age > p.config.MaxSessionAge {
				log.Printf("[IMAP Pool] Expiring session for %s (idle: %v, age: %v)", id, idleTime, age)
				session.healthy = false
				session.mu.Unlock()
				go p.Invalidate(id)
				continue
			}
		}
		
		session.mu.Unlock()
	}
}
