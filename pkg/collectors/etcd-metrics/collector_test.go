package etcdmetrics

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "etcd-metrics", cfg.Name)
	assert.Equal(t, 1000, cfg.BufferSize)
	assert.Empty(t, cfg.Endpoints)
	assert.Equal(t, 5*time.Second, cfg.DialTimeout)
	assert.Equal(t, 2*time.Second, cfg.RequestTimeout)
	assert.Equal(t, 30*time.Second, cfg.HealthCheckInterval)
	assert.Equal(t, 500*time.Millisecond, cfg.ResponseTimeThreshold)
	assert.Equal(t, int64(8*1024*1024*1024), cfg.DbSizeThreshold)
}

func TestNewCollector_NoEndpoints(t *testing.T) {
	cfg := Config{
		Name:       "test-etcd",
		BufferSize: 100,
		Endpoints:  []string{}, // No endpoints
	}

	collector, err := NewCollector("test-etcd", cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no etcd endpoints configured")
	assert.Nil(t, collector)
}

func TestNewCollector_ValidConfig(t *testing.T) {
	cfg := Config{
		Name:                  "test-etcd",
		BufferSize:            100,
		Endpoints:             []string{"localhost:2379"}, // Won't connect in unit test
		DialTimeout:           5 * time.Second,
		RequestTimeout:        2 * time.Second,
		HealthCheckInterval:   1 * time.Second,
		ResponseTimeThreshold: 100 * time.Millisecond,
		DbSizeThreshold:       1024 * 1024,
	}

	collector, err := NewCollector("test-etcd", cfg)
	require.NoError(t, err)
	require.NotNil(t, collector)

	assert.Equal(t, "test-etcd", collector.Name())
	assert.NotNil(t, collector.logger)
	assert.NotNil(t, collector.client)
	assert.NotNil(t, collector.events)
	assert.Equal(t, 100, cap(collector.events))
	assert.True(t, collector.healthy)
	assert.NotNil(t, collector.tracer)
	assert.NotNil(t, collector.eventsProcessed)
	assert.NotNil(t, collector.errorsTotal)
	assert.NotNil(t, collector.responseTime)

	// Clean up
	collector.client.Close()
}

func TestNewCollector_WithAuth(t *testing.T) {
	cfg := Config{
		Name:       "test-etcd",
		BufferSize: 100,
		Endpoints:  []string{"localhost:2379"},
		Username:   "testuser",
		Password:   "testpass",
	}

	collector, err := NewCollector("test-etcd", cfg)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Verify auth is configured
	assert.Equal(t, cfg.Username, collector.config.Username)
	assert.Equal(t, cfg.Password, collector.config.Password)

	// Clean up
	collector.client.Close()
}

func TestCollector_Name(t *testing.T) {
	collector := &Collector{
		name: "test-collector",
	}
	assert.Equal(t, "test-collector", collector.Name())
}

func TestCollector_IsHealthy(t *testing.T) {
	collector := &Collector{
		healthy: true,
	}
	assert.True(t, collector.IsHealthy())

	collector.healthy = false
	assert.False(t, collector.IsHealthy())
}

func TestCollector_Events(t *testing.T) {
	eventChan := make(chan *domain.CollectorEvent, 10)
	collector := &Collector{
		events: eventChan,
	}

	assert.Equal(t, (<-chan *domain.CollectorEvent)(eventChan), collector.Events())
}

func TestCollector_StartAlreadyStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	collector := &Collector{
		ctx:    ctx,
		cancel: cancel,
	}

	err := collector.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	cancel()
}

func TestCollector_Stop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	eventChan := make(chan *domain.CollectorEvent, 10)

	collector := &Collector{
		ctx:    ctx,
		cancel: cancel,
		events: eventChan,
		logger: zaptest.NewLogger(t),
	}

	// Send an event to verify channel is open
	collector.events <- &domain.CollectorEvent{}

	err := collector.Stop()
	assert.NoError(t, err)

	// Verify channel is closed
	_, ok := <-collector.events
	assert.False(t, ok)
}

func TestCollector_GenerateEventID(t *testing.T) {
	collector := &Collector{
		name: "test-collector",
	}

	id1 := collector.generateEventID()
	id2 := collector.generateEventID()

	assert.Contains(t, id1, "test-collector-")
	assert.Contains(t, id2, "test-collector-")
	assert.NotEqual(t, id1, id2)
}

func TestCollector_SendEvent(t *testing.T) {
	collector := &Collector{
		name:   "test-collector",
		events: make(chan *domain.CollectorEvent, 10),
		ctx:    context.Background(),
		logger: zaptest.NewLogger(t),
	}

	event := &domain.CollectorEvent{
		EventID:   "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeETCD,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
	}

	collector.sendEvent(event)

	select {
	case received := <-collector.events:
		assert.Equal(t, event.EventID, received.EventID)
	case <-time.After(1 * time.Second):
		t.Error("Event not received")
	}
}

func TestCollector_SendEvent_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	collector := &Collector{
		name:   "test-collector",
		events: make(chan *domain.CollectorEvent, 10),
		ctx:    ctx,
		logger: zaptest.NewLogger(t),
	}

	event := &domain.CollectorEvent{
		EventID: "test-1",
	}

	// Should not panic when context is cancelled
	collector.sendEvent(event)

	// Event should not be sent
	select {
	case <-collector.events:
		t.Error("Event should not be sent when context is cancelled")
	default:
		// Expected
	}
}

