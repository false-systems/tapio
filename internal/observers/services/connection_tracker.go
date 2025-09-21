package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ConnectionTracker implements raw TCP connection tracking via eBPF
type ConnectionTracker struct {
	config *Config
	logger *zap.Logger

	// Connection state
	mu          sync.RWMutex
	connections map[ConnectionKey]*ActiveConnection
	stats       ConnectionStats

	// eBPF state (platform-specific)
	ebpfState interface{}

	// Event processing
	eventCh chan *ConnectionEvent
	stopCh  chan struct{}
}

// NewConnectionTracker creates a new connection tracking connection tracker
func NewConnectionTracker(config *Config, logger *zap.Logger) *ConnectionTracker {
	return &ConnectionTracker{
		config:      config,
		logger:      logger.Named("connections"),
		connections: make(map[ConnectionKey]*ActiveConnection),
		eventCh:     make(chan *ConnectionEvent, config.BufferSize),
		stopCh:      make(chan struct{}),
	}
}

// Start begins connection tracking
func (t *ConnectionTracker) Start(ctx context.Context) error {
	t.logger.Info("Starting connection tracking connection tracker")

	// Start eBPF tracking
	if err := t.startEBPF(); err != nil {
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start event processor
	go t.processEvents(ctx)

	// Start cleanup routine
	go t.cleanupConnections(ctx)

	t.logger.Info("connection tracking connection tracker started")
	return nil
}

// Stop stops the connection tracker
func (t *ConnectionTracker) Stop() error {
	t.logger.Info("Stopping connection tracking connection tracker")

	close(t.stopCh)
	t.stopEBPF()

	t.logger.Info("connection tracking connection tracker stopped")
	return nil
}

// GetActiveConnections returns current active connections
func (t *ConnectionTracker) GetActiveConnections() map[ConnectionKey]*ActiveConnection {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[ConnectionKey]*ActiveConnection)
	for k, v := range t.connections {
		result[k] = v
	}
	return result
}

// GetStats returns connection statistics
func (t *ConnectionTracker) GetStats() ConnectionStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.stats
}

// Events returns the event channel for K8s enrichment processing
func (t *ConnectionTracker) Events() <-chan *ConnectionEvent {
	return t.eventCh
}

// processEvents handles incoming connection events
func (t *ConnectionTracker) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.stopCh:
			return
		case event := <-t.eventCh:
			t.handleConnectionEvent(event)
		}
	}
}

// handleConnectionEvent processes a single connection event
func (t *ConnectionTracker) handleConnectionEvent(event *ConnectionEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := ConnectionKey{
		SrcIP:   event.GetSrcIPString(),
		DstIP:   event.GetDstIPString(),
		SrcPort: event.SrcPort,
		DstPort: event.DstPort,
		PID:     event.PID,
	}

	switch event.EventType {
	case ConnectionConnect, ConnectionAccept:
		t.handleConnectionStart(key, event)
	case ConnectionClose:
		t.handleConnectionClose(key, event)
	}

	t.stats.LastEventTime = event.GetTimestamp()
}

// handleConnectionStart handles new connection events
func (t *ConnectionTracker) handleConnectionStart(key ConnectionKey, event *ConnectionEvent) {
	conn := &ActiveConnection{
		Key:         key,
		StartTime:   event.GetTimestamp(),
		LastSeen:    event.GetTimestamp(),
		State:       StateActive,
		ProcessName: event.GetComm(),
		CgroupID:    event.CgroupID,
		NetNS:       event.NetNS,
	}

	t.connections[key] = conn

	if event.EventType == ConnectionConnect {
		t.stats.TotalConnects++
	} else {
		t.stats.TotalAccepts++
	}

	t.stats.ActiveConnections = uint64(len(t.connections))

	t.logger.Debug("New connection tracked",
		zap.String("key", key.String()),
		zap.String("type", event.EventType.String()),
		zap.String("process", event.GetComm()))
}

// handleConnectionClose handles connection close events
func (t *ConnectionTracker) handleConnectionClose(key ConnectionKey, event *ConnectionEvent) {
	if conn, exists := t.connections[key]; exists {
		conn.State = StateClosed
		conn.LastSeen = event.GetTimestamp()
		delete(t.connections, key)

		t.stats.TotalCloses++
		t.stats.ActiveConnections = uint64(len(t.connections))

		t.logger.Debug("Connection closed",
			zap.String("key", key.String()),
			zap.Duration("duration", conn.LastSeen.Sub(conn.StartTime)))
	}
}

// cleanupConnections removes stale connections
func (t *ConnectionTracker) cleanupConnections(ctx context.Context) {
	ticker := time.NewTicker(t.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.doCleanup()
		}
	}
}

// doCleanup performs the actual cleanup of stale connections
func (t *ConnectionTracker) doCleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.config.ConnectionTimeout)

	for key, conn := range t.connections {
		if conn.LastSeen.Before(cutoff) {
			delete(t.connections, key)
			t.logger.Debug("Cleaned up stale connection",
				zap.String("key", key.String()),
				zap.Duration("age", now.Sub(conn.LastSeen)))
		}
	}

	t.stats.ActiveConnections = uint64(len(t.connections))
}
