package base

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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

// OTELEmitter exports events as OpenTelemetry metrics (not fake spans)
// Events are discrete occurrences - they should be metrics, not point-in-time spans
type OTELEmitter struct {
	meter           metric.Meter
	eventsCounter   metric.Int64Counter
	durationHisto   metric.Float64Histogram
	bytesCounter    metric.Int64Counter
	statusCodeHisto metric.Int64Histogram
}

// NewOTELEmitter creates an OTEL emitter that emits proper metrics
func NewOTELEmitter() (*OTELEmitter, error) {
	meter := otel.Meter("tapio.events")

	eventsCounter, err := meter.Int64Counter(
		"tapio_events_total",
		metric.WithDescription("Total events emitted from observers"),
		metric.WithUnit("{events}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events counter: %w", err)
	}

	durationHisto, err := meter.Float64Histogram(
		"tapio_event_duration_ms",
		metric.WithDescription("Event duration in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create duration histogram: %w", err)
	}

	bytesCounter, err := meter.Int64Counter(
		"tapio_event_bytes_total",
		metric.WithDescription("Total bytes in network events"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create bytes counter: %w", err)
	}

	statusCodeHisto, err := meter.Int64Histogram(
		"tapio_http_status_codes",
		metric.WithDescription("HTTP status codes from network events"),
		metric.WithUnit("{status_code}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create status code histogram: %w", err)
	}

	return &OTELEmitter{
		meter:           meter,
		eventsCounter:   eventsCounter,
		durationHisto:   durationHisto,
		bytesCounter:    bytesCounter,
		statusCodeHisto: statusCodeHisto,
	}, nil
}

// Emit emits event as OTEL metrics with semantic conventions
func (e *OTELEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("nil event")
	}

	// Build base attributes using semantic conventions
	attrs := []attribute.KeyValue{
		attribute.String("event.id", event.ID),
		attribute.String("event.source", event.Source),
		attribute.String("event.type", event.Type),
		EventDomainAttribute(event.Type),
	}

	// Add error classification if applicable
	if IsErrorEvent(event) {
		attrs = append(attrs, ErrorTypeAttribute(event.Type))
	}

	// Emit base event counter
	e.eventsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))

	// Add network-specific metrics and attributes
	if event.NetworkData != nil {
		networkAttrs := append(attrs, NetworkAttributes(event.NetworkData)...)

		// Network duration
		if event.NetworkData.Duration > 0 {
			durationMs := float64(event.NetworkData.Duration) / 1_000_000.0
			e.durationHisto.Record(ctx, durationMs, metric.WithAttributes(networkAttrs...))
		}

		// Network bytes
		if event.NetworkData.BytesSent > 0 || event.NetworkData.BytesReceived > 0 {
			totalBytes := int64(event.NetworkData.BytesSent + event.NetworkData.BytesReceived)
			e.bytesCounter.Add(ctx, totalBytes, metric.WithAttributes(networkAttrs...))
		}

		// HTTP status codes
		if event.NetworkData.HTTPStatusCode > 0 {
			e.statusCodeHisto.Record(ctx, int64(event.NetworkData.HTTPStatusCode), metric.WithAttributes(networkAttrs...))
		}
	}

	// Add process-specific attributes
	if event.ProcessData != nil {
		processAttrs := append(attrs, ProcessAttributes(event.ProcessData)...)
		e.eventsCounter.Add(ctx, 1, metric.WithAttributes(processAttrs...))
	}

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
// BREAKING CHANGE: The function signature changed from
//
//	CreateEmitters(config OutputConfig, tracer trace.Tracer) Emitter
//
// to
//
//	CreateEmitters(config OutputConfig) (Emitter, error)
//
// The tracer parameter was removed and an error return value was added.
func CreateEmitters(config OutputConfig) (Emitter, error) {
	var emitters []Emitter

	if config.Stdout {
		emitters = append(emitters, NewStdoutEmitter())
	}

	if config.OTEL {
		otelEmitter, err := NewOTELEmitter()
		if err != nil {
			return nil, fmt.Errorf("failed to create OTEL emitter: %w", err)
		}
		emitters = append(emitters, otelEmitter)
	}

	if config.Tapio {
		emitters = append(emitters, NewTapioEmitter(1000)) // 1000 event buffer
	}

	if len(emitters) == 0 {
		// Default to stdout if no outputs configured
		return NewStdoutEmitter(), nil
	}

	if len(emitters) == 1 {
		return emitters[0], nil
	}

	return NewMultiEmitter(emitters...), nil
}
