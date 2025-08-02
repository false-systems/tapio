package engine

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// AnalyticsEngine provides real-time analytics and correlation capabilities
type AnalyticsEngine struct {
	// Core processing
	eventPipeline     interfaces.EventPipeline
	correlationEngine interfaces.CorrelationEngine
	semanticTracer    interfaces.SemanticTracer

	// Real-time processor
	realTimeProcessor *RealTimeProcessor
	confidenceScorer  *ConfidenceScorer
	impactAssessment  *ImpactAssessment

	// Configuration
	config Config
	logger *zap.Logger
	tracer trace.Tracer

	// State management
	mu         sync.RWMutex
	running    bool
	eventBatch []*domain.UnifiedEvent
	batchSize  int
	lastFlush  time.Time

	// Metrics
	eventsProcessed   uint64
	correlationsFound uint64
	groupsCreated     uint64
	analysisLatency   time.Duration

	// Event streams
	inputStream  chan *domain.UnifiedEvent
	outputStream chan *AnalyticsResult

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config configures the analytics engine
type Config struct {
	// Processing settings
	MaxEventsPerSecond int
	BatchSize          int
	FlushInterval      time.Duration
	WorkerCount        int

	// Correlation settings
	EnableSemanticGrouping bool
	ConfidenceThreshold    float64
	GroupRetentionPeriod   time.Duration

	// Performance settings
	BufferSize     int
	EnableZeroCopy bool
	UseAffinity    bool
	MaxLatency     time.Duration

	// Feature flags
	EnableRealTimeAnalysis   bool
	EnablePredictiveAnalysis bool
	EnableImpactAssessment   bool

	// OTEL settings
	ServiceName    string
	ServiceVersion string
	Environment    string
}

// DefaultConfig returns production-ready default configuration
func DefaultConfig() Config {
	return Config{
		MaxEventsPerSecond:       165000, // Target throughput
		BatchSize:                100,
		FlushInterval:            100 * time.Millisecond,
		WorkerCount:              8,
		EnableSemanticGrouping:   true,
		ConfidenceThreshold:      0.7,
		GroupRetentionPeriod:     30 * time.Minute,
		BufferSize:               65536,
		EnableZeroCopy:           true,
		UseAffinity:              true,
		MaxLatency:               1 * time.Millisecond,
		EnableRealTimeAnalysis:   true,
		EnablePredictiveAnalysis: true,
		EnableImpactAssessment:   true,
		ServiceName:              "tapio.analytics",
		ServiceVersion:           "v1.0.0",
		Environment:              "production",
	}
}

// AnalyticsResult contains analysis results
type AnalyticsResult struct {
	EventID          string
	Timestamp        time.Time
	CorrelationID    string
	SemanticGroupID  string
	ConfidenceScore  float64
	ImpactAssessment *ImpactResult
	PredictedOutcome *PredictionResult
	RelatedEvents    []string
	AnalysisLatency  time.Duration
	Metadata         map[string]interface{}
}

// ImpactResult contains impact assessment results
type ImpactResult struct {
	InfrastructureImpact float64
	TechnicalSeverity    string
	CascadeRisk          float64
	AffectedServices     []string
	RecommendedActions   []string
}

// PredictionResult contains prediction results
type PredictionResult struct {
	Scenario    string
	Probability float64
	TimeToEvent time.Duration
	Confidence  float64
	Mitigation  []string
}

// NewAnalyticsEngine creates a new analytics engine with dependency injection
func NewAnalyticsEngine(
	config Config,
	logger *zap.Logger,
	eventPipeline interfaces.EventPipeline,
	correlationEngine interfaces.CorrelationEngine,
	semanticTracer interfaces.SemanticTracer,
) (*AnalyticsEngine, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize OTEL tracer
	tracer := otel.Tracer(
		config.ServiceName,
		trace.WithInstrumentationVersion(config.ServiceVersion),
	)

	// Initialize analytics components
	realTimeProcessor := NewRealTimeProcessor(config.MaxEventsPerSecond)
	confidenceScorer := NewConfidenceScorer(config.ConfidenceThreshold)
	impactAssessment := NewImpactAssessment()

	engine := &AnalyticsEngine{
		eventPipeline:     eventPipeline,
		correlationEngine: correlationEngine,
		semanticTracer:    semanticTracer,
		realTimeProcessor: realTimeProcessor,
		confidenceScorer:  confidenceScorer,
		impactAssessment:  impactAssessment,
		config:            config,
		logger:            logger,
		tracer:            tracer,
		batchSize:         config.BatchSize,
		eventBatch:        make([]*domain.UnifiedEvent, 0, config.BatchSize),
		inputStream:       make(chan *domain.UnifiedEvent, config.BufferSize),
		outputStream:      make(chan *AnalyticsResult, config.BufferSize),
		ctx:               ctx,
		cancel:            cancel,
		lastFlush:         time.Now(),
	}

	return engine, nil
}

// Start starts the analytics engine
func (ae *AnalyticsEngine) Start() error {
	ae.mu.Lock()
	if ae.running {
		ae.mu.Unlock()
		return fmt.Errorf("analytics engine already running")
	}
	ae.running = true
	ae.mu.Unlock()

	// Start event pipeline
	if err := ae.eventPipeline.Start(); err != nil {
		return fmt.Errorf("failed to start event pipeline: %w", err)
	}

	// Start correlation engine
	if err := ae.correlationEngine.Start(); err != nil {
		return fmt.Errorf("failed to start correlation engine: %w", err)
	}

	// Start processing workers
	ae.wg.Add(4)
	go ae.eventIngestionWorker()
	go ae.analyticsWorker()
	go ae.correlationWorker()
	go ae.metricsCollector()

	ae.logger.Info("Analytics engine started",
		zap.Int("max_events_per_second", ae.config.MaxEventsPerSecond),
		zap.Int("worker_count", ae.config.WorkerCount),
		zap.Int("batch_size", ae.config.BatchSize),
	)

	return nil
}

// Stop stops the analytics engine
func (ae *AnalyticsEngine) Stop() error {
	ae.mu.Lock()
	if !ae.running {
		ae.mu.Unlock()
		return nil
	}
	ae.running = false
	ae.mu.Unlock()

	// Cancel context
	ae.cancel()

	// Wait for workers
	ae.wg.Wait()

	// Stop pipeline
	if err := ae.eventPipeline.Stop(); err != nil {
		ae.logger.Error("Failed to stop event pipeline", zap.Error(err))
	}

	// Stop correlation engine
	if err := ae.correlationEngine.Stop(); err != nil {
		ae.logger.Error("Failed to stop correlation engine", zap.Error(err))
	}

	// Close channels
	close(ae.inputStream)
	close(ae.outputStream)

	ae.logger.Info("Analytics engine stopped")
	return nil
}

// ProcessEvent processes a single event
func (ae *AnalyticsEngine) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) (*AnalyticsResult, error) {
	if !ae.running {
		return nil, fmt.Errorf("analytics engine not running")
	}

	if event == nil {
		return nil, fmt.Errorf("event cannot be nil")
	}

	start := time.Now()
	ctx, span := ae.tracer.Start(ctx, "analytics.process_event",
		trace.WithAttributes(
			attribute.String("event.id", event.ID),
			attribute.String("event.type", string(event.Type)),
			attribute.String("event.severity", event.GetSeverity()),
		),
	)
	defer span.End()

	// Submit to processing pipeline
	select {
	case ae.inputStream <- event:
		ae.eventsProcessed++
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("analytics engine overloaded")
	}

	// Real-time analysis
	result := &AnalyticsResult{
		EventID:         event.ID,
		Timestamp:       time.Now(),
		AnalysisLatency: time.Since(start),
		Metadata:        make(map[string]interface{}),
	}

	// Apply real-time processing
	if ae.config.EnableRealTimeAnalysis {
		if err := ae.realTimeProcessor.Process(ctx, event, result); err != nil {
			span.RecordError(err)
			ae.logger.Error("Real-time processing failed", zap.Error(err))
		}
	}

	// Apply confidence scoring
	result.ConfidenceScore = ae.confidenceScorer.Score(event)

	// Apply impact assessment
	if ae.config.EnableImpactAssessment {
		impact, err := ae.impactAssessment.Assess(ctx, event)
		if err != nil {
			ae.logger.Error("Impact assessment failed", zap.Error(err))
		} else {
			result.ImpactAssessment = impact
		}
	}

	// Update span with results
	span.SetAttributes(
		attribute.Float64("confidence_score", result.ConfidenceScore),
		attribute.String("correlation_id", result.CorrelationID),
		attribute.Float64("analysis_latency_ms", float64(result.AnalysisLatency.Nanoseconds())/1e6),
	)

	return result, nil
}

