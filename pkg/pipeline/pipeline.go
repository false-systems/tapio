package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Config holds pipeline configuration
type Config struct {
	// Buffer sizes
	InputBufferSize  int // Buffer for incoming RawEvents
	OutputBufferSize int // Buffer for outgoing ObservationEvents

	// Worker configuration
	Workers int // Number of parser workers

	// Metrics
	MetricsEnabled bool
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		InputBufferSize:  10000,
		OutputBufferSize: 1000,
		Workers:          4,
		MetricsEnabled:   true,
	}
}

// Pipeline transforms RawEvents to ObservationEvents
type Pipeline struct {
	logger   *zap.Logger
	config   *Config
	registry *ParserRegistry

	// Channels
	input  chan *domain.RawEvent
	output chan *domain.ObservationEvent

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	tracer          trace.Tracer
	eventsReceived  metric.Int64Counter
	eventsParsed    metric.Int64Counter
	parseErrors     metric.Int64Counter
	parseLatency    metric.Float64Histogram
	inputQueueSize  metric.Int64ObservableGauge
	outputQueueSize metric.Int64ObservableGauge
}

// New creates a new pipeline
func New(logger *zap.Logger, config *Config) *Pipeline {
	if config == nil {
		config = DefaultConfig()
	}

	p := &Pipeline{
		logger:   logger,
		config:   config,
		registry: NewParserRegistry(),
		input:    make(chan *domain.RawEvent, config.InputBufferSize),
		output:   make(chan *domain.ObservationEvent, config.OutputBufferSize),
	}

	if config.MetricsEnabled {
		p.initMetrics()
	}

	return p
}

// initMetrics initializes OpenTelemetry metrics
func (p *Pipeline) initMetrics() {
	p.tracer = otel.Tracer("pipeline")
	meter := otel.Meter("pipeline")

	var err error

	p.eventsReceived, err = meter.Int64Counter(
		"pipeline_events_received_total",
		metric.WithDescription("Total raw events received"),
	)
	if err != nil {
		p.logger.Warn("Failed to create events received counter", zap.Error(err))
	}

	p.eventsParsed, err = meter.Int64Counter(
		"pipeline_events_parsed_total",
		metric.WithDescription("Total events successfully parsed"),
	)
	if err != nil {
		p.logger.Warn("Failed to create events parsed counter", zap.Error(err))
	}

	p.parseErrors, err = meter.Int64Counter(
		"pipeline_parse_errors_total",
		metric.WithDescription("Total parse errors"),
	)
	if err != nil {
		p.logger.Warn("Failed to create parse errors counter", zap.Error(err))
	}

	p.parseLatency, err = meter.Float64Histogram(
		"pipeline_parse_duration_ms",
		metric.WithDescription("Parse operation duration in milliseconds"),
	)
	if err != nil {
		p.logger.Warn("Failed to create parse latency histogram", zap.Error(err))
	}

	// Observable gauges for queue sizes
	p.inputQueueSize, err = meter.Int64ObservableGauge(
		"pipeline_input_queue_size",
		metric.WithDescription("Current input queue size"),
	)
	if err != nil {
		p.logger.Warn("Failed to create input queue gauge", zap.Error(err))
	}

	p.outputQueueSize, err = meter.Int64ObservableGauge(
		"pipeline_output_queue_size",
		metric.WithDescription("Current output queue size"),
	)
	if err != nil {
		p.logger.Warn("Failed to create output queue gauge", zap.Error(err))
	}

	// Register gauge callbacks
	if p.inputQueueSize != nil && p.outputQueueSize != nil {
		meter.RegisterCallback(
			func(_ context.Context, o metric.Observer) error {
				o.ObserveInt64(p.inputQueueSize, int64(len(p.input)))
				o.ObserveInt64(p.outputQueueSize, int64(len(p.output)))
				return nil
			},
			p.inputQueueSize,
			p.outputQueueSize,
		)
	}
}

// RegisterParser adds a parser to the pipeline
func (p *Pipeline) RegisterParser(parser Parser) error {
	return p.registry.Register(parser)
}

// Input returns the channel for sending raw events to the pipeline
func (p *Pipeline) Input() chan<- *domain.RawEvent {
	return p.input
}

// Output returns the channel for receiving parsed observation events
func (p *Pipeline) Output() <-chan *domain.ObservationEvent {
	return p.output
}

