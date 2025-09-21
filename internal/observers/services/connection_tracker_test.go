package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

// TestNewConnectionTracker tests ConnectionTracker creation
func TestNewConnectionTracker(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		ConnectionTableSize: 1000,
		ConnectionTimeout:   time.Minute,
		BufferSize:          100,
		CleanupInterval:     time.Second,
	}

	tracker := NewConnectionTracker(config, logger)
	assert.NotNil(t, tracker)
	assert.NotNil(t, tracker.config)
	assert.NotNil(t, tracker.logger)
	assert.NotNil(t, tracker.connections)
	assert.NotNil(t, tracker.eventCh)
	assert.NotNil(t, tracker.stopCh)
	assert.Equal(t, 100, cap(tracker.eventCh))
}

// TestConnectionTrackerLifecycle tests Start and Stop
func TestConnectionTrackerLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		ConnectionTableSize: 100,
		ConnectionTimeout:   time.Minute,
		BufferSize:          10,
		CleanupInterval:     100 * time.Millisecond,
	}

	tracker := NewConnectionTracker(config, logger)
	ctx := context.Background()

	// Start tracker
	err := tracker.Start(ctx)
	assert.NoError(t, err)

	// Give goroutines time to start
	time.Sleep(50 * time.Millisecond)

	// Stop tracker
	err = tracker.Stop()
	assert.NoError(t, err)

	// Verify channels are closed
	select {
	case <-tracker.stopCh:
		// Expected - channel should be closed
	default:
		t.Fatal("stopCh should be closed")
	}
}

// TestConnectionTrackerEvents tests event channel
func TestConnectionTrackerEvents(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	tracker := NewConnectionTracker(config, logger)
	eventCh := tracker.Events()
	assert.NotNil(t, eventCh)

	// Send test event
	testEvent := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionConnect,
		PID:       1234,
	}

	tracker.eventCh <- testEvent

	// Receive event
	select {
	case event := <-eventCh:
		assert.Equal(t, testEvent, event)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestConnectionTrackerHandleConnectionEvent tests connection event handling
func TestConnectionTrackerHandleConnectionEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	tracker := NewConnectionTracker(config, logger)

	// Test connection start
	connectEvent := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionConnect,
		PID:       1234,
		SrcPort:   8080,
		DstPort:   3306,
	}
	copy(connectEvent.SrcIP[:], []byte("10.0.0.1"))
	copy(connectEvent.DstIP[:], []byte("10.0.0.2"))
	copy(connectEvent.Comm[:], []byte("test-app"))

	tracker.handleConnectionEvent(connectEvent)

	// Verify connection was tracked
	assert.Equal(t, uint64(1), tracker.stats.TotalConnects)
	assert.Equal(t, uint64(1), tracker.stats.ActiveConnections)
	assert.Equal(t, 1, len(tracker.connections))

	// Test connection close
	closeEvent := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionClose,
		PID:       1234,
		SrcPort:   8080,
		DstPort:   3306,
	}
	copy(closeEvent.SrcIP[:], connectEvent.SrcIP[:])
	copy(closeEvent.DstIP[:], connectEvent.DstIP[:])

	tracker.handleConnectionEvent(closeEvent)

	// Verify connection was removed
	assert.Equal(t, uint64(1), tracker.stats.TotalCloses)
	assert.Equal(t, uint64(0), tracker.stats.ActiveConnections)
	assert.Equal(t, 0, len(tracker.connections))
}

// TestConnectionTrackerHandleConnectionStart tests handleConnectionStart
func TestConnectionTrackerHandleConnectionStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	tracker := NewConnectionTracker(config, logger)

	event := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionAccept,
		PID:       5678,
		SrcPort:   9090,
		DstPort:   443,
		CgroupID:  12345,
		NetNS:     67890,
	}
	copy(event.SrcIP[:], []byte("192.168.1.1"))
	copy(event.DstIP[:], []byte("192.168.1.2"))
	copy(event.Comm[:], []byte("nginx"))

	key := ConnectionKey{
		SrcIP:   event.GetSrcIPString(),
		DstIP:   event.GetDstIPString(),
		SrcPort: event.SrcPort,
		DstPort: event.DstPort,
		PID:     event.PID,
	}

	tracker.handleConnectionStart(key, event)

	// Verify connection was added
	assert.Equal(t, 1, len(tracker.connections))
	assert.Equal(t, uint64(1), tracker.stats.TotalAccepts)
	assert.Equal(t, uint64(1), tracker.stats.ActiveConnections)

	conn := tracker.connections[key]
	assert.NotNil(t, conn)
	assert.Equal(t, key, conn.Key)
	assert.Equal(t, StateActive, conn.State)
	assert.Equal(t, "nginx", conn.ProcessName)
	assert.Equal(t, uint64(12345), conn.CgroupID)
	assert.Equal(t, uint32(67890), conn.NetNS)
}

