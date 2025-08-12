package systemd

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
)

// TestSystemdCollectorEBPFIntegration tests eBPF program loading and event handling
func TestSystemdCollectorEBPFIntegration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("eBPF integration tests require root privileges")
	}

	if testing.Short() {
		t.Skip("Skipping eBPF integration test in short mode")
	}

	// Setup OTEL
	mp := metric.NewMeterProvider()
	otel.SetMeterProvider(mp)
	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)

	defer func() {
		_ = mp.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
	}()

	config := DefaultConfig()
	config.EnableEBPF = true
	config.EnableJournal = false
	config.Logger = zap.NewNop()

	collector, err := NewCollector("ebpf-integration-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// This may fail in CI/test environments, but should not panic
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("eBPF start failed (expected in test environment): %v", err)

		// Verify error is handled gracefully
		assert.Contains(t, err.Error(), "failed to load eBPF program")
		assert.False(t, collector.IsHealthy())
		return
	}

	defer collector.Stop()

	// If eBPF loaded successfully, test event collection
	t.Log("eBPF programs loaded successfully")
	assert.True(t, collector.IsHealthy())

	// Collect events for a short period
	eventCollected := false
	timeout := time.After(5 * time.Second)

	for !eventCollected {
		select {
		case event := <-collector.Events():
			assert.Equal(t, "systemd", event.Type)
			assert.NotEmpty(t, event.TraceID)
			assert.NotEmpty(t, event.SpanID)
			assert.NotEmpty(t, event.Metadata["collector"])
			eventCollected = true
			t.Logf("Collected eBPF event: %s", event.Metadata["event"])

		case <-timeout:
			t.Log("No eBPF events collected (may be normal in test environment)")
			break
		}
	}

	// Verify eBPF statistics
	stats := collector.Statistics()
	assert.Contains(t, stats, "ebpf_load_success")
	assert.Greater(t, stats["ebpf_load_success"], int64(0))
}

// TestSystemdCollectorEBPFFailure tests eBPF failure handling
func TestSystemdCollectorEBPFFailure(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = true
	config.EnableJournal = false
	config.Logger = zap.NewNop()

	collector, err := NewCollector("ebpf-failure-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This should fail in most test environments
	err = collector.Start(ctx)
	assert.Error(t, err)
	assert.False(t, collector.IsHealthy())

	// Verify failure metrics are recorded
	stats := collector.Statistics()
	assert.Contains(t, stats, "ebpf_load_failures")

	// Stop should succeed even after failed start
	err = collector.Stop()
	assert.NoError(t, err)
}

// TestSystemdCollectorJournalIntegration tests systemd journal integration
func TestSystemdCollectorJournalIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping journal integration test in short mode")
	}

	// Check if systemd journal is available
	if _, err := os.Stat("/var/log/journal"); os.IsNotExist(err) {
		if _, err := os.Stat("/run/log/journal"); os.IsNotExist(err) {
			t.Skip("systemd journal not available")
		}
	}

	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = true
	config.Logger = zap.NewNop()

	collector, err := NewCollector("journal-integration-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This may fail if journal access is restricted
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Journal start failed (may be expected): %v", err)
		assert.Contains(t, err.Error(), "journal")
		return
	}

	defer collector.Stop()

	// If journal integration succeeded, test event collection
	t.Log("Journal integration successful")
	assert.True(t, collector.IsHealthy())

	// Look for journal events
	timeout := time.After(3 * time.Second)
	journalEventFound := false

	for !journalEventFound {
		select {
		case event := <-collector.Events():
			if event.Metadata["source"] == "journal" {
				assert.Equal(t, "systemd", event.Type)
				assert.NotEmpty(t, event.Data)
				journalEventFound = true
				t.Logf("Collected journal event: %s", event.Metadata["event"])
			}

		case <-timeout:
			t.Log("No journal events collected (may be normal)")
			break
		}
	}

	// Verify journal statistics
	stats := collector.Statistics()
	assert.Contains(t, stats, "journal_read_time")
}