// Start begins processing events
func (p *Pipeline) Start(ctx context.Context) error {
	if p.ctx != nil {
		return fmt.Errorf("pipeline already started")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)

	// Start parser workers
	for i := 0; i < p.config.Workers; i++ {
		p.wg.Add(1)
		go p.parserWorker(i)
	}

	p.logger.Info("Pipeline started",
		zap.Int("workers", p.config.Workers),
		zap.Strings("parsers", p.registry.List()),
	)

	return nil
}

// Stop gracefully shuts down the pipeline
func (p *Pipeline) Stop() error {
	if p.cancel == nil {
		return fmt.Errorf("pipeline not started")
	}

	p.logger.Info("Stopping pipeline")

	// Cancel context to signal workers
	p.cancel()

	// Wait for workers with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("All workers stopped")
	case <-time.After(5 * time.Second):
		p.logger.Error("Timeout waiting for workers")
	}

	// Close channels
	close(p.input)
	close(p.output)

	p.cancel = nil
	p.logger.Info("Pipeline stopped")

	return nil
}

// parserWorker processes raw events
func (p *Pipeline) parserWorker(id int) {
	defer p.wg.Done()

	p.logger.Debug("Parser worker started", zap.Int("worker_id", id))

	for {
		select {
		case <-p.ctx.Done():
			p.logger.Debug("Parser worker stopping", zap.Int("worker_id", id))
			return

		case raw, ok := <-p.input:
			if !ok {
				p.logger.Debug("Input channel closed", zap.Int("worker_id", id))
				return
			}

			p.processEvent(raw)
		}
	}
}

// processEvent parses a single raw event
func (p *Pipeline) processEvent(raw *domain.RawEvent) {
	if raw == nil {
		return
	}

	// Record metric
	if p.eventsReceived != nil {
		p.eventsReceived.Add(p.ctx, 1, metric.WithAttributes(
			attribute.String("source", raw.Source),
		))
	}

	// Start span if tracer is available
	var span trace.Span
	ctx := p.ctx
	if p.tracer != nil {
		ctx, span = p.tracer.Start(p.ctx, "pipeline.parse_event")
		defer span.End()

		span.SetAttributes(
			attribute.String("source", raw.Source),
			attribute.String("type", raw.Type),
			attribute.Int("data_size", len(raw.Data)),
		)
	}

	start := time.Now()

	// Parse the event
	observation, err := p.registry.Parse(raw)
	if err != nil {
		// Log and record error
		p.logger.Debug("Failed to parse event",
			zap.String("source", raw.Source),
			zap.String("type", raw.Type),
			zap.Error(err),
		)

		if p.parseErrors != nil {
			p.parseErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("source", raw.Source),
				attribute.String("error", err.Error()),
			))
		}

		if span != nil {
			span.SetAttributes(attribute.String("error", err.Error()))
		}
		return
	}

	// Validate the parsed event
	if err := observation.Validate(); err != nil {
		p.logger.Debug("Invalid observation event",
			zap.String("source", raw.Source),
			zap.Error(err),
		)

		if p.parseErrors != nil {
			p.parseErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("source", raw.Source),
				attribute.String("error", "validation_failed"),
			))
		}

		if span != nil {
			span.SetAttributes(attribute.String("error", "validation_failed"))
		}
		return
	}

	// Record success metrics
	duration := time.Since(start).Milliseconds()
	if p.parseLatency != nil {
		p.parseLatency.Record(ctx, float64(duration), metric.WithAttributes(
			attribute.String("source", raw.Source),
		))
	}

	if p.eventsParsed != nil {
		p.eventsParsed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("source", raw.Source),
			attribute.String("type", observation.Type),
		))
	}

	// Send to output channel
	select {
	case <-p.ctx.Done():
		return
	case p.output <- observation:
		// Success
	case <-time.After(100 * time.Millisecond):
		// Output channel blocked, drop event
		p.logger.Debug("Dropping event due to output backpressure",
			zap.String("source", raw.Source),
			zap.String("id", observation.ID),
		)
	}
}

// ProcessRawEvent implements domain.RawEventProcessor for compatibility
func (p *Pipeline) ProcessRawEvent(ctx context.Context, event domain.RawEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.input <- &event:
		return nil
	case <-time.After(100 * time.Millisecond):
		return fmt.Errorf("pipeline input buffer full")
	}
}
