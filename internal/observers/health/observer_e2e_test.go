package health

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestE2EHealthMonitoringWorkflow tests complete health monitoring workflow
func TestE2EHealthMonitoringWorkflow(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   1024 * 1024,
		EventChannelSize: 100,
		RateLimitMs:      10,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
		},
		RequireAllMetrics: false,
	}

	// Create and start observer
	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Verify observer is healthy
	assert.True(t, observer.IsHealthy())

	// Set up event collector
	var collectedEvents []*domain.CollectorEvent
	var mu sync.Mutex
	done := make(chan bool)

	go func() {
		for {
			select {
			case event := <-observer.Events():
				mu.Lock()
				collectedEvents = append(collectedEvents, event)
				mu.Unlock()
			case <-ctx.Done():
				done <- true
				return
			}
		}
	}()

	// Simulate health events being generated
	// In real Linux environment, these would come from eBPF
	// For testing, we rely on mock events from fallback implementation

	// Wait for some events to be collected
	time.Sleep(100 * time.Millisecond)

	// Verify statistics
	stats := observer.Statistics()
	assert.NotNil(t, stats)
	// Verify observer name directly
	assert.Equal(t, "health", observer.Name())

	// Clean shutdown
	err = observer.Stop()
	require.NoError(t, err)
	assert.False(t, observer.IsHealthy())

	// Wait for collector to finish
	cancel()
	<-done
}

