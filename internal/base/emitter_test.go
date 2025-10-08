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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	emitter := NewOTELEmitter(tracer)
	require.NotNil(t, emitter)
	assert.NotNil(t, emitter.tracer)
}

func TestOTELEmitter_Emit(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)

	tracer := tp.Tracer("test")
	emitter := NewOTELEmitter(tracer)

	event := &domain.ObserverEvent{
		ID:        "test-456",
		Type:      "tcp_connect",
		Source:    "network-observer",
		Timestamp: time.Now(),
		NetworkData: &domain.NetworkEventData{
			SrcIP:    "10.0.1.5",
			DstIP:    "10.0.2.10",
			SrcPort:  45678,
			DstPort:  443,
			Protocol: "TCP",
		},
		ProcessData: &domain.ProcessEventData{
			PID:         1234,
			ProcessName: "curl",
			CommandLine: "curl https://example.com",
		},
	}

	ctx := context.Background()
	err := emitter.Emit(ctx, event)

	require.NoError(t, err)

	// Verify span was created
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "tcp_connect", span.Name)

	// Verify attributes exist (simplified check)
	attrMap := make(map[string]interface{})
	for _, attr := range span.Attributes {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}

	assert.Equal(t, "test-456", attrMap["event.id"])
	assert.Equal(t, "10.0.1.5", attrMap["network.src_ip"])
	assert.Equal(t, "10.0.2.10", attrMap["network.dst_ip"])
	assert.Equal(t, int64(1234), attrMap["process.pid"])
	assert.Equal(t, "curl", attrMap["process.name"])
}

func TestOTELEmitter_Emit_NilTracer(t *testing.T) {
	emitter := &OTELEmitter{tracer: nil}

	event := &domain.ObserverEvent{
		ID:        "test-789",
		Type:      "test_event",
		Source:    "test",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := emitter.Emit(ctx, event)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tracer not initialized")
}

func TestOTELEmitter_Close(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)

	tracer := tp.Tracer("test")
	emitter := NewOTELEmitter(tracer)

	err := emitter.Close()
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
	stdout := NewStdoutEmitter()
	failingEmitter := &OTELEmitter{tracer: nil} // Will fail

	multi := NewMultiEmitter(stdout, failingEmitter)

	event := &domain.ObserverEvent{
		ID:        "partial-fail",
		Type:      "test",
		Source:    "test",
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := multi.Emit(ctx, event)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to emit")
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

	emitter := CreateEmitters(config, nil)
	require.NotNil(t, emitter)

	_, ok := emitter.(*StdoutEmitter)
	assert.True(t, ok, "should be StdoutEmitter")
}

func TestCreateEmitters_OTEL(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)

	tracer := tp.Tracer("test")
	config := OutputConfig{
		Stdout: false,
		OTEL:   true,
		Tapio:  false,
	}

	emitter := CreateEmitters(config, tracer)
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

	emitter := CreateEmitters(config, nil)
	require.NotNil(t, emitter)

	_, ok := emitter.(*TapioEmitter)
	assert.True(t, ok, "should be TapioEmitter")
}

func TestCreateEmitters_Multiple(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)

	tracer := tp.Tracer("test")
	config := OutputConfig{
		Stdout: true,
		OTEL:   true,
		Tapio:  true,
	}

	emitter := CreateEmitters(config, tracer)
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

	emitter := CreateEmitters(config, nil)
	require.NotNil(t, emitter)

	// Should default to stdout
	_, ok := emitter.(*StdoutEmitter)
	assert.True(t, ok, "should default to StdoutEmitter")
}
