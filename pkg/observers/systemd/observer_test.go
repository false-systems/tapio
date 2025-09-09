package systemd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestObserverCreation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "Default config",
			config:      nil,
			expectError: false,
		},
		{
			name:        "Custom config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "Invalid config - negative buffer",
			config: &Config{
				BufferSize: -1,
			},
			expectError: true,
		},
		{
			name: "Invalid config - no monitoring enabled",
			config: &Config{
				BufferSize:    1000,
				EnableEBPF:    false,
				EnableJournal: false,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("test-systemd", tt.config)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, observer)
			} else {
				require.NoError(t, err)
				require.NotNil(t, observer)
				assert.Equal(t, "test-systemd", observer.GetName())
			}
		})
	}
}

func TestObserverLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.InfoLevel))
	config := &Config{
		BufferSize:          100,
		EnableEBPF:          true,
		EnableJournal:       false,
		HealthCheckInterval: 1 * time.Second,
		Logger:              logger,
	}

	observer, err := NewObserver("test-lifecycle", config)
	require.NoError(t, err)
	require.NotNil(t, observer)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start observer
	err = observer.Start(ctx)
	require.NoError(t, err)
	assert.True(t, observer.IsHealthy())

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Check statistics
	stats := observer.Statistics()
	assert.NotNil(t, stats)

	// Check health
	health := observer.Health()
	assert.NotNil(t, health)
	assert.Equal(t, "test-lifecycle", health.Component)
	assert.Equal(t, domain.HealthHealthy, health.Status)

	// Stop observer
	err = observer.Stop()
	require.NoError(t, err)
	assert.False(t, observer.IsHealthy())
}

func TestServiceStateTracking(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.InfoLevel))
	config := &Config{
		BufferSize:    100,
		EnableEBPF:    true,
		EnableJournal: false,
		Logger:        logger,
	}

	observer, err := NewObserver("test-state", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)

	// Simulate some events
	events := []struct {
		serviceName string
		eventType   uint8
		exitCode    uint32
	}{
		{"docker.service", EventTypeServiceStart, 0},
		{"nginx.service", EventTypeServiceStart, 0},
		{"nginx.service", EventTypeServiceStop, 0},
		{"postgres.service", EventTypeServiceStart, 0},
		{"postgres.service", EventTypeServiceFailed, 1},
	}

	for _, e := range events {
		event := &SystemdEvent{
			Timestamp: uint64(time.Now().UnixNano()),
			PID:       1234,
			EventType: e.eventType,
			ExitCode:  e.exitCode,
		}
		copy(event.ServiceName[:], e.serviceName)
		observer.processSystemdEvent(ctx, event)
	}

	// Check service states
	states := observer.GetServiceStates()
	assert.Len(t, states, 3)

	// Check specific service states
	dockerState, exists := observer.GetServiceState("docker.service")
	require.True(t, exists)
	assert.Equal(t, StateActive, dockerState.State)

	nginxState, exists := observer.GetServiceState("nginx.service")
	require.True(t, exists)
	assert.Equal(t, StateInactive, nginxState.State)

	postgresState, exists := observer.GetServiceState("postgres.service")
	require.True(t, exists)
	assert.Equal(t, StateFailed, postgresState.State)
	assert.Equal(t, int32(1), postgresState.ExitCode)

	err = observer.Stop()
	require.NoError(t, err)
}

func TestEventProcessing(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.InfoLevel))
	config := &Config{
		BufferSize:         100,
		EnableEBPF:         true,
		EnableJournal:      false,
		RateLimitPerSecond: 10,
		Logger:             logger,
	}

	observer, err := NewObserver("test-events", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)

	// Process an event
	event := &SystemdEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       5678,
		PPID:      1,
		UID:       0,
		GID:       0,
		CgroupID:  12345,
		EventType: EventTypeServiceStart,
		ExitCode:  0,
		Signal:    0,
	}
	copy(event.ServiceName[:], "test.service")
	copy(event.Comm[:], "systemd")
	copy(event.CgroupPath[:], "/system.slice/test.service")

	// Process the event
	observer.processSystemdEvent(ctx, event)

	// Check that event was processed
	select {
	case domainEvent := <-observer.GetChannel():
		assert.NotNil(t, domainEvent)
		assert.Equal(t, domain.EventTypeSystemdService, domainEvent.Type)
		assert.Equal(t, "test-events", domainEvent.Source)

		// Check event data
		systemdData, ok := domainEvent.GetSystemdData()
		require.True(t, ok)
		assert.Equal(t, "test.service", systemdData.Unit)
		assert.Equal(t, "service_start", systemdData.Message)
		assert.Equal(t, int32(5678), systemdData.MainPID)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for event")
	}

	err = observer.Stop()
	require.NoError(t, err)
}

func TestRateLimiting(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.InfoLevel))
	config := &Config{
		BufferSize:         100,
		EnableEBPF:         true,
		EnableJournal:      false,
		RateLimitPerSecond: 2, // Very low rate limit for testing
		Logger:             logger,
	}

	observer, err := NewObserver("test-ratelimit", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)

	// Send many events quickly
	for i := 0; i < 10; i++ {
		event := &SystemdEvent{
			Timestamp: uint64(time.Now().UnixNano()),
			PID:       uint32(1000 + i),
			EventType: EventTypeServiceStart,
		}
		copy(event.ServiceName[:], "test.service")
		observer.processSystemdEvent(ctx, event)
	}

	// Check statistics - should have dropped events
	stats := observer.Statistics()
	assert.Greater(t, stats.EventsDropped, uint64(0))
	assert.Less(t, stats.EventsGenerated, uint64(10))

	err = observer.Stop()
	require.NoError(t, err)
}

func TestEventTypeNames(t *testing.T) {
	tests := []struct {
		eventType uint8
		expected  string
	}{
		{EventTypeServiceStart, "service_start"},
		{EventTypeServiceStop, "service_stop"},
		{EventTypeServiceRestart, "service_restart"},
		{EventTypeServiceReload, "service_reload"},
		{EventTypeServiceFailed, "service_failed"},
		{EventTypeCgroupCreated, "cgroup_created"},
		{EventTypeCgroupDestroyed, "cgroup_destroyed"},
		{99, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := getEventTypeName(tt.eventType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCleanString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello\x00world", "hello"},
		{"test", "test"},
		{"\x00", ""},
		{"service.name\x00\x00\x00", "service.name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := cleanString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "Valid config",
			config: &Config{
				BufferSize:    1000,
				EnableEBPF:    true,
				EnableJournal: false,
			},
			expectError: false,
		},
		{
			name: "Negative buffer size",
			config: &Config{
				BufferSize: -1,
				EnableEBPF: true,
			},
			expectError: true,
			errorMsg:    "buffer size must be greater than 0",
		},
		{
			name: "Buffer size too large",
			config: &Config{
				BufferSize: 2000000,
				EnableEBPF: true,
			},
			expectError: true,
			errorMsg:    "buffer size must not exceed 1,000,000",
		},
		{
			name: "No monitoring enabled",
			config: &Config{
				BufferSize:    1000,
				EnableEBPF:    false,
				EnableJournal: false,
			},
			expectError: true,
			errorMsg:    "at least one of EnableEBPF or EnableJournal must be true",
		},
		{
			name: "Negative rate limit",
			config: &Config{
				BufferSize:         1000,
				EnableEBPF:         true,
				RateLimitPerSecond: -1,
			},
			expectError: true,
			errorMsg:    "rate limit must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
