package dataflow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	otelpkg "github.com/yairfalse/tapio/pkg/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// TapioDataFlow integrates OTEL semantic correlation into the existing collector pipeline
type TapioDataFlow struct {
	// Core components
	eventStream       <-chan domain.UnifiedEvent
	outputStream      chan<- domain.UnifiedEvent
	semanticTracer    *correlation.SemanticOTELTracer
	correlationEngine *correlation.SemanticCorrelationEngine

	// OTEL components
	tracer           trace.Tracer
	meter            metric.Meter
	metricsCollector *otelpkg.MetricsCollector
	rootSpan         trace.Span

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	eventsProcessed uint64
	groupsCreated   uint64
	tracesExported  uint64
	lastStatus      time.Time

	// Subscriptions for real-time event delivery
	subscriptions map[string]*subscription
	subMutex      sync.RWMutex
}

// Config holds configuration for TapioDataFlow
type Config struct {
	// Correlation settings
	EnableSemanticGrouping bool
	GroupRetentionPeriod   time.Duration

	// OTEL settings
	ServiceName    string
	ServiceVersion string
	Environment    string

	// Performance settings
	BufferSize    int
	FlushInterval time.Duration
}

// NewTapioDataFlow creates a new data flow integration layer
func NewTapioDataFlow(cfg Config) (*TapioDataFlow, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize OTEL tracer
	tracer := otel.Tracer(
		cfg.ServiceName,
		trace.WithInstrumentationVersion(cfg.ServiceVersion),
	)

	// Initialize OTEL meter
	meter := otel.Meter(
		cfg.ServiceName,
		metric.WithInstrumentationVersion(cfg.ServiceVersion),
	)

	// Create metrics collector
	metricsCollector, err := otelpkg.NewMetricsCollector(meter)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create metrics collector: %w", err)
	}

	// Create root span for the data flow
	ctx, rootSpan := tracer.Start(ctx, "tapio.dataflow.pipeline",
		trace.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("service.version", cfg.ServiceVersion),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)

	tdf := &TapioDataFlow{
		semanticTracer:    correlation.NewSemanticOTELTracer(),
		correlationEngine: correlation.NewSemanticCorrelationEngine(),
		tracer:            tracer,
		meter:             meter,
		metricsCollector:  metricsCollector,
		rootSpan:          rootSpan,
		ctx:               ctx,
		cancel:            cancel,
		lastStatus:        time.Now(),
		subscriptions:     make(map[string]*subscription),
	}

	// Register buffer utilization callback
	if err := metricsCollector.RegisterBufferCallback(ctx, func() float64 {
		if tdf.eventStream == nil {
			return 0
		}
		// This is approximate since we can't get channel length for receive-only channels
		return 0 // Would need access to actual buffer to calculate
	}); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to register buffer callback: %w", err)
	}

	return tdf, nil
}

// Connect links the data flow to input and output streams
func (tdf *TapioDataFlow) Connect(input <-chan domain.UnifiedEvent, output chan<- domain.UnifiedEvent) {
	tdf.eventStream = input
	tdf.outputStream = output
}

// Start begins processing events with OTEL semantic correlation
func (tdf *TapioDataFlow) Start() error {
	if tdf.eventStream == nil || tdf.outputStream == nil {
		return fmt.Errorf("data flow not connected: use Connect() first")
	}

	// Start correlation engine
	if err := tdf.correlationEngine.Start(); err != nil {
		return fmt.Errorf("failed to start correlation engine: %w", err)
	}

	// Start processing goroutines
	tdf.wg.Add(3)
	go tdf.processEvents()
	go tdf.cleanupRoutine()
	go tdf.statusReporter()

	return nil
}

// Stop gracefully shuts down the data flow
func (tdf *TapioDataFlow) Stop() error {
	tdf.cancel()
	tdf.wg.Wait()

	// Stop correlation engine
	if err := tdf.correlationEngine.Stop(); err != nil {
		return fmt.Errorf("failed to stop correlation engine: %w", err)
	}

	// Shutdown metrics collector
	if err := tdf.metricsCollector.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown metrics collector: %w", err)
	}

	// End root span
	tdf.rootSpan.End()

	return nil
}

