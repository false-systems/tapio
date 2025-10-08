package base

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OutputConfig defines which output destinations are enabled
type OutputConfig struct {
	OTEL   bool // Export to OpenTelemetry (Grafana, Prometheus, Jaeger)
	Tapio  bool // Export to Tapio ecosystem (UKKO)
	Stdout bool // Debug output to stdout
}

// Emitter interface for all output implementations
type Emitter interface {
	Emit(ctx context.Context, event *domain.ObserverEvent) error
	Close() error
}

// StdoutEmitter outputs events as JSON lines to stdout for debugging
type StdoutEmitter struct {
	writer io.Writer
}

// NewStdoutEmitter creates a stdout emitter
func NewStdoutEmitter() *StdoutEmitter {
	return &StdoutEmitter{
		writer: os.Stdout,
	}
}

// Emit writes event as JSON to stdout
func (e *StdoutEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if _, err := fmt.Fprintln(e.writer, string(data)); err != nil {
		return fmt.Errorf("failed to write to stdout: %w", err)
	}

	return nil
}

// Close is a no-op for stdout
func (e *StdoutEmitter) Close() error {
	return nil
}

// OTELEmitter exports events as OpenTelemetry spans
type OTELEmitter struct {
	tracer trace.Tracer
}

// NewOTELEmitter creates an OTEL emitter
func NewOTELEmitter(tracer trace.Tracer) *OTELEmitter {
	return &OTELEmitter{
		tracer: tracer,
	}
}

// Emit creates an OTEL span for the event
func (e *OTELEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if e.tracer == nil {
		return fmt.Errorf("tracer not initialized")
	}

	_, span := e.tracer.Start(ctx, event.Type,
		trace.WithTimestamp(event.Timestamp),
		trace.WithAttributes(
			attribute.String("event.id", event.ID),
			attribute.String("event.type", event.Type),
			attribute.String("event.source", event.Source),
		),
	)
	defer span.End()

	// Add network data if present
	if event.NetworkData != nil {
		span.SetAttributes(
			attribute.String("network.src_ip", event.NetworkData.SrcIP),
			attribute.String("network.dst_ip", event.NetworkData.DstIP),
			attribute.Int("network.src_port", int(event.NetworkData.SrcPort)),
			attribute.Int("network.dst_port", int(event.NetworkData.DstPort)),
			attribute.String("network.protocol", event.NetworkData.Protocol),
		)
	}

	// Add process data if present
	if event.ProcessData != nil {
		span.SetAttributes(
			attribute.Int("process.pid", int(event.ProcessData.PID)),
			attribute.String("process.name", event.ProcessData.ProcessName),
			attribute.String("process.command", event.ProcessData.CommandLine),
		)
	}

	// Mark span as successful
	span.SetStatus(codes.Ok, "event emitted")

	return nil
}

// Close is a no-op for OTEL
func (e *OTELEmitter) Close() error {
	return nil
}

// TapioEmitter sends events to a channel for Tapio ecosystem processing
type TapioEmitter struct {
	eventChan  chan *domain.ObserverEvent
	bufferSize int
}

// NewTapioEmitter creates a Tapio emitter with buffered channel
func NewTapioEmitter(bufferSize int) *TapioEmitter {
	return &TapioEmitter{
		eventChan:  make(chan *domain.ObserverEvent, bufferSize),
		bufferSize: bufferSize,
	}
}

// Emit sends event to channel (non-blocking with timeout)
func (e *TapioEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	select {
	case e.eventChan <- event:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while emitting event: %w", ctx.Err())
	default:
		return fmt.Errorf("event channel full, dropping event %s", event.ID)


	}
}

// Events returns the channel for reading emitted events
func (e *TapioEmitter) Events() <-chan *domain.ObserverEvent {
	return e.eventChan
}

// Close closes the event channel
func (e *TapioEmitter) Close() error {
	close(e.eventChan)
	return nil
}

// MultiEmitter sends events to multiple emitters
type MultiEmitter struct {
	emitters []Emitter
}

// NewMultiEmitter creates an emitter that fans out to multiple destinations
func NewMultiEmitter(emitters ...Emitter) *MultiEmitter {
	return &MultiEmitter{
		emitters: emitters,
	}
}

// Emit sends event to all configured emitters
func (m *MultiEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	var errs []error

	for _, emitter := range m.emitters {
		if err := emitter.Emit(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to emit to %d emitters: %v", len(errs), errs)
	}

	return nil
}

// Close closes all emitters
func (m *MultiEmitter) Close() error {
	var errs []error

	for _, emitter := range m.emitters {
		if err := emitter.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to close %d emitters: %v", len(errs), errs)
	}

	return nil
}

// CreateEmitters creates emitters based on output configuration
func CreateEmitters(config OutputConfig, tracer trace.Tracer) Emitter {
	var emitters []Emitter

	if config.Stdout {
		emitters = append(emitters, NewStdoutEmitter())
	}

	if config.OTEL {
		emitters = append(emitters, NewOTELEmitter(tracer))
	}

	if config.Tapio {
		emitters = append(emitters, NewTapioEmitter(1000)) // 1000 event buffer
	}

	if len(emitters) == 0 {
		// Default to stdout if no outputs configured
		return NewStdoutEmitter()
	}

	if len(emitters) == 1 {
		return emitters[0]
	}

	return NewMultiEmitter(emitters...)
}
