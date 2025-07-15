package grpc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
)

// Connection represents a client connection with state tracking
type Connection struct {
	ID            string
	CollectorID   string
	CollectorType string

	// Connection metadata
	ConnectedAt   time.Time
	LastActivity  time.Time
	LastHeartbeat time.Time

	// Flow control state
	requestedRate     uint32
	bufferUtilization float32
	memoryPressure    MemoryPressure

	// Statistics
	eventsReceived    uint64
	bytesReceived     uint64
	batchesReceived   uint64
	avgProcessingTime int64 // nanoseconds

	// Communication channels
	ResponseChan chan *StreamResponse

	// Sequence tracking
	sequence uint64

	// State
	status *CollectorStatus

	// Synchronization
	mu sync.RWMutex

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// ConnectionManager manages all active connections
type ConnectionManager struct {
	connections map[string]*Connection
	mu          sync.RWMutex
	config      ServerConfig

	// Statistics
	totalConnections  uint64
	activeConnections uint64
	connectionsByType map[string]uint64

	// Connection pool for reuse
	connPool sync.Pool
}

// ConnectionStats provides connection statistics
type ConnectionStats struct {
	TotalConnections  uint64
	ActiveConnections uint64
	ConnectionsByType map[string]uint64
	AvgConnDuration   time.Duration
	ConnectionErrors  uint64
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager(config ServerConfig) *ConnectionManager {
	return &ConnectionManager{
		connections:       make(map[string]*Connection),
		config:            config,
		connectionsByType: make(map[string]uint64),
		connPool: sync.Pool{
			New: func() interface{} {
				return &Connection{
					ResponseChan: make(chan *StreamResponse, 100),
				}
			},
		},
	}
}

// NewConnection creates a new connection
func (cm *ConnectionManager) NewConnection(ctx context.Context) *Connection {
	conn := cm.connPool.Get().(*Connection)

	// Reset connection state
	*conn = Connection{
		ID:           generateConnectionID(),
		ConnectedAt:  time.Now(),
		LastActivity: time.Now(),
		ResponseChan: make(chan *StreamResponse, 100),
		sequence:     0,
	}

	conn.ctx, conn.cancel = context.WithCancel(ctx)

	cm.mu.Lock()
	cm.connections[conn.ID] = conn
	atomic.AddUint64(&cm.totalConnections, 1)
	atomic.AddUint64(&cm.activeConnections, 1)
	cm.mu.Unlock()

	return conn
}

// CloseConnection closes and cleans up a connection
func (cm *ConnectionManager) CloseConnection(connectionID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conn, exists := cm.connections[connectionID]
	if !exists {
		return
	}

	// Cancel context
	conn.cancel()

	// Close response channel
	close(conn.ResponseChan)

	// Remove from active connections
	delete(cm.connections, connectionID)
	atomic.AddUint64(&cm.activeConnections, ^uint64(0)) // Subtract 1

	// Update type counters
	if conn.CollectorType != "" {
		cm.connectionsByType[conn.CollectorType]--
		if cm.connectionsByType[conn.CollectorType] == 0 {
			delete(cm.connectionsByType, conn.CollectorType)
		}
	}

	// Return to pool
	cm.connPool.Put(conn)
}

// GetConnection retrieves a connection by collector ID
func (cm *ConnectionManager) GetConnection(collectorID string) *Connection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for _, conn := range cm.connections {
		if conn.CollectorID == collectorID {
			return conn
		}
	}

	return nil
}

// GetConnectionByID retrieves a connection by connection ID
func (cm *ConnectionManager) GetConnectionByID(connectionID string) *Connection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.connections[connectionID]
}

// RegisterCollector registers a new collector
func (cm *ConnectionManager) RegisterCollector(reg *CollectorRegistration) *CollectorConfig {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Update type counters
	cm.connectionsByType[reg.CollectorType]++

	// Create configuration
	config := &CollectorConfig{
		CollectorId:   reg.CollectorId,
		ConfigVersion: "1.0",
		FlowControl: &FlowControlDirective{
			MaxEventsPerSecond: cm.config.DefaultEventsPerSec,
			MaxBatchSize:       cm.config.MaxBatchSize,
			BatchInterval:      durationpb.New(time.Second),
			EnableCompression:  true,
			CompressionType:    CompressionType_COMPRESSION_LZ4,
			ValidDuration:      durationpb.New(time.Hour),
		},
		Quality: &QualitySettings{
			MinConfidence:   0.8,
			RequiredContext: 0.9,
			MaxLatency:      durationpb.New(10 * time.Millisecond),
		},
		HeartbeatInterval: durationpb.New(30 * time.Second),
		ConfigTtl:         durationpb.New(time.Hour),
	}

	return config
}

// CleanupIdleConnections removes idle connections
func (cm *ConnectionManager) CleanupIdleConnections() {
	now := time.Now()
	maxIdle := cm.config.MaxConnectionIdle

	cm.mu.Lock()
	defer cm.mu.Unlock()

	for id, conn := range cm.connections {
		if now.Sub(conn.LastActivity) > maxIdle {
			// Close idle connection
			conn.cancel()
			close(conn.ResponseChan)
			delete(cm.connections, id)
			atomic.AddUint64(&cm.activeConnections, ^uint64(0))

			// Update type counters
			if conn.CollectorType != "" {
				cm.connectionsByType[conn.CollectorType]--
				if cm.connectionsByType[conn.CollectorType] == 0 {
					delete(cm.connectionsByType, conn.CollectorType)
				}
			}
		}
	}
}

