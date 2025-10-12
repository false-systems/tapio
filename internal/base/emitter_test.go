package base

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewStdoutEmitter(t *testing.T) {
	emitter := NewStdoutEmitter()
	require.NotNil(t, emitter)
	assert.NotNil(t, emitter.writer)
}

func TestStdoutEmitter_Emit(t *testing.T) {
	buf := &bytes.Buffer{}
	emitter := &StdoutEmitter{writer: buf}

	event := &domain.ObserverEvent{
		ID:        "test-123",
		Type:      "tcp_connect",
		Source:    "network-observer",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := emitter.Emit(ctx, event)

	require.NoError(t, err)

	// Verify JSON was written
	var decoded domain.ObserverEvent
	err = json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)

	assert.Equal(t, "test-123", decoded.ID)
	assert.Equal(t, "tcp_connect", decoded.Type)
	assert.Equal(t, "network-observer", decoded.Source)
}

func TestStdoutEmitter_Close(t *testing.T) {
	emitter := NewStdoutEmitter()
	err := emitter.Close()
	require.NoError(t, err)
}

func TestNewOTELEmitter(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	emitter, err := NewOTELEmitter()
	require.NoError(t, err)
	require.NotNil(t, emitter)
	assert.NotNil(t, emitter.meter)
	assert.NotNil(t, emitter.eventsCounter)
	assert.NotNil(t, emitter.durationHisto)
	assert.NotNil(t, emitter.bytesCounter)
	assert.NotNil(t, emitter.statusCodeHisto)
}

func TestOTELEmitter_Emit(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	emitter, err := NewOTELEmitter()
	require.NoError(t, err)

	event := &domain.ObserverEvent{
		ID:        "test-456",
		Type:      "tcp_connect",
		Source:    "network-observer",
		Timestamp: time.Now(),
		NetworkData: &domain.NetworkEventData{
			SrcIP:         "10.0.1.5",
			DstIP:         "10.0.2.10",
			SrcPort:       45678,
			DstPort:       443,
			Protocol:      "tcp",
			Duration:      1500000,
			BytesSent:     512,
			BytesReceived: 2048,
		},
	}

	ctx := context.Background()
	err = emitter.Emit(ctx, event)
	require.NoError(t, err)

	// Collect metrics
	rm := metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, &rm)
	require.NoError(t, err)

	// Verify events counter was incremented
	eventsSum := findMetricSum(t, rm, "tapio_events_total")
	assert.Equal(t, int64(1), eventsSum)
}

