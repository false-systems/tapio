package managers

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/server/domain"
)

// ConnectionManager manages client connections
type ConnectionManager struct {
	connections map[string]*connectionEntry
	mu          sync.RWMutex

	maxConnections int
	activeCount    atomic.Int64
	totalCount     atomic.Int64

	metricsCollector domain.MetricsCollector
	eventPublisher   domain.EventPublisher
	logger           domain.Logger
}

type connectionEntry struct {
	connection *domain.Connection
	mu         sync.RWMutex
	lastPing   time.Time
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager(
	maxConnections int,
	metricsCollector domain.MetricsCollector,
	eventPublisher domain.EventPublisher,
	logger domain.Logger,
) *ConnectionManager {
	return &ConnectionManager{
		connections:      make(map[string]*connectionEntry),
		maxConnections:   maxConnections,
		metricsCollector: metricsCollector,
		eventPublisher:   eventPublisher,
		logger:           logger,
	}
}

// AcceptConnection accepts a new connection
func (m *ConnectionManager) AcceptConnection(ctx context.Context, connection *domain.Connection) error {
	if connection == nil {
		return domain.ErrInvalidRequest("connection cannot be nil")
	}

	// Check connection limit
	currentActive := m.activeCount.Load()
	if currentActive >= int64(m.maxConnections) {
		return domain.ErrResourceExhaustedGeneric("maximum connections reached")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if connection already exists
	if _, exists := m.connections[connection.ID]; exists {
		return domain.ErrResourceAlreadyExists(fmt.Sprintf("connection %s already exists", connection.ID))
	}

	// Create connection entry
	entry := &connectionEntry{
		connection: connection,
		lastPing:   time.Now(),
	}

	// Store connection
	m.connections[connection.ID] = entry
	m.activeCount.Add(1)
	m.totalCount.Add(1)

	// Record metrics
	if m.metricsCollector != nil {
		m.metricsCollector.RecordRequest(ctx, &domain.Request{
			Type: "connection_accepted",
			Data: map[string]interface{}{
				"connection_id": connection.ID,
				"protocol":      connection.Protocol,
				"remote_addr":   connection.RemoteAddress,
			},
		})
	}

	// Publish event
	if m.eventPublisher != nil {
		event := &domain.Event{
			ID:        fmt.Sprintf("conn-accept-%s", connection.ID),
			Type:      domain.EventTypeConnection,
			Severity:  domain.SeverityInfo,
			Source:    "connection_manager",
			Message:   fmt.Sprintf("connection accepted: %s", connection.ID),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"connection_id": connection.ID,
				"protocol":      connection.Protocol,
				"remote_addr":   connection.RemoteAddress,
			},
			Context: ctx,
		}
		m.eventPublisher.PublishEvent(ctx, event)
	}

	// Log connection
	if m.logger != nil {
		m.logger.Info(ctx, fmt.Sprintf("connection accepted: %s from %s",
			connection.ID, connection.RemoteAddress))
	}

	return nil
}

// CloseConnection closes a connection
func (m *ConnectionManager) CloseConnection(ctx context.Context, connectionID string) error {
	if connectionID == "" {
		return domain.ErrInvalidRequest("connection ID cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.connections[connectionID]
	if !exists {
		return domain.ErrResourceNotFound(fmt.Sprintf("connection %s not found", connectionID))
	}

	// Update connection status
	entry.mu.Lock()
	entry.connection.Status = domain.ConnectionClosed
	entry.mu.Unlock()

	// Remove from active connections
	delete(m.connections, connectionID)
	m.activeCount.Add(-1)

	// Record metrics
	if m.metricsCollector != nil {
		m.metricsCollector.RecordRequest(ctx, &domain.Request{
			Type: "connection_closed",
			Data: map[string]interface{}{
				"connection_id": connectionID,
				"duration":      time.Since(entry.connection.StartTime),
			},
		})
	}

	// Publish event
	if m.eventPublisher != nil {
		event := &domain.Event{
			ID:        fmt.Sprintf("conn-close-%s", connectionID),
			Type:      domain.EventTypeConnection,
			Severity:  domain.SeverityInfo,
			Source:    "connection_manager",
			Message:   fmt.Sprintf("connection closed: %s", connectionID),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"connection_id": connectionID,
				"duration":      time.Since(entry.connection.StartTime).String(),
			},
			Context: ctx,
		}
		m.eventPublisher.PublishEvent(ctx, event)
	}

	// Log closure
	if m.logger != nil {
		m.logger.Info(ctx, fmt.Sprintf("connection closed: %s", connectionID))
	}

	return nil
}

// GetConnection gets a connection by ID
func (m *ConnectionManager) GetConnection(ctx context.Context, connectionID string) (*domain.Connection, error) {
	if connectionID == "" {
		return nil, domain.ErrInvalidRequest("connection ID cannot be empty")
	}

	m.mu.RLock()
	entry, exists := m.connections[connectionID]
	m.mu.RUnlock()

	if !exists {
		return nil, domain.ErrResourceNotFound(fmt.Sprintf("connection %s not found", connectionID))
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()

	// Return a copy to avoid race conditions
	connCopy := *entry.connection
	return &connCopy, nil
}

// GetConnections gets all active connections
func (m *ConnectionManager) GetConnections(ctx context.Context) ([]*domain.Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	connections := make([]*domain.Connection, 0, len(m.connections))

	for _, entry := range m.connections {
		entry.mu.RLock()
		connCopy := *entry.connection
		entry.mu.RUnlock()
		connections = append(connections, &connCopy)
	}

	return connections, nil
}

// GetConnectionMetrics gets metrics for a specific connection
func (m *ConnectionManager) GetConnectionMetrics(ctx context.Context, connectionID string) (*domain.ConnectionMetrics, error) {
	if connectionID == "" {
		return nil, domain.ErrInvalidRequest("connection ID cannot be empty")
	}

	m.mu.RLock()
	entry, exists := m.connections[connectionID]
	m.mu.RUnlock()

	if !exists {
		return nil, domain.ErrResourceNotFound(fmt.Sprintf("connection %s not found", connectionID))
	}

	entry.mu.RLock()
	metrics := entry.connection.Metrics
	entry.mu.RUnlock()

	return &metrics, nil
}

// CleanupIdleConnections removes idle connections
func (m *ConnectionManager) CleanupIdleConnections(ctx context.Context, maxIdle time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	toClose := []string{}

	for id, entry := range m.connections {
		entry.mu.RLock()
		if entry.connection.Status == domain.ConnectionIdle &&
			now.Sub(entry.connection.LastActivity) > maxIdle {
			toClose = append(toClose, id)
		}
		entry.mu.RUnlock()
	}

	// Close idle connections
	closedCount := 0
	for _, id := range toClose {
		if entry, exists := m.connections[id]; exists {
			entry.mu.Lock()
			entry.connection.Status = domain.ConnectionClosed
			entry.mu.Unlock()

			delete(m.connections, id)
			m.activeCount.Add(-1)
			closedCount++
		}
	}

	// Log cleanup
	if m.logger != nil && closedCount > 0 {
		m.logger.Info(ctx, fmt.Sprintf("cleaned up %d idle connections", closedCount))
	}

	// Publish event
	if m.eventPublisher != nil && closedCount > 0 {
		event := &domain.Event{
			ID:        fmt.Sprintf("conn-cleanup-%d", time.Now().Unix()),
			Type:      domain.EventTypeConnection,
			Severity:  domain.SeverityInfo,
			Source:    "connection_manager",
			Message:   fmt.Sprintf("cleaned up %d idle connections", closedCount),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"closed_count": closedCount,
				"max_idle":     maxIdle.String(),
			},
			Context: ctx,
		}
		m.eventPublisher.PublishEvent(ctx, event)
	}

	return nil
}

// UpdateConnectionActivity updates connection activity timestamp
func (m *ConnectionManager) UpdateConnectionActivity(ctx context.Context, connectionID string) error {
	m.mu.RLock()
	entry, exists := m.connections[connectionID]
	m.mu.RUnlock()

	if !exists {
		return domain.ErrResourceNotFound(fmt.Sprintf("connection %s not found", connectionID))
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.connection.LastActivity = time.Now()
	entry.lastPing = time.Now()

	if entry.connection.Status == domain.ConnectionIdle {
		entry.connection.Status = domain.ConnectionActive
	}

	return nil
}

// GetStatistics returns connection statistics
func (m *ConnectionManager) GetStatistics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statusCounts := make(map[domain.ConnectionStatus]int)
	protocolCounts := make(map[string]int)

	for _, entry := range m.connections {
		entry.mu.RLock()
		statusCounts[entry.connection.Status]++
		protocolCounts[entry.connection.Protocol]++
		entry.mu.RUnlock()
	}

	return map[string]interface{}{
		"total_connections":   m.totalCount.Load(),
		"active_connections":  m.activeCount.Load(),
		"max_connections":     m.maxConnections,
		"status_breakdown":    statusCounts,
		"protocol_breakdown":  protocolCounts,
		"utilization_percent": float64(m.activeCount.Load()) / float64(m.maxConnections) * 100,
	}
}