// ProcessBatch processes multiple events in batch
func (ae *AnalyticsEngine) ProcessBatch(ctx context.Context, events []*domain.UnifiedEvent) ([]*AnalyticsResult, error) {
	if !ae.running {
		return nil, fmt.Errorf("analytics engine not running")
	}

	start := time.Now()
	ctx, span := ae.tracer.Start(ctx, "analytics.process_batch",
		trace.WithAttributes(
			attribute.Int("batch_size", len(events)),
		),
	)
	defer span.End()

	results := make([]*AnalyticsResult, len(events))
	errors := 0

	// Process each event
	for i, event := range events {
		result, err := ae.ProcessEvent(ctx, event)
		if err != nil {
			errors++
			if event != nil {
				ae.logger.Error("Failed to process event in batch",
					zap.String("event_id", event.ID),
					zap.Error(err),
				)
			} else {
				ae.logger.Error("Failed to process nil event in batch",
					zap.Int("index", i),
					zap.Error(err),
				)
			}
			continue
		}
		results[i] = result
	}

	batchLatency := time.Since(start)

	span.SetAttributes(
		attribute.Int("errors", errors),
		attribute.Float64("batch_latency_ms", float64(batchLatency.Nanoseconds())/1e6),
		attribute.Float64("events_per_second", float64(len(events))/batchLatency.Seconds()),
	)

	ae.logger.Debug("Processed event batch",
		zap.Int("batch_size", len(events)),
		zap.Int("errors", errors),
		zap.Duration("latency", batchLatency),
	)

	return results, nil
}