// TestConnectionTrackerHandleConnectionClose tests handleConnectionClose
func TestConnectionTrackerHandleConnectionClose(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	tracker := NewConnectionTracker(config, logger)

	// Add a connection first
	key := ConnectionKey{
		SrcIP:   "10.0.0.1",
		DstIP:   "10.0.0.2",
		SrcPort: 8080,
		DstPort: 3306,
		PID:     1234,
	}
	tracker.connections[key] = &ActiveConnection{
		Key:       key,
		StartTime: time.Now(),
		LastSeen:  time.Now(),
		State:     StateActive,
	}
	tracker.stats.ActiveConnections = 1

	// Create close event
	event := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionClose,
	}

	tracker.handleConnectionClose(key, event)

	// Verify connection was removed
	assert.Equal(t, 0, len(tracker.connections))
	assert.Equal(t, uint64(1), tracker.stats.TotalCloses)
	assert.Equal(t, uint64(0), tracker.stats.ActiveConnections)
}

// TestConnectionTrackerCleanup tests connection cleanup
func TestConnectionTrackerCleanup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		ConnectionTableSize: 100,
		ConnectionTimeout:   100 * time.Millisecond, // Short timeout for testing
		BufferSize:          10,
		CleanupInterval:     50 * time.Millisecond,
	}

	tracker := NewConnectionTracker(config, logger)

	// Add stale connection
	staleKey := ConnectionKey{
		SrcIP:   "10.0.0.1",
		DstIP:   "10.0.0.2",
		SrcPort: 8080,
		DstPort: 3306,
		PID:     1234,
	}
	tracker.connections[staleKey] = &ActiveConnection{
		Key:       staleKey,
		StartTime: time.Now().Add(-time.Hour), // Old connection
		LastSeen:  time.Now().Add(-time.Hour),
		State:     StateActive,
	}

	// Add fresh connection
	freshKey := ConnectionKey{
		SrcIP:   "10.0.0.3",
		DstIP:   "10.0.0.4",
		SrcPort: 9090,
		DstPort: 443,
		PID:     5678,
	}
	tracker.connections[freshKey] = &ActiveConnection{
		Key:       freshKey,
		StartTime: time.Now(),
		LastSeen:  time.Now(),
		State:     StateActive,
	}

	tracker.stats.ActiveConnections = 2

	// Run cleanup
	tracker.doCleanup()

	// Verify only fresh connection remains
	assert.Equal(t, 1, len(tracker.connections))
	assert.Contains(t, tracker.connections, freshKey)
	assert.NotContains(t, tracker.connections, staleKey)
	assert.Equal(t, uint64(1), tracker.stats.ActiveConnections)
}

// TestConnectionTrackerGetActiveConnections tests GetActiveConnections
func TestConnectionTrackerGetActiveConnections(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	tracker := NewConnectionTracker(config, logger)

	// Add some connections
	key1 := ConnectionKey{
		SrcIP:   "10.0.0.1",
		DstIP:   "10.0.0.2",
		SrcPort: 8080,
		DstPort: 3306,
		PID:     1234,
	}
	conn1 := &ActiveConnection{
		Key:       key1,
		StartTime: time.Now(),
		LastSeen:  time.Now(),
		State:     StateActive,
	}

	key2 := ConnectionKey{
		SrcIP:   "10.0.0.3",
		DstIP:   "10.0.0.4",
		SrcPort: 9090,
		DstPort: 443,
		PID:     5678,
	}
	conn2 := &ActiveConnection{
		Key:       key2,
		StartTime: time.Now(),
		LastSeen:  time.Now(),
		State:     StateActive,
	}

	tracker.connections[key1] = conn1
	tracker.connections[key2] = conn2

	// Get active connections
	active := tracker.GetActiveConnections()
	assert.Equal(t, 2, len(active))
	assert.Contains(t, active, key1)
	assert.Contains(t, active, key2)

	// Verify it's a copy (modifying returned map shouldn't affect original)
	delete(active, key1)
	assert.Equal(t, 2, len(tracker.connections))
}