// GetStats returns connection statistics
func (cm *ConnectionManager) GetStats() ConnectionStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Calculate average connection duration
	var totalDuration time.Duration
	activeCount := len(cm.connections)
	now := time.Now()

	for _, conn := range cm.connections {
		totalDuration += now.Sub(conn.ConnectedAt)
	}

	var avgDuration time.Duration
	if activeCount > 0 {
		avgDuration = totalDuration / time.Duration(activeCount)
	}

	// Copy type counters
	typeCounters := make(map[string]uint64)
	for k, v := range cm.connectionsByType {
		typeCounters[k] = v
	}

	return ConnectionStats{
		TotalConnections:  atomic.LoadUint64(&cm.totalConnections),
		ActiveConnections: atomic.LoadUint64(&cm.activeConnections),
		ConnectionsByType: typeCounters,
		AvgConnDuration:   avgDuration,
	}
}

// Connection methods

// NextSequence returns the next sequence number
func (c *Connection) NextSequence() uint64 {
	return atomic.AddUint64(&c.sequence, 1)
}

// UpdateStats updates connection statistics
func (c *Connection) UpdateStats(events uint64, processingTime time.Duration) {
	atomic.AddUint64(&c.eventsReceived, events)
	atomic.AddUint64(&c.batchesReceived, 1)

	// Update average processing time using exponential moving average
	newAvg := processingTime.Nanoseconds()
	for {
		old := atomic.LoadInt64(&c.avgProcessingTime)
		// EMA with alpha = 0.1
		updated := old + (newAvg-old)/10
		if atomic.CompareAndSwapInt64(&c.avgProcessingTime, old, updated) {
			break
		}
	}

	c.mu.Lock()
	c.LastActivity = time.Now()
	c.mu.Unlock()
}

// UpdateHeartbeat updates the last heartbeat time
func (c *Connection) UpdateHeartbeat(timestamp time.Time) {
	c.mu.Lock()
	c.LastHeartbeat = timestamp
	c.LastActivity = time.Now()
	c.mu.Unlock()
}

// UpdateStatus updates the collector status
func (c *Connection) UpdateStatus(status *CollectorStatus) {
	c.mu.Lock()
	c.status = status
	c.LastActivity = time.Now()
	c.mu.Unlock()
}

// SetRequestedRate sets the requested events per second
func (c *Connection) SetRequestedRate(rate uint32) {
	atomic.StoreUint32(&c.requestedRate, rate)
}

// GetRequestedRate gets the requested events per second
func (c *Connection) GetRequestedRate() uint32 {
	return atomic.LoadUint32(&c.requestedRate)
}

// SetBufferUtilization sets the buffer utilization
func (c *Connection) SetBufferUtilization(utilization float32) {
	c.mu.Lock()
	c.bufferUtilization = utilization
	c.mu.Unlock()
}

// GetBufferUtilization gets the buffer utilization
func (c *Connection) GetBufferUtilization() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.bufferUtilization
}

// SetMemoryPressure sets the memory pressure level
func (c *Connection) SetMemoryPressure(pressure MemoryPressure) {
	atomic.StoreInt32((*int32)(&c.memoryPressure), int32(pressure))
}

// GetMemoryPressure gets the memory pressure level
func (c *Connection) GetMemoryPressure() MemoryPressure {
	return MemoryPressure(atomic.LoadInt32((*int32)(&c.memoryPressure)))
}

// GetStats returns connection statistics
func (c *Connection) GetStats() ConnectionStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return ConnectionStats{
		TotalConnections:  atomic.LoadUint64(&c.eventsReceived),
		ActiveConnections: atomic.LoadUint64(&c.batchesReceived),
		AvgConnDuration:   time.Duration(atomic.LoadInt64(&c.avgProcessingTime)),
	}
}

// ProcessAck processes an acknowledgment message
func (c *Connection) ProcessAck(ack *AckMessage) {
	// Implementation for processing acknowledgments
	// This could involve removing items from pending queues, updating metrics, etc.
	c.mu.Lock()
	c.LastActivity = time.Now()
	c.mu.Unlock()
}

// IsHealthy checks if the connection is healthy
func (c *Connection) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()

	// Check if heartbeat is recent (within 2 minutes)
	if !c.LastHeartbeat.IsZero() && now.Sub(c.LastHeartbeat) > 2*time.Minute {
		return false
	}

	// Check if there's recent activity (within 5 minutes)
	if now.Sub(c.LastActivity) > 5*time.Minute {
		return false
	}

	return true
}

// Utility functions

func generateConnectionID() string {
	// Implementation would generate a unique connection ID
	// For now, return a timestamp-based ID
	return fmt.Sprintf("conn_%d", time.Now().UnixNano())
}