// TestSystemdEventStructCompatibility tests C struct compatibility
func TestSystemdEventStructCompatibility(t *testing.T) {
	// Test that Go struct matches expected C struct layout
	event := SystemdEvent{}

	// Check struct size and alignment
	structSize := unsafe.Sizeof(event)
	t.Logf("SystemdEvent struct size: %d bytes", structSize)

	// SystemdEvent should be reasonably sized
	minExpectedSize := uintptr(24 + 16 + 256) // Timestamp+PID+PPID+EventType+ExitCode + Comm + Filename
	assert.GreaterOrEqual(t, structSize, minExpectedSize)

	// Test field offsets for C compatibility
	timestampOffset := unsafe.Offsetof(event.Timestamp)
	pidOffset := unsafe.Offsetof(event.PID)
	commOffset := unsafe.Offsetof(event.Comm)
	filenameOffset := unsafe.Offsetof(event.Filename)

	t.Logf("Field offsets - Timestamp: %d, PID: %d, Comm: %d, Filename: %d",
		timestampOffset, pidOffset, commOffset, filenameOffset)

	// Offsets should be reasonable for C struct packing
	assert.Equal(t, uintptr(0), timestampOffset)
	assert.Greater(t, pidOffset, timestampOffset)
	assert.Greater(t, commOffset, pidOffset)
	assert.Greater(t, filenameOffset, commOffset)
}

// TestSystemdCollectorEventParsingSafety tests safe event parsing
func TestSystemdCollectorEventParsingSafety(t *testing.T) {
	collector := &Collector{
		logger: zap.NewNop(),
	}

	// Test with properly structured event
	validEvent := SystemdEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1,
		PPID:      0,
		EventType: 1,
		ExitCode:  0,
	}
	copy(validEvent.Comm[:], "systemd\x00")
	copy(validEvent.Filename[:], "/usr/lib/systemd/systemd\x00")

	// Convert to bytes safely
	eventBytes := (*[unsafe.Sizeof(validEvent)]byte)(unsafe.Pointer(&validEvent))[:]

	// This would be the eBPF event parsing (simulated)
	parsedEvent := (*SystemdEvent)(unsafe.Pointer(&eventBytes[0]))

	assert.Equal(t, validEvent.PID, parsedEvent.PID)
	assert.Equal(t, validEvent.EventType, parsedEvent.EventType)

	// Test string parsing safety
	comm := collector.nullTerminatedString(parsedEvent.Comm[:])
	filename := collector.nullTerminatedString(parsedEvent.Filename[:])

	assert.Equal(t, "systemd", comm)
	assert.Equal(t, "/usr/lib/systemd/systemd", filename)
}

