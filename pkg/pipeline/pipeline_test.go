package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/pipeline/parsers"
	"go.uber.org/zap"
)

func TestPipeline_Lifecycle(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		InputBufferSize:  100,
		OutputBufferSize: 100,
		Workers:          2,
		MetricsEnabled:   false,
	}

	pipeline := New(logger, config)
	require.NotNil(t, pipeline)

	// Register a parser
	parser := parsers.NewGenericParser("test")
	err := pipeline.RegisterParser(parser)
	require.NoError(t, err)

	// Start pipeline
	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)

	// Should not be able to start again
	err = pipeline.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// Stop pipeline
	err = pipeline.Stop()
	require.NoError(t, err)

	// Should not be able to stop again
	err = pipeline.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not started")
}

func TestPipeline_ParseEvent(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		InputBufferSize:  10,
		OutputBufferSize: 10,
		Workers:          1,
		MetricsEnabled:   false,
	}

	pipeline := New(logger, config)

	// Register kernel parser
	kernelParser := parsers.NewKernelParser()
	err := pipeline.RegisterParser(kernelParser)
	require.NoError(t, err)

	// Start pipeline
	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()

	// Create a kernel event
	kernelData := parsers.KernelEvent{
		EventType:   "syscall",
		PID:         1234,
		ContainerID: "container-123",
		Syscall:     "open",
		Path:        "/etc/passwd",
		Result:      0,
		Timestamp:   time.Now().UnixNano(),
		ProcessName: "test-process",
		Namespace:   "test-ns",
	}

	data, err := json.Marshal(kernelData)
	require.NoError(t, err)

	rawEvent := &domain.RawEvent{
		Timestamp: time.Now(),
		Source:    "kernel",
		Data:      data,
		Type:      "syscall",
	}

	// Send event to pipeline
	select {
	case pipeline.Input() <- rawEvent:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Failed to send event to pipeline")
	}

	// Receive parsed event
	select {
	case obs := <-pipeline.Output():
		require.NotNil(t, obs)
		assert.Equal(t, "kernel", obs.Source)
		assert.Equal(t, "syscall.open", obs.Type)
		assert.NotNil(t, obs.PID)
		assert.Equal(t, int32(1234), *obs.PID)
		assert.NotNil(t, obs.ContainerID)
		assert.Equal(t, "container-123", *obs.ContainerID)
		assert.NotNil(t, obs.Action)
		assert.Equal(t, "open", *obs.Action)
		assert.NotNil(t, obs.Target)
		assert.Equal(t, "/etc/passwd", *obs.Target)
	case <-time.After(1 * time.Second):
		t.Fatal("Did not receive parsed event")
	}
}

func TestPipeline_DNSParsing(t *testing.T) {
	logger := zap.NewNop()
	pipeline := New(logger, DefaultConfig())

	// Register DNS parser
	dnsParser := parsers.NewDNSParser()
	err := pipeline.RegisterParser(dnsParser)
	require.NoError(t, err)

	// Start pipeline
	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()

	// Create a DNS event
	dnsData := parsers.DNSEvent{
		QueryID:      12345,
		QueryType:    "A",
		QueryName:    "example.com",
		ResponseIPs:  []string{"192.168.1.1", "192.168.1.2"},
		ResponseCode: "NOERROR",
		LatencyMS:    25,
		PID:          5678,
		ContainerID:  "container-456",
		PodName:      "test-pod",
		Namespace:    "test-ns",
	}

	data, err := json.Marshal(dnsData)
	require.NoError(t, err)

	rawEvent := &domain.RawEvent{
		Timestamp: time.Now(),
		Source:    "dns",
		Data:      data,
		Type:      "query",
	}

	// Send and receive
	pipeline.Input() <- rawEvent

	select {
	case obs := <-pipeline.Output():
		require.NotNil(t, obs)
		assert.Equal(t, "dns", obs.Source)
		assert.Equal(t, "dns.a", obs.Type)
		assert.NotNil(t, obs.PID)
		assert.Equal(t, int32(5678), *obs.PID)
		assert.NotNil(t, obs.Target)
		assert.Equal(t, "example.com", *obs.Target)
		assert.NotNil(t, obs.Duration)
		assert.Equal(t, int64(25), *obs.Duration)
		assert.Contains(t, obs.Data["response_ips"], "192.168.1.1")
	case <-time.After(1 * time.Second):
		t.Fatal("Did not receive parsed DNS event")
	}
}