// TestConnectionTrackerGetStats tests GetStats
func TestConnectionTrackerGetStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	tracker := NewConnectionTracker(config, logger)

	// Set some stats
	tracker.stats = ConnectionStats{
		ActiveConnections: 5,
		TotalConnects:     100,
		TotalAccepts:      50,
		TotalCloses:       145,
		LastEventTime:     time.Now(),
	}

	stats := tracker.GetStats()
	assert.Equal(t, uint64(5), stats.ActiveConnections)
	assert.Equal(t, uint64(100), stats.TotalConnects)
	assert.Equal(t, uint64(50), stats.TotalAccepts)
	assert.Equal(t, uint64(145), stats.TotalCloses)
}

// TestConnectionEventMethods tests ConnectionEvent helper methods
func TestConnectionEventMethods(t *testing.T) {
	event := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionConnect,
		PID:       1234,
		Family:    2, // AF_INET
	}
	// Set IPv4 addresses properly
	event.SrcIP = [16]byte{192, 168, 1, 1}
	event.DstIP = [16]byte{10, 0, 0, 2}
	copy(event.Comm[:], []byte("test-app"))

	// Test GetTimestamp
	ts := event.GetTimestamp()
	assert.NotZero(t, ts)

	// Test GetSrcIPString
	srcIP := event.GetSrcIPString()
	assert.Contains(t, srcIP, "192.168.1.1")

	// Test GetDstIPString
	dstIP := event.GetDstIPString()
	assert.Contains(t, dstIP, "10.0.0.2")

	// Test GetComm
	comm := event.GetComm()
	assert.Equal(t, "test-app", comm)

	// Test String method for EventType
	assert.Equal(t, "connect", event.EventType.String())
	assert.Equal(t, "accept", ConnectionAccept.String())
	assert.Equal(t, "close", ConnectionClose.String())
	assert.Equal(t, "unknown", ConnectionEventType(99).String())
}

// TestConnectionKey tests ConnectionKey methods
func TestConnectionKey(t *testing.T) {
	key := ConnectionKey{
		SrcIP:   "10.0.0.1",
		DstIP:   "10.0.0.2",
		SrcPort: 8080,
		DstPort: 3306,
		PID:     1234,
	}

	// Test String method
	str := key.String()
	assert.Contains(t, str, "10.0.0.1")
	assert.Contains(t, str, "8080")
	assert.Contains(t, str, "10.0.0.2")
	assert.Contains(t, str, "3306")
	assert.Contains(t, str, "1234")
}

// TestProcessEvents tests processEvents goroutine
func TestProcessEvents(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	tracker := NewConnectionTracker(config, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start processing in background
	go tracker.processEvents(ctx)

	// Send test event
	event := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionConnect,
		PID:       1234,
		SrcPort:   8080,
		DstPort:   3306,
	}
	copy(event.SrcIP[:], []byte("10.0.0.1"))
	copy(event.DstIP[:], []byte("10.0.0.2"))

	tracker.eventCh <- event

	// Give time to process
	time.Sleep(100 * time.Millisecond)

	// Verify event was processed
	assert.Equal(t, uint64(1), tracker.stats.TotalConnects)
	assert.Equal(t, 1, len(tracker.connections))
}

// BenchmarkHandleConnectionEvent benchmarks connection event handling
func BenchmarkHandleConnectionEvent(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConfig()
	tracker := NewConnectionTracker(config, logger)

	event := &ConnectionEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		EventType: ConnectionConnect,
		PID:       1234,
		SrcPort:   8080,
		DstPort:   3306,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event.PID = uint32(i) // Make each connection unique
		tracker.handleConnectionEvent(event)
	}
}
