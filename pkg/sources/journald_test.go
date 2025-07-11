package sources

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/falseyair/tapio/pkg/correlation"
	"github.com/falseyair/tapio/pkg/journald"
)

func TestDefaultJournaldConfig(t *testing.T) {
	config := DefaultJournaldConfig()
	
	assert.NotNil(t, config)
	assert.NotEmpty(t, config.MonitoredServices)
	assert.NotEmpty(t, config.LogLevels)
	assert.NotEmpty(t, config.ErrorPatterns)
	assert.NotEmpty(t, config.WarningPatterns)
	assert.True(t, config.EnableClassification)
	assert.Greater(t, config.EventBufferSize, 0)
	assert.Greater(t, config.ReadBatchSize, 0)
	assert.Greater(t, config.MaxEventsPerSecond, 0)
}

func TestNewJournaldSource(t *testing.T) {
	// Test with default config
	source, err := NewJournaldSource(nil)
	assert.NoError(t, err)
	assert.NotNil(t, source)
	assert.NotNil(t, source.config)
	assert.NotNil(t, source.reader)
	assert.NotNil(t, source.filters)
	assert.NotNil(t, source.patternMatcher)
	assert.NotNil(t, source.eventClassifier)
	assert.False(t, source.isStarted)

	// Test with custom config
	customConfig := &JournaldConfig{
		MonitoredServices:   []string{"test-service"},
		LogLevels:          []string{"error"},
		EnableClassification: false,
		EventBufferSize:    1000,
		ReadBatchSize:      100,
		ReadTimeout:        500 * time.Millisecond,
		MaxEventsPerSecond: 5000,
	}

	source2, err := NewJournaldSource(customConfig)
	assert.NoError(t, err)
	assert.NotNil(t, source2)
	assert.Equal(t, customConfig, source2.config)
	assert.Nil(t, source2.eventClassifier) // Should be nil when classification disabled
}

func TestJournaldSource_GetType(t *testing.T) {
	source, err := NewJournaldSource(nil)
	require.NoError(t, err)
	
	assert.Equal(t, correlation.SourceJournald, source.GetType())
}

func TestJournaldSource_IsAvailable(t *testing.T) {
	source, err := NewJournaldSource(nil)
	require.NoError(t, err)
	
	// Should not be available when not started
	assert.False(t, source.IsAvailable())
}

func TestJournaldSource_StartStop(t *testing.T) {
	config := &JournaldConfig{
		MonitoredServices:    []string{"test"},
		LogLevels:           []string{"info"},
		EnableClassification: false,
		EventBufferSize:     100,
		ReadBatchSize:       10,
		ReadTimeout:         100 * time.Millisecond,
		MaxEventsPerSecond:  1000,
		JournalPath:         "/tmp/test-journal",
		SeekToEnd:           true,
		FollowMode:          false,
		ReconnectInterval:   time.Second,
	}

	source, err := NewJournaldSource(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Start should work
	err = source.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, source.isStarted)
	assert.True(t, source.IsAvailable())

	// Double start should fail
	err = source.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// Stop should work
	err = source.Stop()
	assert.NoError(t, err)
	assert.False(t, source.isStarted)
	assert.False(t, source.IsAvailable())

	// Double stop should be safe
	err = source.Stop()
	assert.NoError(t, err)
}

func TestJournaldSource_Collect(t *testing.T) {
	config := &JournaldConfig{
		MonitoredServices:    []string{"test"},
		LogLevels:           []string{"info"},
		EnableClassification: false,
		EventBufferSize:     100,
		ReadBatchSize:       10,
		ReadTimeout:         50 * time.Millisecond,
		MaxEventsPerSecond:  1000,
		JournalPath:         "/tmp/test-journal",
		SeekToEnd:           true,
		FollowMode:          false,
	}

	source, err := NewJournaldSource(config)
	require.NoError(t, err)

	// Should fail when not started
	_, err = source.Collect()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not started")

	// Start the source
	ctx := context.Background()
	err = source.Start(ctx)
	require.NoError(t, err)
	defer source.Stop()

	// Should succeed when started
	data, err := source.Collect()
	assert.NoError(t, err)
	assert.NotNil(t, data)
	
	// Should be JournaldData type
	journaldData, ok := data.(*correlation.JournaldData)
	assert.True(t, ok)
	assert.NotNil(t, journaldData.Events)
	assert.NotNil(t, journaldData.PatternMatches)
	assert.NotNil(t, journaldData.Statistics)
	assert.NotZero(t, journaldData.Timestamp)
}