func TestPipeline_InvalidEvent(t *testing.T) {
	logger := zap.NewNop()
	pipeline := New(logger, DefaultConfig())

	// Register a parser
	parser := parsers.NewGenericParser("test")
	err := pipeline.RegisterParser(parser)
	require.NoError(t, err)

	// Start pipeline
	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()

	// Send event with unregistered source
	rawEvent := &domain.RawEvent{
		Timestamp: time.Now(),
		Source:    "unknown",
		Data:      []byte("{}"),
		Type:      "test",
	}

	pipeline.Input() <- rawEvent

	// Should not receive anything (event dropped due to no parser)
	select {
	case obs := <-pipeline.Output():
		t.Fatalf("Should not have received event: %v", obs)
	case <-time.After(100 * time.Millisecond):
		// Expected - no output for invalid source
	}
}

func TestPipeline_Backpressure(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		InputBufferSize:  2,
		OutputBufferSize: 1,
		Workers:          1,
		MetricsEnabled:   false,
	}

	pipeline := New(logger, config)

	// Register parser
	parser := parsers.NewGenericParser("test")
	err := pipeline.RegisterParser(parser)
	require.NoError(t, err)

	// Start pipeline
	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()

	// Send multiple events to fill buffers
	for i := 0; i < 3; i++ {
		event := &domain.RawEvent{
			Timestamp: time.Now(),
			Source:    "test",
			Data:      []byte(fmt.Sprintf(`{"type":"test","pod_name":"pod-%d"}`, i)),
			Type:      "test",
		}

		select {
		case pipeline.Input() <- event:
			// Success
		case <-time.After(50 * time.Millisecond):
			// Expected when buffer is full
			if i < 2 {
				t.Fatalf("Should have accepted event %d", i)
			}
		}
	}

	// Now read events to verify processing
	received := 0
	for i := 0; i < 2; i++ {
		select {
		case obs := <-pipeline.Output():
			require.NotNil(t, obs)
			received++
		case <-time.After(100 * time.Millisecond):
			break
		}
	}

	assert.GreaterOrEqual(t, received, 1, "Should have received at least one event")
}

func TestParserRegistry(t *testing.T) {
	registry := NewParserRegistry()

	// Register parsers
	kernelParser := parsers.NewKernelParser()
	err := registry.Register(kernelParser)
	require.NoError(t, err)

	dnsParser := parsers.NewDNSParser()
	err = registry.Register(dnsParser)
	require.NoError(t, err)

	// Should not allow duplicate registration
	err = registry.Register(kernelParser)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	// Get parser
	parser, exists := registry.Get("kernel")
	assert.True(t, exists)
	assert.NotNil(t, parser)
	assert.Equal(t, "kernel", parser.Source())

	// Get non-existent parser
	parser, exists = registry.Get("unknown")
	assert.False(t, exists)
	assert.Nil(t, parser)

	// List sources
	sources := registry.List()
	assert.Contains(t, sources, "kernel")
	assert.Contains(t, sources, "dns")
	assert.Len(t, sources, 2)
}

func TestPipeline_ProcessRawEvent(t *testing.T) {
	logger := zap.NewNop()
	pipeline := New(logger, DefaultConfig())

	// Register parser
	parser := parsers.NewGenericParser("test")
	err := pipeline.RegisterParser(parser)
	require.NoError(t, err)

	// Start pipeline
	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()

	// Use ProcessRawEvent method
	rawEvent := domain.RawEvent{
		Timestamp: time.Now(),
		Source:    "test",
		Data:      []byte(`{"type":"test","namespace":"test-ns"}`),
		Type:      "test",
	}

	err = pipeline.ProcessRawEvent(ctx, rawEvent)
	require.NoError(t, err)

	// Verify event was processed
	select {
	case obs := <-pipeline.Output():
		require.NotNil(t, obs)
		assert.Equal(t, "test", obs.Source)
		assert.NotNil(t, obs.Namespace)
		assert.Equal(t, "test-ns", *obs.Namespace)
	case <-time.After(1 * time.Second):
		t.Fatal("Did not receive processed event")
	}
}

func BenchmarkPipeline_Throughput(b *testing.B) {
	logger := zap.NewNop()
	config := &Config{
		InputBufferSize:  1000,
		OutputBufferSize: 1000,
		Workers:          4,
		MetricsEnabled:   false,
	}

	pipeline := New(logger, config)

	// Register parser
	parser := parsers.NewGenericParser("bench")
	_ = pipeline.RegisterParser(parser)

	// Start pipeline
	ctx := context.Background()
	_ = pipeline.Start(ctx)
	defer pipeline.Stop()

	// Prepare test data
	data := []byte(`{"type":"bench","pid":1234,"namespace":"bench-ns"}`)

	b.ResetTimer()

	// Send events
	go func() {
		for i := 0; i < b.N; i++ {
			event := &domain.RawEvent{
				Timestamp: time.Now(),
				Source:    "bench",
				Data:      data,
				Type:      "bench",
			}
			pipeline.Input() <- event
		}
	}()

	// Receive events
	for i := 0; i < b.N; i++ {
		<-pipeline.Output()
	}
}