// GetAnalyticsStream returns the analytics results stream
func (ae *AnalyticsEngine) GetAnalyticsStream() <-chan *AnalyticsResult {
	return ae.outputStream
}

// GetMetrics returns current analytics metrics
func (ae *AnalyticsEngine) GetMetrics() *AnalyticsMetrics {
	ae.mu.RLock()
	defer ae.mu.RUnlock()

	pipelineMetrics := ae.eventPipeline.GetMetrics()

	return &AnalyticsMetrics{
		EventsProcessed:   ae.eventsProcessed,
		CorrelationsFound: ae.correlationsFound,
		SemanticGroups:    ae.groupsCreated,
		AnalysisLatency:   ae.analysisLatency,
		Throughput:        pipelineMetrics.Throughput,
		PipelineMetrics:   pipelineMetrics,
		QueueDepth:        len(ae.inputStream),
		OutputBacklog:     len(ae.outputStream),
		IsRunning:         ae.running,
		Uptime:            time.Since(ae.lastFlush),
	}
}

// AnalyticsMetrics contains analytics engine metrics
type AnalyticsMetrics struct {
	EventsProcessed   uint64
	CorrelationsFound uint64
	SemanticGroups    uint64
	AnalysisLatency   time.Duration
	Throughput        uint64
	PipelineMetrics   *interfaces.PipelineMetrics
	QueueDepth        int
	OutputBacklog     int
	IsRunning         bool
	Uptime            time.Duration
}

// eventIngestionWorker handles event ingestion
func (ae *AnalyticsEngine) eventIngestionWorker() {
	defer ae.wg.Done()

	ticker := time.NewTicker(ae.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ae.ctx.Done():
			// Flush remaining batch
			if len(ae.eventBatch) > 0 {
				ae.flushBatch()
			}
			return

		case event := <-ae.inputStream:
			ae.eventBatch = append(ae.eventBatch, event)

			// Flush if batch is full
			if len(ae.eventBatch) >= ae.batchSize {
				ae.flushBatch()
			}

		case <-ticker.C:
			// Periodic flush
			if len(ae.eventBatch) > 0 {
				ae.flushBatch()
			}
		}
	}
}

// analyticsWorker handles analytics processing
func (ae *AnalyticsEngine) analyticsWorker() {
	defer ae.wg.Done()

	for {
		select {
		case <-ae.ctx.Done():
			return
		default:
			// Get processed event from pipeline
			if event, err := ae.eventPipeline.GetOutput(); err == nil {
				ae.processAnalyticsEvent(event)
			} else {
				// No events, sleep briefly
				time.Sleep(10 * time.Microsecond)
			}
		}
	}
}

// correlationWorker handles correlation processing
func (ae *AnalyticsEngine) correlationWorker() {
	defer ae.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ae.ctx.Done():
			return
		case <-ticker.C:
			// Get correlation findings
			if findings := ae.correlationEngine.GetLatestFindings(); findings != nil {
				ae.correlationsFound++
				ae.processCorrelationFindings(findings)
			}

			// Get semantic groups
			groups := ae.semanticTracer.GetSemanticGroups()
			ae.groupsCreated = uint64(len(groups))
		}
	}
}

// metricsCollector collects and reports metrics
func (ae *AnalyticsEngine) metricsCollector() {
	defer ae.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ae.ctx.Done():
			return
		case <-ticker.C:
			metrics := ae.GetMetrics()

			_, span := ae.tracer.Start(ae.ctx, "analytics.metrics_report",
				trace.WithAttributes(
					attribute.Int64("events_processed", int64(metrics.EventsProcessed)),
					attribute.Int64("correlations_found", int64(metrics.CorrelationsFound)),
					attribute.Int64("semantic_groups", int64(metrics.SemanticGroups)),
					attribute.Int64("throughput", int64(metrics.Throughput)),
					attribute.Int("queue_depth", metrics.QueueDepth),
					attribute.Float64("analysis_latency_ms", float64(metrics.AnalysisLatency.Nanoseconds())/1e6),
				),
			)
			span.End()

			ae.logger.Info("Analytics engine metrics",
				zap.Uint64("events_processed", metrics.EventsProcessed),
				zap.Uint64("correlations_found", metrics.CorrelationsFound),
				zap.Uint64("throughput", metrics.Throughput),
				zap.Int("queue_depth", metrics.QueueDepth),
			)
		}
	}
}

