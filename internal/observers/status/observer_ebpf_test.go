//go:build linux
// +build linux

package status

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestEBPFProgramLoading(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests require Linux")
	}

	if os.Getuid() != 0 {
		t.Skip("eBPF loading requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    1000,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-ebpf-load", config)
	require.NoError(t, err)

	t.Run("Load eBPF programs", func(t *testing.T) {
		err := observer.loadEBPF()
		if err != nil {
			t.Logf("eBPF loading failed (expected if not compiled): %v", err)
			// Check if it's a verifier error
			var ve *ebpf.VerifierError
			if errors.As(err, &ve) {
				t.Logf("Verifier error details: %v", ve)
			}
			return
		}

		assert.NotNil(t, observer.ebpfState)
		ebpfState := observer.ebpfState.(*statusEBPF)
		assert.NotNil(t, ebpfState.objs)
		assert.NotNil(t, ebpfState.connTracker)
	})

	t.Run("Clean up eBPF resources", func(t *testing.T) {
		if observer.ebpfState != nil {
			observer.closeEBPF()
			assert.Nil(t, observer.ebpfState)
		}
	})
}

func TestEBPFProbeAttachment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests require Linux")
	}

	if os.Getuid() != 0 {
		t.Skip("Probe attachment requires root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    1000,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-probes", config)
	require.NoError(t, err)

	// Remove memlock limit
	err = rlimit.RemoveMemlock()
	require.NoError(t, err)

	err = observer.loadEBPF()
	if err != nil {
		t.Skipf("Cannot test probes without eBPF: %v", err)
	}
	defer observer.closeEBPF()

	ebpfState := observer.ebpfState.(*statusEBPF)

	t.Run("Attach TCP probes", func(t *testing.T) {
		err := observer.attachStatusProbes()
		if err != nil {
			t.Logf("Probe attachment failed: %v", err)
			return
		}

		// Should have attached at least one probe
		assert.NotEmpty(t, ebpfState.links)
		t.Logf("Successfully attached %d probes", len(ebpfState.links))
	})

	t.Run("Verify probe links", func(t *testing.T) {
		for i, link := range ebpfState.links {
			assert.NotNil(t, link, "Link %d should not be nil", i)
		}
	})
}

func TestEBPFEventParsing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-parsing", config)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Parse valid statusEvent", func(t *testing.T) {
		// Create a mock statusEvent
		event := statusEvent{
			Timestamp:    uint64(time.Now().UnixNano()),
			PID:          1234,
			TID:          5678,
			ServiceHash:  0x12345678,
			EndpointHash: 0x87654321,
			LatencyUS:    1500,
			StatusCode:   500,
			ErrorType:    STATUS_ERROR_5XX,
			Protocol:     1, // HTTP
			Port:         8080,
			SrcIP:        0x0100007F, // 127.0.0.1
			DstIP:        0x0200007F, // 127.0.0.2
			Comm:         [16]byte{'t', 'e', 's', 't', 'a', 'p', 'p'},
		}

		// Serialize to bytes
		var buf bytes.Buffer
		err := binary.Write(&buf, binary.LittleEndian, &event)
		require.NoError(t, err)

		// Process the event
		err = observer.processRawStatusEvent(ctx, buf.Bytes())
		assert.NoError(t, err)
	})

	t.Run("Parse invalid event size", func(t *testing.T) {
		// Too small data
		data := []byte{1, 2, 3, 4}
		err := observer.processRawStatusEvent(ctx, data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid event size")
	})

	t.Run("Convert to domain event", func(t *testing.T) {
		event := &statusEvent{
			Timestamp:    uint64(time.Now().UnixNano()),
			PID:          9999,
			ServiceHash:  0xABCDEF,
			EndpointHash: 0xFEDCBA,
			LatencyUS:    2500,
			StatusCode:   404,
			ErrorType:    STATUS_ERROR_4XX,
			Protocol:     2, // gRPC
			Port:         50051,
			SrcIP:        0x0A000001, // 10.0.0.1
			DstIP:        0x0A000002, // 10.0.0.2
			Comm:         [16]byte{'g', 'r', 'p', 'c', '-', 's', 'v', 'c'},
		}

		domainEvent := observer.convertToDomainEvent(ctx, event)
		assert.NotNil(t, domainEvent)
		assert.Equal(t, domain.EventTypeNetworkConnection, domainEvent.Type)
		assert.Equal(t, domain.EventSeverityWarning, domainEvent.Severity) // 4XX error
		assert.NotNil(t, domainEvent.EventData.Network)
		assert.Equal(t, "1.0.0.10", domainEvent.EventData.Network.SrcIP)
		assert.Equal(t, "2.0.0.10", domainEvent.EventData.Network.DstIP)
		assert.Equal(t, int32(50051), domainEvent.EventData.Network.DstPort)
		assert.Equal(t, "gRPC", domainEvent.EventData.Network.Protocol)
	})
}