// TestSystemdCollectorFullPipeline tests end-to-end event pipeline
func TestSystemdCollectorFullPipeline(t *testing.T) {
	// Setup complete OTEL pipeline
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)

	defer func() {
		_ = provider.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
	}()

	config := DefaultConfig()
	config.EnableEBPF = false // Disable for reliable testing
	config.EnableJournal = false
	config.Logger = zap.NewNop()

	collector, err := NewCollector("pipeline-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate event processing through the pipeline
	var eventsCollected []collectors.RawEvent

	// Generate test events
	go func() {
		for i := 0; i < 10; i++ {
			data := map[string]interface{}{
				"pid":        1000 + i,
				"comm":       fmt.Sprintf("test-process-%d", i),
				"unit":       fmt.Sprintf("test-%d.service", i),
				"filename":   fmt.Sprintf("/usr/bin/test-%d", i),
				"event_type": "exec",
			}

			event := collector.createEvent("systemd_exec", data)
			if event != nil {
				// Simulate sending through eBPF channel (we create directly)
				select {
				case collector.events <- *event:
				default:
					// Channel full
				}
			}

			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Collect events from pipeline
	timeout := time.After(2 * time.Second)
	for len(eventsCollected) < 5 { // Collect at least 5 events
		select {
		case event := <-collector.Events():
			eventsCollected = append(eventsCollected, event)

		case <-timeout:
			break
		}
	}

	// Verify pipeline processed events correctly
	assert.GreaterOrEqual(t, len(eventsCollected), 1, "Should collect some events")

	for _, event := range eventsCollected {
		assert.Equal(t, "systemd", event.Type)
		assert.NotEmpty(t, event.TraceID)
		assert.NotEmpty(t, event.SpanID)
		assert.Equal(t, "pipeline-systemd", event.Metadata["collector"])
		assert.NotNil(t, event.Data)
		assert.False(t, event.Timestamp.IsZero())
	}

	// Verify metrics were recorded
	metrics := &metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, metrics)
	require.NoError(t, err)

	// Check for systemd-specific metrics
	metricNames := getSystemdMetricNames(metrics)
	t.Logf("Collected metrics: %v", metricNames)

	// Should have some systemd collector metrics
	hasSystemdMetrics := false
	for _, name := range metricNames {
		if contains(name, "pipeline-systemd") || contains(name, "systemd") {
			hasSystemdMetrics = true
			break
		}
	}
	assert.True(t, hasSystemdMetrics, "Should have SystemD-related metrics")
}

// TestSystemdCollectorErrorRecovery tests error recovery mechanisms
func TestSystemdCollectorErrorRecovery(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false
	config.Logger = zap.NewNop()

	collector, err := NewCollector("recovery-systemd", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate various error conditions
	initialStats := collector.Statistics()

	// Test invalid event type handling
	collector.eventTypeToString(999)
	collector.eventTypeToString(0)

	// Test malformed string handling
	malformedData := make([]byte, 256)
	for i := range malformedData {
		malformedData[i] = byte(i % 256) // Include non-printable characters
	}
	malformedData[255] = 0 // Null terminate

	result := collector.nullTerminatedString(malformedData)
	assert.NotPanics(t, func() {
		t.Logf("Parsed malformed string: %q", result)
	})

	// Test rapid event creation (potential memory pressure)
	for i := 0; i < 1000; i++ {
		data := map[string]interface{}{
			"stress": i,
		}
		event := collector.createEvent("stress_test", data)
		assert.NotNil(t, event)
	}

	// Collector should remain healthy after errors
	assert.True(t, collector.IsHealthy())

	finalStats := collector.Statistics()

	// Error count may have increased, but should be handled gracefully
	if finalStats["error_count"].(int64) > initialStats["error_count"].(int64) {
		t.Logf("Error count increased as expected: %d -> %d",
			initialStats["error_count"], finalStats["error_count"])
	}
}

// TestSystemdCollectorContextCancellation tests proper context handling
func TestSystemdCollectorContextCancellation(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false
	config.Logger = zap.NewNop()

	collector, err := NewCollector("context-systemd", config)
	require.NoError(t, err)

	// Create context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Verify collector is running
	assert.True(t, collector.IsHealthy())

	// Cancel context
	cancel()

	// Give some time for cancellation to propagate
	time.Sleep(100 * time.Millisecond)

	// Collector should handle cancellation gracefully
	// (Implementation details may vary, but shouldn't panic)
	collector.Statistics() // Should not panic

	err = collector.Stop()
	assert.NoError(t, err)
}

// Helper functions

func getSystemdMetricNames(rm *metricdata.ResourceMetrics) []string {
	var names []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names = append(names, m.Name)
		}
	}
	return names
}

func contains(str, substr string) bool {
	return len(str) >= len(substr) &&
		(str == substr || (len(str) > len(substr) &&
			(str[:len(substr)] == substr || str[len(str)-len(substr):] == substr)))
}

// TestSystemdCollectorResourceCleanup tests proper resource cleanup
func TestSystemdCollectorResourceCleanup(t *testing.T) {
	config := DefaultConfig()
	config.EnableEBPF = false
	config.EnableJournal = false
	config.Logger = zap.NewNop()

	// Create and destroy multiple collectors to test cleanup
	for i := 0; i < 5; i++ {
		collector, err := NewCollector(fmt.Sprintf("cleanup-test-%d", i), config)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)

		err = collector.Start(ctx)
		require.NoError(t, err)

		// Generate some activity
		data := map[string]interface{}{"test": i}
		event := collector.createEvent("cleanup_test", data)
		assert.NotNil(t, event)

		// Stop collector
		err = collector.Stop()
		assert.NoError(t, err)

		cancel()

		// Ensure collector is properly cleaned up
		assert.False(t, collector.IsHealthy())
	}

	// No goroutine leaks or resource leaks should occur
	// (This would be detected by go test -race or runtime.NumGoroutine())
}