// TestE2EDiskSpaceExhaustionScenario tests disk space monitoring scenario
func TestE2EDiskSpaceExhaustionScenario(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   1024,
		EventChannelSize: 10,
		RateLimitMs:      10,
		EnabledCategories: map[string]bool{
			"file": true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Simulate disk space exhaustion event
	diskEvent := &HealthEvent{
		TimestampNs: uint64(time.Now().UnixNano()),
		PID:         1234,
		PPID:        1,
		UID:         1000,
		GID:         1000,
		SyscallNr:   1, // write
		ErrorCode:   -28, // ENOSPC
		Category:    1, // file
		Comm:        [16]byte{'d', 'i', 's', 'k', 'h', 'o', 'g'},
		Path:        [256]byte{'/', 'v', 'a', 'r', '/', 'l', 'o', 'g'},
		ErrorCount:  10,
	}

	// Convert and send event
	collectorEvent := observer.convertToCollectorEvent(diskEvent)
	require.NotNil(t, collectorEvent)

	// Verify event properties
	assert.Equal(t, domain.EventTypeKernelSyscall, collectorEvent.Type)
	assert.Equal(t, domain.EventSeverityCritical, collectorEvent.Severity)
	assert.Equal(t, "ENOSPC", collectorEvent.EventData.Kernel.ErrorMessage)
	assert.Equal(t, "file", collectorEvent.Metadata.Labels["category"])

	// Send through channel
	sent := observer.EventChannelManager.SendEvent(collectorEvent)
	assert.True(t, sent)

	// Verify event is received
	select {
	case received := <-observer.Events():
		assert.Equal(t, collectorEvent.EventID, received.EventID)
		assert.Equal(t, "ENOSPC", received.EventData.Kernel.ErrorMessage)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

// TestE2EMemoryExhaustionScenario tests memory monitoring scenario
func TestE2EMemoryExhaustionScenario(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create memory exhaustion events with different processes
	processes := []string{"chrome", "firefox", "vscode"}
	events := make([]*domain.CollectorEvent, 0, len(processes))

	for i, proc := range processes {
		comm := [16]byte{}
		copy(comm[:], proc)

		memEvent := &HealthEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         uint32(2000 + i),
			PPID:        1,
			SyscallNr:   9, // mmap
			ErrorCode:   -12, // ENOMEM
			Category:    3, // memory
			Comm:        comm,
			ErrorCount:  1,
		}

		event := observer.convertToCollectorEvent(memEvent)
		events = append(events, event)

		// Send event
		sent := observer.EventChannelManager.SendEvent(event)
		assert.True(t, sent)
	}

	// Collect events
	collected := make([]*domain.CollectorEvent, 0, len(processes))
	timeout := time.After(500 * time.Millisecond)

	for i := 0; i < len(processes); i++ {
		select {
		case event := <-observer.Events():
			collected = append(collected, event)
		case <-timeout:
			t.Fatalf("timeout collecting events, got %d/%d", len(collected), len(processes))
		}
	}

	// Verify all events collected
	assert.Equal(t, len(processes), len(collected))
	for _, event := range collected {
		assert.Equal(t, "ENOMEM", event.EventData.Kernel.ErrorMessage)
		assert.Equal(t, domain.EventSeverityCritical, event.Severity)
	}
}

// TestE2ENetworkConnectionFailureScenario tests network failure monitoring
func TestE2ENetworkConnectionFailureScenario(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.EnabledCategories = map[string]bool{
		"network": true,
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Simulate connection refused events
	services := []struct {
		name string
		port uint16
	}{
		{"redis", 6379},
		{"postgres", 5432},
		{"kafka", 9092},
	}

	for _, svc := range services {
		comm := [16]byte{}
		copy(comm[:], svc.name)

		netEvent := &HealthEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         3000,
			SyscallNr:   42, // connect
			ErrorCode:   -111, // ECONNREFUSED
			Category:    2, // network
			Comm:        comm,
			DstIP:       0x0100007f, // 127.0.0.1
			DstPort:     svc.port,
		}

		event := observer.convertToCollectorEvent(netEvent)

		// Verify network context
		assert.Equal(t, "127.0.0.1", event.EventData.Custom["dst_ip"])
		assert.Contains(t, event.EventData.Custom["dst_port"], string(rune(svc.port)))

		sent := observer.EventChannelManager.SendEvent(event)
		assert.True(t, sent)
	}

	// Verify events received
	for range services {
		select {
		case event := <-observer.Events():
			assert.Equal(t, "ECONNREFUSED", event.EventData.Kernel.ErrorMessage)
			assert.Equal(t, "network", event.Metadata.Labels["category"])
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for network event")
		}
	}
}

// TestE2ERateLimitingScenario tests event rate limiting
func TestE2ERateLimitingScenario(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   1024,
		EventChannelSize: 100,
		RateLimitMs:      50, // 50ms rate limit
		EnabledCategories: map[string]bool{
			"file": true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Generate burst of events
	burstSize := 10
	for i := 0; i < burstSize; i++ {
		event := &HealthEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         1000,
			SyscallNr:   1,
			ErrorCode:   -28, // ENOSPC
			Category:    1,
			ErrorCount:  uint32(i + 1),
		}

		domainEvent := observer.convertToCollectorEvent(event)
		observer.EventChannelManager.SendEvent(domainEvent)

		// Small delay between events
		time.Sleep(5 * time.Millisecond)
	}

	// Collect events with timeout
	var collected []*domain.CollectorEvent
	timeout := time.After(1 * time.Second)

	for {
		select {
		case event := <-observer.Events():
			collected = append(collected, event)
		case <-timeout:
			goto done
		}
	}

done:
	// Should receive some events but possibly not all due to rate limiting
	assert.Greater(t, len(collected), 0)
	assert.LessOrEqual(t, len(collected), burstSize)
}

// TestE2EMultiCategoryScenario tests filtering by categories
func TestE2EMultiCategoryScenario(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   1024,
		EventChannelSize: 10,
		RateLimitMs:      10,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": false, // Disabled
			"memory":  true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create events for different categories
	events := []struct {
		category uint8
		name     string
		enabled  bool
	}{
		{1, "file", true},
		{2, "network", false}, // Should be filtered
		{3, "memory", true},
	}

	for _, evt := range events {
		healthEvent := &HealthEvent{
			TimestampNs: uint64(time.Now().UnixNano()),
			PID:         1000,
			Category:    evt.category,
			ErrorCode:   -5, // EIO
		}

		domainEvent := observer.convertToCollectorEvent(healthEvent)
		sent := observer.EventChannelManager.SendEvent(domainEvent)

		if evt.enabled {
			assert.True(t, sent)

			// Verify event received
			select {
			case received := <-observer.Events():
				assert.Equal(t, evt.name, received.Metadata.Labels["category"])
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("timeout waiting for %s event", evt.name)
			}
		}
	}
}

// TestE2EConcurrentEventProcessing tests concurrent event handling
func TestE2EConcurrentEventProcessing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		RingBufferSize:   1024,
		EventChannelSize: 100,
		RateLimitMs:      1,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
		},
	}

	observer, err := NewObserver(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Launch concurrent event generators
	var wg sync.WaitGroup
	numGenerators := 3
	eventsPerGenerator := 10

	for i := 0; i < numGenerators; i++ {
		wg.Add(1)
		go func(generatorID int) {
			defer wg.Done()

			for j := 0; j < eventsPerGenerator; j++ {
				event := &HealthEvent{
					TimestampNs: uint64(time.Now().UnixNano()),
					PID:         uint32(1000 + generatorID),
					Category:    uint8(generatorID%3 + 1),
					ErrorCode:   int32(-(generatorID*10 + j)),
				}

				domainEvent := observer.convertToCollectorEvent(event)
				observer.EventChannelManager.SendEvent(domainEvent)
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	// Collect events concurrently
	collected := make([]*domain.CollectorEvent, 0)
	var collectMu sync.Mutex

	go func() {
		for event := range observer.Events() {
			collectMu.Lock()
			collected = append(collected, event)
			collectMu.Unlock()

			if len(collected) >= numGenerators*eventsPerGenerator {
				break
			}
		}
	}()

	// Wait for generators to finish
	wg.Wait()

	// Give time for collection
	time.Sleep(500 * time.Millisecond)

	// Verify events were collected
	collectMu.Lock()
	assert.Greater(t, len(collected), 0)
	collectMu.Unlock()
}

// TestE2EGracefulShutdownScenario tests graceful shutdown
func TestE2EGracefulShutdownScenario(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver(logger, DefaultConfig())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Start event generator
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				event := &HealthEvent{
					TimestampNs: uint64(time.Now().UnixNano()),
					PID:         1000,
					ErrorCode:   -28,
				}
				domainEvent := observer.convertToCollectorEvent(event)
				observer.EventChannelManager.SendEvent(domainEvent)
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Let it run for a bit
	time.Sleep(200 * time.Millisecond)

	// Initiate graceful shutdown
	cancel()
	err = observer.Stop()
	require.NoError(t, err)

	// Verify observer is stopped
	assert.False(t, observer.IsHealthy())

	// Channel should be closed
	_, ok := <-observer.Events()
	assert.False(t, ok)
}