func TestEBPFMetricsUpdate(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-metrics", config)
	require.NoError(t, err)

	ctx := context.Background()

	// Add service names for testing
	observer.hashDecoder.AddService(12345, "test-service")
	observer.hashDecoder.AddEndpoint(67890, "/api/test")

	t.Run("Update HTTP error metrics", func(t *testing.T) {
		event := &statusEvent{
			ServiceHash:  12345,
			EndpointHash: 67890,
			StatusCode:   503,
			ErrorType:    STATUS_ERROR_5XX,
			LatencyUS:    5000,
		}

		observer.updateStatusMetrics(ctx, event)

		// Check aggregator was updated
		aggregates := observer.aggregator.Flush()
		assert.Len(t, aggregates, 1)
		agg := aggregates[12345]
		assert.NotNil(t, agg)
		assert.Equal(t, uint64(1), agg.ErrorCount)
	})

	t.Run("Update timeout metrics", func(t *testing.T) {
		event := &statusEvent{
			ServiceHash: 12345,
			ErrorType:   STATUS_ERROR_TIMEOUT,
		}

		observer.updateStatusMetrics(ctx, event)

		// Verify timeout was tracked
		aggregates := observer.aggregator.Flush()
		if agg, exists := aggregates[12345]; exists {
			assert.NotNil(t, agg.ErrorTypes)
		}
	})
}

func TestEBPFRingBufferOperations(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Ring buffer tests require Linux")
	}

	if os.Getuid() != 0 {
		t.Skip("Ring buffer operations require root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    1000,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-ringbuf", config)
	require.NoError(t, err)

	// Start with eBPF
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	if err != nil {
		t.Skipf("Cannot test ring buffer: %v", err)
	}
	defer observer.Stop()

	if observer.ebpfState == nil {
		t.Skip("eBPF not loaded, skipping ring buffer tests")
	}

	ebpfState := observer.ebpfState.(*statusEBPF)

	t.Run("Ring buffer reader exists", func(t *testing.T) {
		assert.NotNil(t, ebpfState.reader)
	})

	t.Run("Event processing goroutine running", func(t *testing.T) {
		// The processEBPFEvents goroutine should be running
		// We can't directly test it, but we can verify the observer is healthy
		assert.True(t, observer.IsHealthy())
	})
}

func TestEBPFMapManagement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Map management tests require Linux")
	}

	if os.Getuid() != 0 {
		t.Skip("Map operations require root privileges")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-maps", config)
	require.NoError(t, err)

	err = observer.loadEBPF()
	if err != nil {
		t.Skipf("Cannot test maps without eBPF: %v", err)
	}
	defer observer.closeEBPF()

	ebpfState := observer.ebpfState.(*statusEBPF)

	t.Run("Connection tracker map", func(t *testing.T) {
		assert.NotNil(t, ebpfState.connTracker)

		info, err := ebpfState.connTracker.Info()
		if err == nil {
			t.Logf("Connection tracker map info:")
			t.Logf("  Type: %v", info.Type)
			t.Logf("  Max entries: %d", info.MaxEntries)
			t.Logf("  Key size: %d", info.KeySize)
			t.Logf("  Value size: %d", info.ValueSize)
		}
	})

	t.Run("Maps cleanup", func(t *testing.T) {
		// Maps should be properly closed
		observer.closeEBPF()
		assert.Nil(t, observer.ebpfState)
	})
}

func TestEBPFErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-errors", config)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Handle corrupted event data", func(t *testing.T) {
		// Create invalid event data
		data := make([]byte, unsafe.Sizeof(statusEvent{}))
		// Fill with random data
		for i := range data {
			data[i] = byte(i % 256)
		}

		// Should not panic
		err := observer.processRawStatusEvent(ctx, data)
		// May or may not error, but should not panic
		_ = err
	})

	t.Run("Handle channel full", func(t *testing.T) {
		// Fill the channel
		observer.EventChannelManager = base.NewEventChannelManager(1, "test", logger)

		// Send multiple events
		sent := 0
		dropped := 0
		for i := 0; i < 10; i++ {
			event := &domain.CollectorEvent{
				EventID: string(rune(i)),
			}
			if observer.EventChannelManager.SendEvent(event) {
				sent++
			} else {
				dropped++
			}
		}

		assert.Greater(t, dropped, 0, "Should have dropped some events")
	})
}