// processEvents is the main event processing loop
func (tdf *TapioDataFlow) processEvents() {
	defer tdf.wg.Done()

	for {
		select {
		case <-tdf.ctx.Done():
			return

		case event, ok := <-tdf.eventStream:
			if !ok {
				return
			}

			// Process with semantic correlation
			start := time.Now()
			if err := tdf.processEventWithSemantics(event); err != nil {
				// Log error but continue processing
				tdf.recordError(err, "failed to process event")
				tdf.metricsCollector.RecordError(tdf.ctx, "event_processing", "dataflow")
			}

			// Record processing duration
			tdf.metricsCollector.RecordEventProcessingDuration(tdf.ctx, time.Since(start), string(event.Type))

			// Forward enriched event
			select {
			case tdf.outputStream <- event:
				tdf.eventsProcessed++
				tdf.metricsCollector.RecordEventProcessed(tdf.ctx, string(event.Type), string(event.Source))
			case <-tdf.ctx.Done():
				return
			}
		}
	}
}

// processEventWithSemantics enriches event with OTEL semantic correlation
func (tdf *TapioDataFlow) processEventWithSemantics(event domain.UnifiedEvent) error {
	ctx, span := tdf.tracer.Start(tdf.ctx, "dataflow.process_event",
		trace.WithAttributes(
			attribute.String("event.id", event.ID),
			attribute.String("event.type", string(event.Type)),
			attribute.String("event.severity", event.GetSeverity()),
		),
	)
	defer span.End()

	// Apply semantic correlation
	if err := tdf.semanticTracer.ProcessUnifiedEventWithSemanticTrace(ctx, &event); err != nil {
		span.RecordError(err)
		return fmt.Errorf("semantic trace processing failed: %w", err)
	}

	// Feed to correlation engine
	if err := tdf.correlationEngine.ProcessEvent(ctx, &event); err != nil {
		span.RecordError(err)
		return fmt.Errorf("correlation engine processing failed: %w", err)
	}

	// Get correlation findings
	if findings := tdf.correlationEngine.GetLatestFindings(); findings != nil {
		// Enrich event with correlation data
		tdf.enrichEventWithFindings(&event, findings)

		// Add findings to span
		span.SetAttributes(
			attribute.Int("correlation.related_events", len(findings.RelatedEvents)),
			attribute.Float64("correlation.confidence", findings.Confidence),
			attribute.String("correlation.pattern", findings.PatternType),
		)

		// Record correlation metrics
		tdf.metricsCollector.RecordCorrelationFound(ctx, findings.Confidence, findings.PatternType)
	}

	// Get semantic groups for metrics
	groups := tdf.semanticTracer.GetSemanticGroups()
	if len(groups) > int(tdf.groupsCreated) {
		delta := int64(len(groups)) - int64(tdf.groupsCreated)
		tdf.metricsCollector.UpdateActiveSemanticGroups(ctx, delta)
		tdf.groupsCreated = uint64(len(groups))
	}

	span.SetAttributes(
		attribute.Int64("dataflow.events_processed", int64(tdf.eventsProcessed)),
		attribute.Int64("dataflow.semantic_groups", int64(tdf.groupsCreated)),
	)

	return nil
}

// enrichEventWithFindings adds correlation findings to event metadata
func (tdf *TapioDataFlow) enrichEventWithFindings(event *domain.UnifiedEvent, findings *interfaces.Finding) {
	if event.Metadata == nil {
		event.Metadata = make(map[string]string)
	}

	// Add correlation data
	event.Metadata["correlation_id"] = findings.ID
	event.Metadata["correlation_confidence"] = fmt.Sprintf("%.2f", findings.Confidence)
	event.Metadata["correlation_pattern"] = findings.PatternType
	event.Metadata["related_event_count"] = fmt.Sprintf("%d", len(findings.RelatedEvents))

	// Add semantic group info if available
	if findings.SemanticGroup != nil {
		event.Metadata["semantic_group_id"] = findings.SemanticGroup.ID
		event.Metadata["semantic_intent"] = findings.SemanticGroup.Intent
		event.Metadata["semantic_type"] = findings.SemanticGroup.Type
			event.Context.Metadata["prediction_scenario"] = findings.SemanticGroup.Prediction.Scenario
			event.Context.Metadata["prediction_probability"] = findings.SemanticGroup.Prediction.Probability
		}
	}
}

