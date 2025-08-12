package systemd

import (
	"context"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestNewCollector(t *testing.T) {
	config := DefaultConfig()
	collector, err := NewCollector("test-systemd", config)
	require.NoError(t, err)
	assert.NotNil(t, collector)
	assert.Equal(t, "test-systemd", collector.Name())
	assert.Equal(t, config.BufferSize, cap(collector.events))
}

func TestCollectorLifecycle(t *testing.T) {
	config := DefaultConfig()
	// Disable eBPF for testing (requires root)
	config.EnableEBPF = false
	// Disable journal for testing (requires systemd)
	config.EnableJournal = false

	collector, err := NewCollector("test-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	// Get events channel
	events := collector.Events()
	assert.NotNil(t, events)

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)
	assert.False(t, collector.IsHealthy())

	// Ensure channel is closed
	select {
	case _, ok := <-events:
		assert.False(t, ok, "events channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Error("events channel was not closed")
	}
}

func TestEventTypeToString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		eventType uint32
		expected  string
	}{
		{1, "exec"},
		{2, "exit"},
		{3, "kill"},
		{99, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := collector.eventTypeToString(tt.eventType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNullTerminatedString(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "null terminated",
			input:    []byte{'h', 'e', 'l', 'l', 'o', 0, 'w', 'o', 'r', 'l', 'd'},
			expected: "hello",
		},
		{
			name:     "no null terminator",
			input:    []byte{'h', 'e', 'l', 'l', 'o'},
			expected: "hello",
		},
		{
			name:     "empty",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "only null",
			input:    []byte{0},
			expected: "",
		},
		{
			name:     "unicode handling",
			input:    []byte{'t', 'ë', 's', 't', 0},
			expected: "tës",
		},
		{
			name:     "special characters",
			input:    []byte{'s', 'y', 's', 't', 'e', 'm', 'd', '.', 's', 'e', 'r', 'v', 'i', 'c', 'e', 0},
			expected: "systemd.service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.nullTerminatedString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// setupOTELForTesting initializes OTEL providers for testing
func setupOTELForTesting(t *testing.T) {
	res, err := resource.New(context.Background(), resource.WithAttributes(
		attribute.String("service.name", "test-systemd"),
	))
	if err != nil {
		t.Fatalf("Failed to create resource: %v", err)
	}

	// Set up tracer provider
	tp := trace.NewTracerProvider(
		trace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Set up meter provider
	mp := metric.NewMeterProvider(
		metric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
}

func TestSystemdEventStructSize(t *testing.T) {
	// Ensure Go struct matches C struct size
	event := SystemdEvent{}
	size := unsafe.Sizeof(event)

	// SystemdEvent should be properly sized for eBPF
	expectedMinSize := uintptr(24 + 16 + 256) // Basic fields + Comm + Filename
	assert.GreaterOrEqual(t, size, expectedMinSize, "SystemdEvent struct size should match C struct")
}

func TestCollectorOTELIntegration(t *testing.T) {
	setupOTELForTesting(t)

	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false

	collector, err := NewCollector("test-systemd", config)
	require.NoError(t, err)

	// Verify OTEL components are initialized
	assert.NotNil(t, collector.tracer)
	assert.NotNil(t, collector.meter)
	assert.NotNil(t, collector.eventsProcessedCtr)
	assert.NotNil(t, collector.eventsDroppedCtr)
	assert.NotNil(t, collector.errorsTotal)
	assert.NotNil(t, collector.processingTime)
	assert.NotNil(t, collector.ebpfOperationsCtr)
}

func TestCollectorHealthAndStatistics(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false

	collector, err := NewCollector("test-systemd-health", config)
	require.NoError(t, err)

	// Test initial health
	healthy, details := collector.Health()
	assert.True(t, healthy)
	assert.Contains(t, details, "healthy")
	assert.Contains(t, details, "events_collected")
	assert.Contains(t, details, "events_dropped")
	assert.Contains(t, details, "error_count")
	assert.Contains(t, details, "systemd_ebpf_loaded")
	assert.Contains(t, details, "journal_enabled")

	// Test statistics
	stats := collector.Statistics()
	assert.Contains(t, stats, "events_collected")
	assert.Contains(t, stats, "events_dropped")
	assert.Contains(t, stats, "error_count")
	assert.Contains(t, stats, "last_event_time")
	assert.Contains(t, stats, "unit_trace_count")
	assert.Contains(t, stats, "correlation_hits")
	assert.Contains(t, stats, "journal_read_time")
}

func TestCollectorCreateEvent(t *testing.T) {
	setupOTELForTesting(t)

	config := DefaultConfig()
	collector, err := NewCollector("test-systemd-events", config)
	require.NoError(t, err)

	data := map[string]interface{}{
		"pid":      1234,
		"comm":     "systemd",
		"unit":     "test.service",
		"filename": "/usr/bin/test",
	}

	event := collector.createEvent("systemd_exec", data)

	assert.Equal(t, "systemd", event.Type)
	assert.Equal(t, "systemd_exec", event.Metadata["event"])
	assert.Equal(t, "test-systemd-events", event.Metadata["collector"])
	assert.NotEmpty(t, event.TraceID)
	assert.NotEmpty(t, event.SpanID)
	assert.NotNil(t, event.Data)
}

func TestCollectorConcurrentOperations(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false

	collector, err := NewCollector("test-systemd-concurrent", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Test concurrent access to health and statistics
	const numGoroutines = 20
	const numIterations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				collector.IsHealthy()
				collector.Statistics()
				collector.Health()
			}
		}()
	}

	wg.Wait()
	assert.True(t, collector.IsHealthy())
}

func TestSystemdEventParsing(t *testing.T) {
	collector := &Collector{}

	// Test with valid systemd event data
	event := SystemdEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1,
		PPID:      0,
		EventType: 1, // exec
		ExitCode:  0,
	}
	copy(event.Comm[:], "systemd\x00")
	copy(event.Filename[:], "/usr/lib/systemd/systemd\x00")

	// Parse event type
	eventTypeStr := collector.eventTypeToString(event.EventType)
	assert.Equal(t, "exec", eventTypeStr)

	// Parse comm field
	comm := collector.nullTerminatedString(event.Comm[:])
	assert.Equal(t, "systemd", comm)

	// Parse filename field
	filename := collector.nullTerminatedString(event.Filename[:])
	assert.Equal(t, "/usr/lib/systemd/systemd", filename)
}

func TestSystemdUnitCorrelation(t *testing.T) {
	config := DefaultConfig()
	collector, err := NewCollector("test-systemd-correlation", config)
	require.NoError(t, err)

	// Test unit trace mapping
	collector.mu.Lock()
	collector.unitTraceMap["nginx.service"] = "trace123"
	collector.unitTraceMap["apache.service"] = "trace456"
	collector.mu.Unlock()

	// Test trace retrieval
	collector.mu.RLock()
	nginxTrace := collector.unitTraceMap["nginx.service"]
	apacheTrace := collector.unitTraceMap["apache.service"]
	collector.mu.RUnlock()

	assert.Equal(t, "trace123", nginxTrace)
	assert.Equal(t, "trace456", apacheTrace)

	// Test non-existent unit
	collector.mu.RLock()
	nonExistentTrace := collector.unitTraceMap["nonexistent.service"]
	collector.mu.RUnlock()
	assert.Empty(t, nonExistentTrace)
}

func TestCollectorErrorHandling(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = true // This will likely fail in test environment
	config.EnableJournal = true

	collector, err := NewCollector("test-systemd-errors", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// This should fail gracefully in test environment
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error in test environment: %v", err)
		// Verify error is handled gracefully
		assert.Contains(t, err.Error(), "failed to load eBPF program")
	} else {
		defer collector.Stop()
	}
}

func TestCollectorDoubleStartStop(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false

	collector, err := NewCollector("test-systemd-double", config)
	require.NoError(t, err)

	ctx := context.Background()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Double start should fail
	err = collector.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)

	// Double stop should not error
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestCollectorInterfaceCompliance(t *testing.T) {
	config := DefaultConfig()
	collector, err := NewCollector("test-systemd-interface", config)
	require.NoError(t, err)

	// Verify it implements collectors.Collector interface
	var _ collectors.Collector = collector
}

// Benchmark tests for performance validation
func BenchmarkEventTypeToString(b *testing.B) {
	collector := &Collector{}
	for i := 0; i < b.N; i++ {
		collector.eventTypeToString(1)
		collector.eventTypeToString(2)
		collector.eventTypeToString(3)
		collector.eventTypeToString(99)
	}
}

func BenchmarkNullTerminatedString(b *testing.B) {
	collector := &Collector{}
	data := []byte{'s', 'y', 's', 't', 'e', 'm', 'd', 0, 'e', 'x', 't', 'r', 'a'}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.nullTerminatedString(data)
	}
}

func BenchmarkCreateEvent(b *testing.B) {
	setupOTELForTesting(&testing.T{})

	config := DefaultConfig()
	collector, _ := NewCollector("bench-systemd", config)

	data := map[string]interface{}{
		"pid":  1234,
		"comm": "systemd",
		"unit": "test.service",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.createEvent("systemd_exec", data)
	}
}

// Test eBPF event type mappings
func TestEventTypeMappingComprehensive(t *testing.T) {
	collector := &Collector{}

	testCases := []struct {
		eventType uint32
		expected  string
	}{
		{0, "unknown"},
		{1, "exec"},
		{2, "exit"},
		{3, "kill"},
		{4, "unknown"},  // Non-existent type
		{10, "unknown"}, // Another non-existent type
		{99, "unknown"}, // Edge case
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("type_%d", tc.eventType), func(t *testing.T) {
			result := collector.eventTypeToString(tc.eventType)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// Test memory safety with invalid data
func TestMemorySafetyWithInvalidData(t *testing.T) {
	collector := &Collector{}

	// Test with various malformed inputs
	testCases := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "all zeros",
			input:    make([]byte, 16),
			expected: "",
		},
		{
			name:     "no null terminator long string",
			input:    []byte("verylongstringwithoutanynullterminatortotesthandling"),
			expected: "verylongstringwithoutanynullterminatortotesthandling",
		},
		{
			name:     "null in middle",
			input:    []byte("sys\x00temd"),
			expected: "sys",
		},
		{
			name:     "binary data",
			input:    []byte{0x01, 0x02, 0x03, 0x00, 0x04, 0x05},
			expected: "\x01\x02\x03",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collector.nullTerminatedString(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
