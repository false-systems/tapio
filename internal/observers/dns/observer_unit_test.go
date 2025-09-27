package dns

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// Helper function to create query name
func makeQueryNameUnit(name string) [253]byte {
	var result [253]byte
	copy(result[:], name)
	return result
}

// Unit tests focus on individual methods and components

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Name:                 "dns-test",
				SlowQueryThresholdMs: 100,
				TimeoutMs:            5000,
			},
			wantErr: false,
		},
		{
			name: "zero values get defaults",
			config: &Config{
				Name: "dns-test",
			},
			wantErr: false,
		},
		{
			name: "negative values get defaults",
			config: &Config{
				Name:                 "dns-test",
				SlowQueryThresholdMs: -1,
				TimeoutMs:            -1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Check defaults were set
				assert.Greater(t, tt.config.SlowQueryThresholdMs, 0)
				assert.Greater(t, tt.config.TimeoutMs, 0)
				assert.Greater(t, tt.config.BufferSize, 0)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, "dns-problems", config.Name)
	assert.Equal(t, 100, config.SlowQueryThresholdMs)
	assert.Equal(t, 5000, config.TimeoutMs)
	assert.Equal(t, 60, config.RepeatWindowSec)
	assert.Equal(t, 3, config.RepeatThreshold)
	assert.True(t, config.OnlyProblems)
	assert.True(t, config.IgnoreLocalhost)
	assert.Contains(t, config.MonitoredPorts, uint16(53))
}

func TestObserver_TrackProblem(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "test-dns"
	config.EnableEBPF = false
	config.RepeatThreshold = 2 // Alert after 2 occurrences

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)
	require.NotNil(t, obs)

	// Create test DNS event
	event := &DNSEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		ProblemType: DNSProblemSlow,
		QueryName:   makeQueryNameUnit("test.example.com"),
		QueryType:   1,           // A record
		LatencyNs:   200_000_000, // 200ms
	}

	// First occurrence - should not trigger repeat alert
	isRepeated := obs.trackProblem(event)
	assert.False(t, isRepeated, "First occurrence should not be marked as repeated")

	// Second occurrence - should trigger repeat alert
	isRepeated = obs.trackProblem(event)
	assert.True(t, isRepeated, "Second occurrence should trigger repeat alert")

	// Check stats
	stats := obs.GetStats()
	assert.Greater(t, stats.SlowQueries, uint64(0))
}

func TestObserver_CleanupOldProblems(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "test-dns"
	config.EnableEBPF = false
	config.RepeatWindowSec = 1 // 1 second window for fast test

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)
	require.NotNil(t, obs)

	// Track a problem
	event := &DNSEvent{
		Timestamp:   uint64(time.Now().UnixNano()),
		ProblemType: DNSProblemTimeout,
		QueryName:   makeQueryNameUnit("timeout.example.com"),
		QueryType:   1,
		LatencyNs:   5_000_000_000, // 5s timeout
	}

	obs.trackProblem(event)
	assert.Len(t, obs.recentProblems, 1, "Should have 1 tracked problem")

	// Sleep to let the problem expire
	time.Sleep(1100 * time.Millisecond)

	// Run cleanup
	obs.doCleanup()
	assert.Len(t, obs.recentProblems, 0, "Old problems should be cleaned up")
}

func TestObserver_Lifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "test-dns"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)
	require.NotNil(t, obs)

	// Should not be healthy before start
	assert.False(t, obs.IsHealthy())

	ctx := context.Background()

	// Start observer
	err = obs.Start(ctx)
	require.NoError(t, err)

	// Should be healthy after start
	assert.True(t, obs.IsHealthy())

	// Get event channel
	events := obs.Events()
	assert.NotNil(t, events)

	// Stop observer
	err = obs.Stop()
	assert.NoError(t, err)

	// Should not be healthy after stop
	assert.False(t, obs.IsHealthy())
}

func TestQueryStats_Fields(t *testing.T) {
	stats := &QueryStats{
		TotalProblems:  4,
		SlowQueries:    1,
		Timeouts:       1,
		NXDomains:      1,
		ServerFailures: 1,
	}

	// Test field values
	assert.Equal(t, uint64(1), stats.SlowQueries)
	assert.Equal(t, uint64(1), stats.Timeouts)
	assert.Equal(t, uint64(1), stats.NXDomains)
	assert.Equal(t, uint64(1), stats.ServerFailures)
	assert.Equal(t, uint64(4), stats.TotalProblems)
}

func TestProblemTracker_Fields(t *testing.T) {
	// Create tracker with recent activity
	tracker := &ProblemTracker{
		QueryName: "test.example.com",
		LastSeen:  time.Now(),
		FirstSeen: time.Now().Add(-30 * time.Second),
		Count:     5,
		ProblemTypes: map[DNSProblemType]int{
			DNSProblemSlow:    3,
			DNSProblemTimeout: 2,
		},
	}

	// Test field values
	assert.Equal(t, "test.example.com", tracker.QueryName)
	assert.Equal(t, 5, tracker.Count)
	assert.Equal(t, 3, tracker.ProblemTypes[DNSProblemSlow])
	assert.Equal(t, 2, tracker.ProblemTypes[DNSProblemTimeout])
}

func TestObserver_Statistics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "test-dns"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)
	require.NotNil(t, obs)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Get statistics
	stats := obs.Statistics()
	assert.NotNil(t, stats)
	assert.GreaterOrEqual(t, stats.EventsProcessed, int64(0))
	assert.GreaterOrEqual(t, stats.ErrorCount, int64(0))
}

func TestObserver_Health(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.Name = "test-dns"
	config.EnableEBPF = false

	obs, err := NewObserver("test", config, logger)
	require.NoError(t, err)
	require.NotNil(t, obs)

	ctx := context.Background()
	err = obs.Start(ctx)
	require.NoError(t, err)
	defer obs.Stop()

	// Get health status
	health := obs.Health()
	assert.NotNil(t, health)
	assert.Equal(t, "healthy", string(health.Status))
}