// cleanupRoutine periodically cleans up old semantic groups
func (tdf *TapioDataFlow) cleanupRoutine() {
	defer tdf.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-tdf.ctx.Done():
			return
		case <-ticker.C:
			tdf.semanticTracer.CleanupOldGroups(30 * time.Minute)
		}
	}
}

// statusReporter logs periodic status updates
func (tdf *TapioDataFlow) statusReporter() {
	defer tdf.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-tdf.ctx.Done():
			return
		case <-ticker.C:
			groups := tdf.semanticTracer.GetSemanticGroups()

			_, span := tdf.tracer.Start(tdf.ctx, "dataflow.status_report",
				trace.WithAttributes(
					attribute.Int64("events_processed", int64(tdf.eventsProcessed)),
					attribute.Int("semantic_groups_active", len(groups)),
					attribute.Int64("traces_exported", int64(tdf.tracesExported)),
					attribute.Float64("events_per_second", tdf.calculateEventsPerSecond()),
				),
			)
			span.End()

			tdf.lastStatus = time.Now()
		}
	}
}

// calculateEventsPerSecond calculates current throughput
func (tdf *TapioDataFlow) calculateEventsPerSecond() float64 {
	elapsed := time.Since(tdf.lastStatus).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(tdf.eventsProcessed) / elapsed
}

// recordError records errors with OTEL
func (tdf *TapioDataFlow) recordError(err error, msg string) {
	_, span := tdf.tracer.Start(tdf.ctx, "dataflow.error",
		trace.WithAttributes(
			attribute.String("error.message", msg),
			attribute.String("error.type", fmt.Sprintf("%T", err)),
		),
	)
	span.RecordError(err)
	span.End()
}

// GetMetrics returns current metrics
func (tdf *TapioDataFlow) GetMetrics() map[string]interface{} {
	groups := tdf.semanticTracer.GetSemanticGroups()

	return map[string]interface{}{
		"events_processed":       tdf.eventsProcessed,
		"semantic_groups_active": len(groups),
		"traces_exported":        tdf.tracesExported,
		"events_per_second":      tdf.calculateEventsPerSecond(),
		"uptime_seconds":         time.Since(tdf.lastStatus).Seconds(),
	}
}

// SubmitEvent submits a single event to the dataflow for processing
func (tdf *TapioDataFlow) SubmitEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	if tdf.outputStream == nil {
		return fmt.Errorf("dataflow not connected: output stream is nil")
	}

	// Convert UnifiedEvent to Event
	domainEvent := domain.Event{
		ID:        domain.EventID(event.ID),
		Type:      event.Type,
		Source:    domain.SourceType(event.Source),
		Timestamp: event.Timestamp,
		Severity:  domain.EventSeverity(event.GetSeverity()),
		Data:      map[string]interface{}{"unified_event": event},
	}

	// Send to output stream with context
	select {
	case tdf.outputStream <- domainEvent:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-tdf.ctx.Done():
		return fmt.Errorf("dataflow is shutting down")
	}
}

// Subscribe creates a subscription for filtered events
func (tdf *TapioDataFlow) Subscribe(ctx context.Context, filter string, eventChan chan<- *domain.UnifiedEvent) string {
	tdf.subMutex.Lock()
	defer tdf.subMutex.Unlock()

	// Generate unique subscription ID
	subID := fmt.Sprintf("sub-%d-%d", time.Now().UnixNano(), len(tdf.subscriptions))

	// Create subscription
	sub := &subscription{
		id:        subID,
		filter:    filter,
		eventChan: eventChan,
	}

	tdf.subscriptions[subID] = sub

	return subID
}

// Unsubscribe removes a subscription
func (tdf *TapioDataFlow) Unsubscribe(subID string) error {
	tdf.subMutex.Lock()
	defer tdf.subMutex.Unlock()

	sub, exists := tdf.subscriptions[subID]
	if !exists {
		return fmt.Errorf("subscription %s not found", subID)
	}

	// Close the channel if we own it
	close(sub.eventChan)

	// Remove subscription
	delete(tdf.subscriptions, subID)

	return nil
}

// subscription represents an event subscription
type subscription struct {
	id        string
	filter    string
	eventChan chan<- *domain.UnifiedEvent
}