func TestOTELEmitter_Emit_NilEvent(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	emitter, err := NewOTELEmitter()
	require.NoError(t, err)

	ctx := context.Background()
	err = emitter.Emit(ctx, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil event")
}

func TestOTELEmitter_Close(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	emitter, err := NewOTELEmitter()
	require.NoError(t, err)

	err = emitter.Close()
	require.NoError(t, err)
}

func TestNewTapioEmitter(t *testing.T) {
	emitter := NewTapioEmitter(100)
	require.NotNil(t, emitter)
	assert.NotNil(t, emitter.eventChan)
	assert.Equal(t, 100, emitter.bufferSize)
}

func TestTapioEmitter_Emit(t *testing.T) {
	emitter := NewTapioEmitter(10)

	event := &domain.ObserverEvent{
		ID:        "test-tapio-1",
		Type:      "dns_query",
		Source:    "network-observer",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := emitter.Emit(ctx, event)

	require.NoError(t, err)

	// Read from channel
	select {
	case received := <-emitter.Events():
		assert.Equal(t, "test-tapio-1", received.ID)
		assert.Equal(t, "dns_query", received.Type)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestTapioEmitter_Emit_ContextCancelled(t *testing.T) {
	emitter := NewTapioEmitter(1)

	// Fill the buffer
	emitter.eventChan <- &domain.ObserverEvent{ID: "filler"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	event := &domain.ObserverEvent{
		ID:        "test-cancelled",
		Type:      "test",
		Source:    "test",
		Timestamp: time.Now(),
	}

	err := emitter.Emit(ctx, event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
}

func TestTapioEmitter_Emit_ChannelFull(t *testing.T) {
	emitter := NewTapioEmitter(1)

	// Fill the buffer
	ctx := context.Background()
	emitter.Emit(ctx, &domain.ObserverEvent{ID: "1"})

	// Try to emit another (should fail immediately)
	event := &domain.ObserverEvent{
		ID:        "2",
		Type:      "test",
		Source:    "test",
		Timestamp: time.Now(),
	}

	err := emitter.Emit(ctx, event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel full")
}

func TestTapioEmitter_Events(t *testing.T) {
	emitter := NewTapioEmitter(10)

	eventChan := emitter.Events()
	require.NotNil(t, eventChan)

	// Verify we can send and receive
	ctx := context.Background()
	event := &domain.ObserverEvent{ID: "test"}

	emitter.Emit(ctx, event)

	received := <-eventChan
	assert.Equal(t, "test", received.ID)
}

func TestTapioEmitter_Close(t *testing.T) {
	emitter := NewTapioEmitter(10)

	err := emitter.Close()
	require.NoError(t, err)

	// Verify channel is closed
	_, ok := <-emitter.Events()
	assert.False(t, ok, "channel should be closed")
}

func TestNewMultiEmitter(t *testing.T) {
	stdout := NewStdoutEmitter()
	tapio := NewTapioEmitter(10)

	multi := NewMultiEmitter(stdout, tapio)
	require.NotNil(t, multi)
	assert.Len(t, multi.emitters, 2)
}

func TestMultiEmitter_Emit(t *testing.T) {
	buf := &bytes.Buffer{}
	stdout := &StdoutEmitter{writer: buf}
	tapio := NewTapioEmitter(10)

	multi := NewMultiEmitter(stdout, tapio)

	event := &domain.ObserverEvent{
		ID:        "multi-test",
		Type:      "test",
		Source:    "test",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := multi.Emit(ctx, event)

	require.NoError(t, err)

	// Verify stdout received
	assert.Contains(t, buf.String(), "multi-test")

	// Verify tapio received
	select {
	case received := <-tapio.Events():
		assert.Equal(t, "multi-test", received.ID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("tapio did not receive event")
	}
}

func TestMultiEmitter_Emit_PartialFailure(t *testing.T) {
	stdout := &bytes.Buffer{}
	stdoutEmitter := &StdoutEmitter{writer: stdout}

	// Create failing emitter (nil event will cause error)
	tapio := NewTapioEmitter(0) // Buffer size 0, will block/fail

	multi := NewMultiEmitter(stdoutEmitter, tapio)

	event := &domain.ObserverEvent{
		ID:        "partial-fail",
		Type:      "test",
		Source:    "test",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := multi.Emit(ctx, event)

	// Should get error from tapio emitter (buffer full)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to emit")

	// But stdout should have received it
	assert.Contains(t, stdout.String(), "partial-fail")
}

func TestMultiEmitter_Close(t *testing.T) {
	stdout := NewStdoutEmitter()
	tapio := NewTapioEmitter(10)

	multi := NewMultiEmitter(stdout, tapio)

	err := multi.Close()
	require.NoError(t, err)

	// Verify tapio channel is closed
	_, ok := <-tapio.Events()
	assert.False(t, ok)
}

func TestCreateEmitters_Stdout(t *testing.T) {
	config := OutputConfig{
		Stdout: true,
		OTEL:   false,
		Tapio:  false,
	}

	emitter, err := CreateEmitters(config)
	require.NoError(t, err)
	require.NotNil(t, emitter)

	_, ok := emitter.(*StdoutEmitter)
	assert.True(t, ok, "should be StdoutEmitter")
}

func TestCreateEmitters_OTEL(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	config := OutputConfig{
		Stdout: false,
		OTEL:   true,
		Tapio:  false,
	}

	emitter, err := CreateEmitters(config)
	require.NoError(t, err)
	require.NotNil(t, emitter)

	_, ok := emitter.(*OTELEmitter)
	assert.True(t, ok, "should be OTELEmitter")
}

func TestCreateEmitters_Tapio(t *testing.T) {
	config := OutputConfig{
		Stdout: false,
		OTEL:   false,
		Tapio:  true,
	}

	emitter, err := CreateEmitters(config)
	require.NoError(t, err)
	require.NotNil(t, emitter)

	_, ok := emitter.(*TapioEmitter)
	assert.True(t, ok, "should be TapioEmitter")
}

func TestCreateEmitters_Multiple(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	config := OutputConfig{
		Stdout: true,
		OTEL:   true,
		Tapio:  true,
	}

	emitter, err := CreateEmitters(config)
	require.NoError(t, err)
	require.NotNil(t, emitter)

	multi, ok := emitter.(*MultiEmitter)
	assert.True(t, ok, "should be MultiEmitter")
	assert.Len(t, multi.emitters, 3)
}

func TestCreateEmitters_Default(t *testing.T) {
	config := OutputConfig{
		Stdout: false,
		OTEL:   false,
		Tapio:  false,
	}

	emitter, err := CreateEmitters(config)
	require.NoError(t, err)
	require.NotNil(t, emitter)

	// Should default to stdout
	_, ok := emitter.(*StdoutEmitter)
	assert.True(t, ok, "should default to StdoutEmitter")
}

// findMetricSum finds the sum value of a metric by name
func findMetricSum(t *testing.T, rm metricdata.ResourceMetrics, metricName string) int64 {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricName {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					if len(sum.DataPoints) > 0 {
						return sum.DataPoints[0].Value
					}
				}
			}
		}
	}

	t.Fatalf("metric %s not found", metricName)
	return 0
}
