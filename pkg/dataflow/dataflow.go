package dataflow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TapioDataFlow integrates OTEL semantic correlation into the existing collector pipeline
type TapioDataFlow struct {
	// Core components
	eventStream       <-chan domain.Event
	outputStream      chan<- domain.Event
	semanticTracer    *correlation.SemanticOTELTracer
	correlationEngine *correlation.SemanticCorrelationEngine

	// OTEL components
	tracer   trace.Tracer
	rootSpan trace.Span

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	eventsProcessed uint64
	groupsCreated   uint64
	tracesExported  uint64
	lastStatus      time.Time
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
func NewTapioDataFlow(cfg Config) *TapioDataFlow {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize OTEL tracer
	tracer := otel.Tracer(
		cfg.ServiceName,
		trace.WithInstrumentationVersion(cfg.ServiceVersion),
	)

	// Create root span for the data flow
	ctx, rootSpan := tracer.Start(ctx, "tapio.dataflow.pipeline",
		trace.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("service.version", cfg.ServiceVersion),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)

	return &TapioDataFlow{
		semanticTracer:    correlation.NewSemanticOTELTracer(),
		correlationEngine: correlation.NewSemanticCorrelationEngine(),
		tracer:            tracer,
		rootSpan:          rootSpan,
		ctx:               ctx,
		cancel:            cancel,
		lastStatus:        time.Now(),
	}
}

// Connect links the data flow to input and output streams
func (tdf *TapioDataFlow) Connect(input <-chan domain.Event, output chan<- domain.Event) {
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
			if err := tdf.processEventWithSemantics(event); err != nil {
				// Log error but continue processing
				tdf.recordError(err, "failed to process event")
			}

			// Forward enriched event
			select {
			case tdf.outputStream <- event:
				tdf.eventsProcessed++
			case <-tdf.ctx.Done():
				return
			}
		}
	}
}

// processEventWithSemantics enriches event with OTEL semantic correlation
func (tdf *TapioDataFlow) processEventWithSemantics(event domain.Event) error {
	ctx, span := tdf.tracer.Start(tdf.ctx, "dataflow.process_event",
		trace.WithAttributes(
			attribute.String("event.id", string(event.ID)),
			attribute.String("event.type", string(event.Type)),
			attribute.String("event.severity", string(event.Severity)),
		),
	)
	defer span.End()

	// Apply semantic correlation
	if err := tdf.semanticTracer.ProcessEventWithSemanticTrace(ctx, &event); err != nil {
		span.RecordError(err)
		return fmt.Errorf("semantic trace processing failed: %w", err)
	}

	// Feed to correlation engine
	tdf.correlationEngine.ProcessEvent(&event)

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
	}

	// Get semantic groups for metrics
	groups := tdf.semanticTracer.GetSemanticGroups()
	if len(groups) > int(tdf.groupsCreated) {
		tdf.groupsCreated = uint64(len(groups))
	}

	span.SetAttributes(
		attribute.Int64("dataflow.events_processed", int64(tdf.eventsProcessed)),
		attribute.Int64("dataflow.semantic_groups", int64(tdf.groupsCreated)),
	)

	return nil
}

// enrichEventWithFindings adds correlation findings to event metadata
func (tdf *TapioDataFlow) enrichEventWithFindings(event *domain.Event, findings *correlation.Finding) {
	if event.Context.Metadata == nil {
		event.Context.Metadata = make(map[string]interface{})
	}

	// Add correlation data
	event.Context.Metadata["correlation_id"] = findings.ID
	event.Context.Metadata["correlation_confidence"] = findings.Confidence
	event.Context.Metadata["correlation_pattern"] = findings.PatternType
	event.Context.Metadata["related_event_count"] = len(findings.RelatedEvents)

	// Add semantic group info if available
	if findings.SemanticGroup != nil {
		event.Context.Metadata["semantic_group_id"] = findings.SemanticGroup.ID
		event.Context.Metadata["semantic_intent"] = findings.SemanticGroup.Intent
		event.Context.Metadata["semantic_type"] = findings.SemanticGroup.Type

		// Add impact assessment
		if findings.SemanticGroup.Impact != nil {
			event.Context.Metadata["impact_business"] = findings.SemanticGroup.Impact.BusinessImpact
			event.Context.Metadata["impact_cascade_risk"] = findings.SemanticGroup.Impact.CascadeRisk
		}

		// Add predictions
		if findings.SemanticGroup.Prediction != nil {
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