// flushBatch flushes the current event batch to the pipeline
func (ae *AnalyticsEngine) flushBatch() {
	if len(ae.eventBatch) == 0 {
		return
	}

	// Convert to pipeline events
	pipelineEvents := make([]*interfaces.PipelineEvent, len(ae.eventBatch))
	for i, event := range ae.eventBatch {
		pipelineEvents[i] = ae.convertToPipelineEvent(event)
	}

	// Submit to pipeline
	for _, pipelineEvent := range pipelineEvents {
		if err := ae.eventPipeline.Submit(pipelineEvent); err != nil {
			ae.logger.Error("Failed to submit event to pipeline", zap.Error(err))
		}
	}

	// Clear batch
	ae.eventBatch = ae.eventBatch[:0]
	ae.lastFlush = time.Now()
}

// convertToPipelineEvent converts unified event to pipeline event
func (ae *AnalyticsEngine) convertToPipelineEvent(event *domain.UnifiedEvent) *interfaces.PipelineEvent {
	pipelineEvent := ae.eventPipeline.GetEvent()
	pipelineEvent.ID = uint64(time.Now().UnixNano())
	pipelineEvent.Type = string(event.Type)
	pipelineEvent.Timestamp = event.Timestamp.UnixNano()
	pipelineEvent.Priority = ae.mapSeverityToPriority(domain.EventSeverity(event.GetSeverity()))

	// Store unified event in metadata
	pipelineEvent.Metadata[0] = uint64(uintptr(unsafe.Pointer(event)))

	return pipelineEvent
}

// processAnalyticsEvent processes an event from the analytics pipeline
func (ae *AnalyticsEngine) processAnalyticsEvent(event *interfaces.PipelineEvent) {
	// Extract unified event from metadata
	unifiedEvent := (*domain.UnifiedEvent)(unsafe.Pointer(uintptr(event.Metadata[0])))

	// Create analytics result
	result := &AnalyticsResult{
		EventID:         unifiedEvent.ID,
		Timestamp:       time.Now(),
		AnalysisLatency: time.Duration(time.Now().UnixNano() - event.Timestamp),
		Metadata:        make(map[string]interface{}),
	}

	// Send result
	select {
	case ae.outputStream <- result:
	default:
		// Output buffer full, drop result
		ae.logger.Warn("Analytics output buffer full, dropping result")
	}

	// Return event to pool
	ae.eventPipeline.PutEvent(event)
}

// processCorrelationFindings processes correlation findings
func (ae *AnalyticsEngine) processCorrelationFindings(findings *interfaces.Finding) {
	// Create enhanced analytics result
	result := &AnalyticsResult{
		EventID:         findings.ID,
		Timestamp:       time.Now(),
		CorrelationID:   findings.ID,
		ConfidenceScore: findings.Confidence,
		RelatedEvents:   extractEventIDsFromDomainEvents(findings.RelatedEvents),
		Metadata:        make(map[string]interface{}),
	}

	// Add correlation metadata
	result.Metadata["pattern_type"] = findings.PatternType
	result.Metadata["description"] = findings.Description

	// Add semantic group info if available
	if findings.SemanticGroup != nil {
		result.SemanticGroupID = findings.SemanticGroup.ID
		result.Metadata["semantic_intent"] = findings.SemanticGroup.Intent
		result.Metadata["semantic_type"] = findings.SemanticGroup.Type
	}

	// Send result
	select {
	case ae.outputStream <- result:
	default:
		ae.logger.Warn("Analytics output buffer full, dropping correlation result")
	}
}

// mapSeverityToPriority maps event severity to pipeline priority
func (ae *AnalyticsEngine) mapSeverityToPriority(severity domain.EventSeverity) uint8 {
	switch severity {
	case "critical":
		return 0 // Highest priority
	case "high", "error":
		return 1
	case "medium", "warning":
		return 2
	case "low", "info":
		return 3
	default:
		return 4 // Lowest priority
	}
}

// ExtractEventIDs extracts event IDs from unified events
func ExtractEventIDs(events []*domain.UnifiedEvent) []string {
	ids := make([]string, len(events))
	for i, event := range events {
		ids[i] = event.ID
	}
	return ids
}

// extractEventIDsFromDomainEvents extracts event IDs from domain events
// This is a temporary adapter until correlation engine is updated to use UnifiedEvent
func extractEventIDsFromDomainEvents(events []*domain.Event) []string {
	ids := make([]string, len(events))
	for i, event := range events {
		ids[i] = string(event.ID)
	}
	return ids
}