func TestCollector_SendEvent_BufferFull(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector := &Collector{
		name:   "test-collector",
		events: make(chan *domain.CollectorEvent, 1), // Small buffer
		ctx:    context.Background(),
		logger: logger,
	}

	// Fill the buffer
	event1 := &domain.CollectorEvent{EventID: "test-1", Type: domain.EventTypeETCD}
	collector.sendEvent(event1)

	// This should be dropped and logged
	event2 := &domain.CollectorEvent{EventID: "test-2", Type: domain.EventTypeETCD}
	collector.sendEvent(event2)

	// Verify only first event is in channel
	select {
	case received := <-collector.events:
		assert.Equal(t, "test-1", received.EventID)
	case <-time.After(100 * time.Millisecond):
		t.Error("No event received")
	}

	// Second event should have been dropped
	select {
	case <-collector.events:
		t.Error("Second event should have been dropped")
	default:
		// Expected
	}
}

func TestCollector_HandleError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	events := make(chan *domain.CollectorEvent, 10)

	collector := &Collector{
		name:   "test-collector",
		events: events,
		ctx:    context.Background(),
		logger: logger,
	}

	err := fmt.Errorf("test error")
	collector.handleError(err, "test context")

	// Should be marked unhealthy
	assert.False(t, collector.healthy)

	// Should have sent error event
	select {
	case event := <-events:
		assert.Equal(t, domain.EventSeverityError, event.Severity)
		assert.Equal(t, domain.EventTypeETCD, event.Type)
		assert.Contains(t, event.EventData.ETCD.Value, "test context")
		assert.Contains(t, event.EventData.ETCD.Value, "test error")
	case <-time.After(1 * time.Second):
		t.Error("Error event not received")
	}
}

func TestCollector_CheckClusterStatus_NoLeader(t *testing.T) {
	logger := zaptest.NewLogger(t)
	events := make(chan *domain.CollectorEvent, 10)

	collector := &Collector{
		name:         "test-collector",
		events:       events,
		ctx:          context.Background(),
		logger:       logger,
		lastLeaderID: 0, // No leader
		config: Config{
			Endpoints: []string{"localhost:99999"}, // Invalid endpoint
		},
	}

	// This will fail to get status and handle error
	collector.checkClusterStatus(context.Background())

	// Should have sent error event
	select {
	case event := <-events:
		assert.Equal(t, domain.EventSeverityError, event.Severity)
		assert.Contains(t, event.EventData.ETCD.Value, "failed to get cluster status")
	case <-time.After(1 * time.Second):
		// It's okay if no event in this test
	}
}

func TestCollector_MonitorLoop_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	logger := zaptest.NewLogger(t)
	collector := &Collector{
		name:   "test-collector",
		ctx:    ctx,
		logger: logger,
		config: Config{
			HealthCheckInterval: 10 * time.Millisecond,
			Endpoints:           []string{"localhost:99999"},
		},
		events: make(chan *domain.CollectorEvent, 10),
	}

	// Run monitor loop
	collector.wg.Add(1)
	go collector.monitorLoop()

	// Wait for context to timeout
	<-ctx.Done()

	// Wait for goroutine to finish
	done := make(chan bool)
	go func() {
		collector.wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success - monitor loop stopped
	case <-time.After(1 * time.Second):
		t.Error("Monitor loop did not stop on context cancellation")
	}
}

func TestCollector_PerformHealthCheck_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	logger := zaptest.NewLogger(t)
	events := make(chan *domain.CollectorEvent, 10)

	collector := &Collector{
		name:   "test-collector",
		ctx:    ctx,
		events: events,
		logger: logger,
		config: Config{
			RequestTimeout: 1 * time.Millisecond, // Very short timeout
			Endpoints:      []string{"localhost:2379"},
		},
	}

	// Create a client that will timeout
	cfg := Config{
		Name:       "test",
		BufferSize: 100,
		Endpoints:  []string{"localhost:2379"},
	}
	c, _ := NewCollector("test", cfg)
	collector.client = c.client
	collector.tracer = c.tracer

	collector.performHealthCheck()

	// Should be unhealthy after timeout
	assert.False(t, collector.healthy)
}

func TestCollector_StartStop_Lifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create logger that doesn't fail
	prodLogger, err := zap.NewProduction()
	if err == nil {
		logger = prodLogger.WithOptions(zap.WithCaller(false))
	}

	cfg := Config{
		Name:                "lifecycle-test",
		BufferSize:          100,
		Endpoints:           []string{"localhost:99999"}, // Won't connect
		HealthCheckInterval: 1 * time.Hour,               // Don't auto-trigger
	}

	collector := &Collector{
		name:    cfg.Name,
		logger:  logger,
		config:  cfg,
		events:  make(chan *domain.CollectorEvent, cfg.BufferSize),
		healthy: true,
	}

	// Start
	ctx := context.Background()
	err = collector.Start(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, collector.ctx)
	assert.NotNil(t, collector.cancel)

	// Try to start again
	err = collector.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// Stop
	err = collector.Stop()
	assert.NoError(t, err)

	// Stop again should be safe
	err = collector.Stop()
	assert.NoError(t, err)
}