func TestJournaldSource_GetData(t *testing.T) {
	config := &JournaldConfig{
		MonitoredServices:    []string{"docker", "kubelet"},
		LogLevels:           []string{"error", "warning"},
		EnableClassification: true,
		EventBufferSize:     100,
		ReadBatchSize:       10,
		ReadTimeout:         50 * time.Millisecond,
		MaxEventsPerSecond:  1000,
		JournalPath:         "/tmp/test-journal",
	}

	source, err := NewJournaldSource(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = source.Start(ctx)
	require.NoError(t, err)
	defer source.Stop()

	tests := []struct {
		dataType string
		params   map[string]interface{}
		valid    bool
	}{
		{"events", nil, true},
		{"patterns", nil, true},
		{"classifications", nil, true},
		{"statistics", nil, true},
		{"service_logs", map[string]interface{}{"service": "docker"}, true},
		{"service_logs", nil, false}, // Missing service parameter
		{"unknown", nil, true},       // Falls back to Collect()
	}

	for _, tt := range tests {
		t.Run(tt.dataType, func(t *testing.T) {
			data, err := source.GetData(ctx, tt.dataType, tt.params)
			if tt.valid {
				assert.NoError(t, err)
				assert.NotNil(t, data)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestJournaldSource_ServiceManagement(t *testing.T) {
	config := &JournaldConfig{
		MonitoredServices:  []string{"docker"},
		EventBufferSize:   100,
		ReadBatchSize:     10,
		ReadTimeout:       50 * time.Millisecond,
		MaxEventsPerSecond: 1000,
		JournalPath:       "/tmp/test-journal",
	}

	source, err := NewJournaldSource(config)
	require.NoError(t, err)

	// Initial services
	services := source.GetMonitoredServices()
	assert.Contains(t, services, "docker")
	assert.Len(t, services, 1)

	// Add service
	err = source.AddMonitoredService("kubelet")
	assert.NoError(t, err)
	
	services = source.GetMonitoredServices()
	assert.Contains(t, services, "docker")
	assert.Contains(t, services, "kubelet")
	assert.Len(t, services, 2)

	// Add duplicate service (should be safe)
	err = source.AddMonitoredService("docker")
	assert.NoError(t, err)
	
	services = source.GetMonitoredServices()
	assert.Len(t, services, 2) // Should still be 2

	// Remove service
	err = source.RemoveMonitoredService("docker")
	assert.NoError(t, err)
	
	services = source.GetMonitoredServices()
	assert.NotContains(t, services, "docker")
	assert.Contains(t, services, "kubelet")
	assert.Len(t, services, 1)

	// Remove non-existent service (should be safe)
	err = source.RemoveMonitoredService("non-existent")
	assert.NoError(t, err)
}

func TestJournaldSource_EventChannel(t *testing.T) {
	config := &JournaldConfig{
		MonitoredServices:  []string{"test"},
		EventBufferSize:   10,
		ReadBatchSize:     5,
		ReadTimeout:       50 * time.Millisecond,
		MaxEventsPerSecond: 1000,
		JournalPath:       "/tmp/test-journal",
	}

	source, err := NewJournaldSource(config)
	require.NoError(t, err)

	// Get event channel
	eventChan := source.GetEventChannel()
	assert.NotNil(t, eventChan)

	// Start source to begin event processing
	ctx := context.Background()
	err = source.Start(ctx)
	require.NoError(t, err)
	defer source.Stop()

	// Channel should be available for reading
	select {
	case <-eventChan:
		// May or may not have events immediately
	case <-time.After(100 * time.Millisecond):
		// Timeout is acceptable
	}
}

func TestJournaldSource_ConcurrentAccess(t *testing.T) {
	config := &JournaldConfig{
		MonitoredServices:  []string{"test"},
		EventBufferSize:   100,
		ReadBatchSize:     10,
		ReadTimeout:       50 * time.Millisecond,
		MaxEventsPerSecond: 1000,
		JournalPath:       "/tmp/test-journal",
	}

	source, err := NewJournaldSource(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = source.Start(ctx)
	require.NoError(t, err)
	defer source.Stop()

	// Test concurrent service management
	results := make(chan error, 10)
	
	for i := 0; i < 5; i++ {
		go func(id int) {
			serviceName := "service-" + string(rune(id))
			err := source.AddMonitoredService(serviceName)
			results <- err
		}(i)
	}
	
	for i := 0; i < 5; i++ {
		go func() {
			_, err := source.Collect()
			results <- err
		}()
	}

	// Collect results
	for i := 0; i < 10; i++ {
		err := <-results
		assert.NoError(t, err)
	}
}

func TestJournaldSource_Configuration(t *testing.T) {
	tests := []struct {
		name   string
		config *JournaldConfig
		valid  bool
	}{
		{
			name:   "nil config uses default",
			config: nil,
			valid:  true,
		},
		{
			name: "minimal valid config",
			config: &JournaldConfig{
				MonitoredServices:  []string{"test"},
				LogLevels:         []string{"info"},
				EventBufferSize:   100,
				ReadBatchSize:     10,
				ReadTimeout:       time.Second,
				MaxEventsPerSecond: 1000,
				JournalPath:       "/tmp/test",
			},
			valid: true,
		},
		{
			name: "config with all features",
			config: &JournaldConfig{
				MonitoredServices:    []string{"docker", "kubelet"},
				IgnoredServices:      []string{"cron"},
				LogLevels:           []string{"error", "warning"},
				ErrorPatterns:       []string{"error", "failed"},
				WarningPatterns:     []string{"warning"},
				EnableClassification: true,
				EventBufferSize:     1000,
				ReadBatchSize:       100,
				ReadTimeout:         2 * time.Second,
				MaxEventsPerSecond:  5000,
				JournalPath:         "/var/log/journal",
				SeekToEnd:           true,
				FollowMode:          true,
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewJournaldSource(tt.config)
			if tt.valid {
				assert.NoError(t, err)
				assert.NotNil(t, source)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// Benchmark tests
func BenchmarkJournaldSource_Collect(b *testing.B) {
	config := &JournaldConfig{
		MonitoredServices:  []string{"test"},
		EventBufferSize:   1000,
		ReadBatchSize:     100,
		ReadTimeout:       10 * time.Millisecond,
		MaxEventsPerSecond: 10000,
		JournalPath:       "/tmp/bench-journal",
	}

	source, err := NewJournaldSource(config)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	err = source.Start(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer source.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := source.Collect()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJournaldSource_GetData(b *testing.B) {
	config := &JournaldConfig{
		MonitoredServices:  []string{"test"},
		EventBufferSize:   1000,
		ReadBatchSize:     100,
		ReadTimeout:       10 * time.Millisecond,
		MaxEventsPerSecond: 10000,
		JournalPath:       "/tmp/bench-journal",
	}

	source, err := NewJournaldSource(config)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	err = source.Start(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer source.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := source.GetData(ctx, "events", nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJournaldSource_ServiceManagement(b *testing.B) {
	config := &JournaldConfig{
		MonitoredServices:  []string{},
		EventBufferSize:   100,
		ReadBatchSize:     10,
		ReadTimeout:       10 * time.Millisecond,
		MaxEventsPerSecond: 1000,
		JournalPath:       "/tmp/bench-journal",
	}

	source, err := NewJournaldSource(config)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		serviceName := "bench-service-" + string(rune(i%100))
		source.AddMonitoredService(serviceName)
		if i%10 == 0 {
			source.RemoveMonitoredService(serviceName)
		}
	}
}