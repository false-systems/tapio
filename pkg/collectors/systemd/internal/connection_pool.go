package internal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
)

// ConnectionPool manages a pool of D-Bus connections
type ConnectionPool struct {
	mu             sync.Mutex
	connections    []*pooledConnection
	available      chan *pooledConnection
	maxConnections int
	connectionFunc func() (core.DBusConnection, error)

	// Metrics
	totalConnections  int
	activeConnections int
	failedConnections uint64
	waitTime          time.Duration
}

// pooledConnection wraps a connection with metadata
type pooledConnection struct {
	conn     core.DBusConnection
	id       int
	lastUsed time.Time
	useCount uint64
	errors   uint64
	pool     *ConnectionPool
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(maxConnections int, connectionFunc func() (core.DBusConnection, error)) (*ConnectionPool, error) {
	if maxConnections <= 0 {
		maxConnections = 5
	}

	pool := &ConnectionPool{
		connections:    make([]*pooledConnection, 0, maxConnections),
		available:      make(chan *pooledConnection, maxConnections),
		maxConnections: maxConnections,
		connectionFunc: connectionFunc,
	}

	// Pre-create minimum connections
	minConnections := 2
	if minConnections > maxConnections {
		minConnections = 1
	}

	for i := 0; i < minConnections; i++ {
		conn, err := pool.createConnection()
		if err != nil {
			// Clean up any created connections
			pool.Close()
			return nil, fmt.Errorf("failed to create initial connections: %w", err)
		}
		pool.available <- conn
	}

	return pool, nil
}

// Get retrieves a connection from the pool
func (p *ConnectionPool) Get(ctx context.Context) (*pooledConnection, error) {
	startTime := time.Now()
	defer func() {
		p.mu.Lock()
		p.waitTime += time.Since(startTime)
		p.mu.Unlock()
	}()

	select {
	case conn := <-p.available:
		// Check if connection is still valid
		if conn.conn.IsConnected() {
			p.mu.Lock()
			p.activeConnections++
			p.mu.Unlock()
			return conn, nil
		}

		// Connection is dead, try to recreate
		if err := p.recreateConnection(conn); err != nil {
			return nil, err
		}

		p.mu.Lock()
		p.activeConnections++
		p.mu.Unlock()
		return conn, nil

	case <-ctx.Done():
		return nil, ctx.Err()

	default:
		// No available connections, try to create a new one
		p.mu.Lock()
		canCreate := len(p.connections) < p.maxConnections
		p.mu.Unlock()

		if canCreate {
			conn, err := p.createConnection()
			if err != nil {
				return nil, err
			}
			p.mu.Lock()
			p.activeConnections++
			p.mu.Unlock()
			return conn, nil
		}

		// Wait for an available connection
		select {
		case conn := <-p.available:
			if !conn.conn.IsConnected() {
				if err := p.recreateConnection(conn); err != nil {
					return nil, err
				}
			}
			p.mu.Lock()
			p.activeConnections++
			p.mu.Unlock()
			return conn, nil

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Put returns a connection to the pool
func (p *ConnectionPool) Put(conn *pooledConnection) {
	if conn == nil {
		return
	}

	p.mu.Lock()
	p.activeConnections--
	p.mu.Unlock()

	conn.lastUsed = time.Now()
	conn.useCount++

	// Check if connection is still healthy
	if !conn.conn.IsConnected() || conn.errors > 10 {
		// Connection is unhealthy, recreate it asynchronously
		go func() {
			if err := p.recreateConnection(conn); err != nil {
				p.mu.Lock()
				p.failedConnections++
				p.mu.Unlock()
			} else {
				p.available <- conn
			}
		}()
		return
	}

	// Return healthy connection to pool
	select {
	case p.available <- conn:
		// Successfully returned
	default:
		// Pool is full, close the connection
		conn.conn.Disconnect()
	}
}

// createConnection creates a new pooled connection
func (p *ConnectionPool) createConnection() (*pooledConnection, error) {
	conn, err := p.connectionFunc()
	if err != nil {
		p.mu.Lock()
		p.failedConnections++
		p.mu.Unlock()
		return nil, err
	}

	if err := conn.Connect(); err != nil {
		p.mu.Lock()
		p.failedConnections++
		p.mu.Unlock()
		return nil, err
	}

	p.mu.Lock()
	p.totalConnections++
	id := p.totalConnections

	pooledConn := &pooledConnection{
		conn:     conn,
		id:       id,
		lastUsed: time.Now(),
		pool:     p,
	}

	p.connections = append(p.connections, pooledConn)
	p.mu.Unlock()

	return pooledConn, nil
}

// recreateConnection recreates a dead connection
func (p *ConnectionPool) recreateConnection(pooledConn *pooledConnection) error {
	// Disconnect old connection
	if pooledConn.conn != nil {
		pooledConn.conn.Disconnect()
	}

	// Create new connection
	conn, err := p.connectionFunc()
	if err != nil {
		p.mu.Lock()
		p.failedConnections++
		p.mu.Unlock()
		return err
	}

	if err := conn.Connect(); err != nil {
		p.mu.Lock()
		p.failedConnections++
		p.mu.Unlock()
		return err
	}

	// Update pooled connection
	pooledConn.conn = conn
	pooledConn.errors = 0
	pooledConn.lastUsed = time.Now()

	return nil
}

// HealthCheck checks the health of all connections
func (p *ConnectionPool) HealthCheck() {
	p.mu.Lock()
	connections := make([]*pooledConnection, len(p.connections))
	copy(connections, p.connections)
	p.mu.Unlock()

	for _, conn := range connections {
		if !conn.conn.IsConnected() {
			// Try to reconnect
			go func(c *pooledConnection) {
				if err := p.recreateConnection(c); err != nil {
					p.mu.Lock()
					p.failedConnections++
					p.mu.Unlock()
				}
			}(conn)
		}
	}
}

// Metrics returns pool metrics
func (p *ConnectionPool) Metrics() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	avgWaitTime := time.Duration(0)
	if p.totalConnections > 0 {
		avgWaitTime = p.waitTime / time.Duration(p.totalConnections)
	}

	return map[string]interface{}{
		"total_connections":     p.totalConnections,
		"active_connections":    p.activeConnections,
		"available_connections": len(p.available),
		"failed_connections":    p.failedConnections,
		"max_connections":       p.maxConnections,
		"avg_wait_time_ms":      avgWaitTime.Milliseconds(),
	}
}

// Close closes all connections in the pool
func (p *ConnectionPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Close available channel
	close(p.available)

	// Disconnect all connections
	var lastErr error
	for _, conn := range p.connections {
		if err := conn.conn.Disconnect(); err != nil {
			lastErr = err
		}
	}

	p.connections = nil
	return lastErr
}

// Execute executes a function with a pooled connection
func (p *ConnectionPool) Execute(ctx context.Context, fn func(core.DBusConnection) error) error {
	conn, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(conn)

	// Execute the function
	err = fn(conn.conn)
	if err != nil {
		conn.errors++
	}

	return err
}