func TestEBPFIntegrationWithAggregator(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-integration", config)
	require.NoError(t, err)

	ctx := context.Background()

	// Add test service mappings
	observer.hashDecoder.AddService(1000, "api-gateway")
	observer.hashDecoder.AddService(2000, "auth-service")
	observer.hashDecoder.AddEndpoint(100, "/api/login")
	observer.hashDecoder.AddEndpoint(200, "/api/users")

	t.Run("Process multiple events", func(t *testing.T) {
		events := []statusEvent{
			{
				ServiceHash:  1000,
				EndpointHash: 100,
				StatusCode:   200,
				ErrorType:    STATUS_OK,
				LatencyUS:    500,
				Timestamp:    uint64(time.Now().UnixNano()),
			},
			{
				ServiceHash:  1000,
				EndpointHash: 100,
				StatusCode:   500,
				ErrorType:    STATUS_ERROR_5XX,
				LatencyUS:    2000,
				Timestamp:    uint64(time.Now().UnixNano()),
			},
			{
				ServiceHash:  2000,
				EndpointHash: 200,
				StatusCode:   401,
				ErrorType:    STATUS_ERROR_4XX,
				LatencyUS:    100,
				Timestamp:    uint64(time.Now().UnixNano()),
			},
		}

		for _, event := range events {
			observer.updateStatusMetrics(ctx, &event)
		}

		// Check aggregation
		aggregates := observer.aggregator.Flush()
		assert.Len(t, aggregates, 2) // Two services

		// Check service 1000 stats
		if agg, exists := aggregates[1000]; exists {
			assert.Equal(t, uint64(2), agg.TotalCount)
			assert.Equal(t, uint64(1), agg.ErrorCount)
			assert.Equal(t, 0.5, agg.ErrorRate())
		}

		// Check service 2000 stats
		if agg, exists := aggregates[2000]; exists {
			assert.Equal(t, uint64(1), agg.TotalCount)
			assert.Equal(t, uint64(1), agg.ErrorCount)
			assert.Equal(t, 1.0, agg.ErrorRate())
		}
	})
}

func TestEBPFProtocolDetection(t *testing.T) {
	tests := []struct {
		proto    uint16
		expected string
	}{
		{1, "HTTP"},
		{2, "gRPC"},
		{3, "TCP"},
		{99, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := getProtocolName(tt.proto)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEBPFErrorTypeMapping(t *testing.T) {
	tests := []struct {
		errorType uint16
		expected  string
	}{
		{STATUS_OK, "OK"},
		{STATUS_ERROR_TIMEOUT, "Timeout"},
		{STATUS_ERROR_REFUSED, "Refused"},
		{STATUS_ERROR_RESET, "Reset"},
		{STATUS_ERROR_5XX, "5XX"},
		{STATUS_ERROR_4XX, "4XX"},
		{STATUS_ERROR_SLOW, "Slow"},
		{STATUS_ERROR_PARTIAL, "Partial"},
		{999, "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := getErrorTypeName(tt.errorType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEBPFEventSeverityMapping(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Enabled:       true,
		BufferSize:    100,
		FlushInterval: 100 * time.Millisecond,
		Logger:        logger,
	}

	observer, err := NewObserver("test-severity", config)
	require.NoError(t, err)

	ctx := context.Background()

	tests := []struct {
		errorType uint16
		expected  domain.EventSeverity
	}{
		{STATUS_OK, domain.EventSeverityInfo},
		{STATUS_ERROR_5XX, domain.EventSeverityError},
		{STATUS_ERROR_TIMEOUT, domain.EventSeverityError},
		{STATUS_ERROR_4XX, domain.EventSeverityWarning},
		{STATUS_ERROR_SLOW, domain.EventSeverityInfo},
	}

	for _, tt := range tests {
		t.Run(getErrorTypeName(tt.errorType), func(t *testing.T) {
			event := &statusEvent{
				ErrorType: tt.errorType,
				Timestamp: uint64(time.Now().UnixNano()),
			}

			domainEvent := observer.convertToDomainEvent(ctx, event)
			assert.Equal(t, tt.expected, domainEvent.Severity)
		})
	}
}