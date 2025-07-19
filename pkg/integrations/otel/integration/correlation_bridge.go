package integration

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/integrations/otel/cqrs"
	"github.com/yairfalse/tapio/pkg/integrations/otel/domain"
)

// CorrelationBridge connects OTEL Phase 3 integration with the correlation engine v2
// This bridge translates between OTEL trace events and correlation engine insights
type CorrelationBridge[T domain.TraceData] struct {
	// OTEL components
	commandBus *cqrs.CommandBus[T]
	queryBus   *cqrs.QueryBus[T]
	eventBus   *cqrs.EventBus

	// Correlation engine v2
	correlationEngine *collector.CorrelationEngine
	// correlationTracer removed - telemetry deleted

	// Bridge configuration
	config BridgeConfig

	// State management
	activeTraces sync.Map // TraceID -> TraceCorrelationState
	insights     chan collector.Insight
	metrics      *BridgeMetrics

	// Background processing
	ctx          context.Context
	cancel       context.CancelFunc
	shutdownOnce sync.Once
}

// BridgeConfig configures the correlation bridge
type BridgeConfig struct {
	// Processing settings
	BufferSize         int
	ProcessingInterval time.Duration
	CorrelationWindow  time.Duration
	InsightThreshold   float64

	// OTEL integration
	EnableTraceToInsight     bool
	EnableInsightToTrace     bool
	EnablePerformanceTracing bool

	// Quality control
	MinConfidenceScore float64
	MaxCorrelationAge  time.Duration
	EnableValidation   bool
}

// TraceCorrelationState tracks correlation state for a trace
type TraceCorrelationState struct {
	TraceID      domain.TraceID
	StartTime    time.Time
	LastActivity time.Time

	// OTEL data
	SpanCount  int
	EventCount int
	ErrorCount int

	// Correlation data
	InsightCount int
	Confidence   float64
	Patterns     map[string]int

	// Metadata
	ServiceName string
	Environment string
	Tags        map[string]string
}

// BridgeMetrics tracks bridge performance
type BridgeMetrics struct {
	mu sync.RWMutex

	// Processing metrics
	TracesProcessed   uint64
	InsightsGenerated uint64
	CorrelationsFound uint64

	// Performance metrics
	AvgProcessingTime time.Duration
	AvgConfidence     float64

	// Error metrics
	ProcessingErrors uint64
	ValidationErrors uint64
	TimeoutErrors    uint64
}

// NewCorrelationBridge creates a new correlation bridge
func NewCorrelationBridge[T domain.TraceData](
	commandBus *cqrs.CommandBus[T],
	queryBus *cqrs.QueryBus[T],
	eventBus *cqrs.EventBus,
	correlationEngine *collector.CorrelationEngine,
	// correlationTracer removed - telemetry deleted,
	config BridgeConfig,
) *CorrelationBridge[T] {

	applyBridgeDefaults(&config)

	bridge := &CorrelationBridge[T]{
		commandBus:        commandBus,
		queryBus:          queryBus,
		eventBus:          eventBus,
		correlationEngine: correlationEngine,
		correlationTracer: correlationTracer,
		config:            config,
		insights:          make(chan collector.Insight, config.BufferSize),
		metrics:           &BridgeMetrics{},
	}

	return bridge
}

// Start begins the correlation bridge processing
func (bridge *CorrelationBridge[T]) Start(ctx context.Context) error {
	bridge.ctx, bridge.cancel = context.WithCancel(ctx)

	// Start listening to OTEL events if enabled
	if bridge.config.EnableTraceToInsight {
		go bridge.processOTELEvents()
	}

	// Start listening to correlation insights if enabled
	if bridge.config.EnableInsightToTrace {
		go bridge.processCorrelationInsights()
	}

	// Start periodic correlation analysis
	go bridge.runPeriodicCorrelation()

	// Start metrics collection
	go bridge.collectMetrics()

	return nil
}

// processOTELEvents processes OTEL trace events and creates correlation data
func (bridge *CorrelationBridge[T]) processOTELEvents() {
	ticker := time.NewTicker(bridge.config.ProcessingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bridge.ctx.Done():
			return

		case <-ticker.C:
			bridge.correlateTraceData()
		}
	}
}

// processCorrelationInsights processes insights from correlation engine v2
func (bridge *CorrelationBridge[T]) processCorrelationInsights() {
	insightsChan := bridge.correlationEngine.Insights()

	for {
		select {
		case <-bridge.ctx.Done():
			return

		case insight := <-insightsChan:
			bridge.processInsight(insight)
		}
	}
}

// correlateTraceData performs correlation analysis on active traces
func (bridge *CorrelationBridge[T]) correlateTraceData() {
	startTime := time.Now()

	// Get active traces from our state
	var traces []*TraceCorrelationState
	bridge.activeTraces.Range(func(key, value interface{}) bool {
		if state, ok := value.(*TraceCorrelationState); ok {
			// Only process traces that are within correlation window
			if time.Since(state.LastActivity) <= bridge.config.CorrelationWindow {
				traces = append(traces, state)
			}
		}
		return true
	})

	if len(traces) == 0 {
		return
	}

	// Create correlation trace for performance monitoring
	ctx, span := bridge.correlationTracer.TraceCorrelationAnalysis(
		bridge.ctx,
		generateCorrelationID(traces),
		bridge.convertTracesToEvents(traces),
	)
	defer span.End()

	// Process each trace for correlation patterns
	for _, trace := range traces {
		bridge.analyzeTraceCorrelation(ctx, trace)
	}

	// Update metrics
	processingTime := time.Since(startTime)
	bridge.updateProcessingMetrics(len(traces), processingTime)

	// Record performance metrics in trace
	bridge.correlationTracer.TracePerformanceMetrics(
		ctx,
		processingTime,
		bridge.calculateAverageConfidence(traces),
		len(traces),
		bridge.estimateMemoryUsage(),
	)
}

// analyzeTraceCorrelation analyzes a single trace for correlation patterns
func (bridge *CorrelationBridge[T]) analyzeTraceCorrelation(ctx context.Context, trace *TraceCorrelationState) {
	// Create trace context for this analysis
	analysisCtx, analysisSpan := bridge.correlationTracer.TraceLayerAnalysis(
		ctx,
		"otel_trace",
		trace.TraceID.String(),
		"correlation_analysis",
	)
	defer analysisSpan.End()

	// Query OTEL data for this trace
	query := &TraceQuery[T]{
		TraceID:   trace.TraceID,
		StartTime: trace.StartTime,
		EndTime:   time.Now(),
	}

	result, err := bridge.queryBus.Execute(analysisCtx, query)
	if err != nil {
		bridge.metrics.mu.Lock()
		bridge.metrics.ProcessingErrors++
		bridge.metrics.mu.Unlock()
		return
	}

	// Analyze for correlation patterns
	patterns := bridge.extractCorrelationPatterns(result)
	confidence := bridge.calculateCorrelationConfidence(patterns)

	// Update trace state
	trace.LastActivity = time.Now()
	trace.Confidence = confidence
	trace.Patterns = patterns

	// Generate insight if confidence is high enough
	if confidence >= bridge.config.InsightThreshold {
		insight := bridge.createInsightFromTrace(trace, patterns, confidence)

		// Send to correlation engine
		select {
		case bridge.insights <- insight:
			bridge.metrics.mu.Lock()
			bridge.metrics.InsightsGenerated++
			bridge.metrics.mu.Unlock()
		default:
			// Drop if buffer full
		}
	}
}

// processInsight processes an insight from the correlation engine v2
func (bridge *CorrelationBridge[T]) processInsight(insight collector.Insight) {
	// Create trace for insight processing
	ctx, span := bridge.correlationTracer.TraceRootCauseAnalysis(
		bridge.ctx,
		insight.Type,
		float64(len(insight.Actions)), // Use action count as confidence proxy
		[]interface{}{insight},
	)
	defer span.End()

	// Convert insight to OTEL trace events
	events := bridge.convertInsightToEvents(insight)

	// Create OTEL spans for each affected resource
	for _, resource := range insight.Resources {
		bridge.createOTELSpanForResource(ctx, resource, insight, events)
	}

	// Update correlation state if trace is tracked
	if traceID := bridge.extractTraceIDFromInsight(insight); traceID != (domain.TraceID{}) {
		if state, exists := bridge.getTraceState(traceID); exists {
			state.InsightCount++
			state.LastActivity = time.Now()

			// Enhance confidence based on insight
			state.Confidence = bridge.combineConfidence(state.Confidence, 0.8) // Default insight confidence
		}
	}

	bridge.metrics.mu.Lock()
	bridge.metrics.CorrelationsFound++
	bridge.metrics.mu.Unlock()
}

// createOTELSpanForResource creates OTEL spans for correlated resources
func (bridge *CorrelationBridge[T]) createOTELSpanForResource(
	ctx context.Context,
	resource collector.AffectedResource,
	insight collector.Insight,
	events []domain.TraceEvent,
) {
	// Create span name based on resource and insight
	spanName := fmt.Sprintf("correlation.%s.%s", resource.Type, insight.Type)

	// Create span with correlation context
	correlationCtx, correlationSpan := bridge.correlationTracer.TraceLayerAnalysis(
		ctx,
		"correlation_insight",
		resource.Name,
		insight.Type,
	)
	defer correlationSpan.End()

	// Add insight metadata to span
	correlationSpan.SetAttributes(
	// Use the telemetry package's attribute functions
	)

	// Create OTEL command to record this correlation
	cmd := &CorrelationCommand[T]{
		CommandID:    generateCommandID(),
		TraceID:      bridge.generateTraceIDFromResource(resource),
		ResourceName: resource.Name,
		ResourceType: resource.Type,
		InsightType:  insight.Type,
		Confidence:   0.8, // Default insight confidence
		Events:       events,
		Timestamp:    time.Now(),
	}

	// Execute command through OTEL command bus
	if _, err := bridge.commandBus.Execute(correlationCtx, cmd); err != nil {
		bridge.metrics.mu.Lock()
		bridge.metrics.ProcessingErrors++
		bridge.metrics.mu.Unlock()
	}
}

// runPeriodicCorrelation runs periodic correlation analysis
func (bridge *CorrelationBridge[T]) runPeriodicCorrelation() {
	ticker := time.NewTicker(bridge.config.ProcessingInterval * 5) // Less frequent than event processing
	defer ticker.Stop()

	for {
		select {
		case <-bridge.ctx.Done():
			return

		case <-ticker.C:
			bridge.performCrossTraceCorrelation()
		}
	}
}

// performCrossTraceCorrelation analyzes correlations across multiple traces
func (bridge *CorrelationBridge[T]) performCrossTraceCorrelation() {
	// Create correlation analysis trace
	ctx, span := bridge.correlationTracer.TraceMultiLayerCorrelation(
		bridge.ctx,
		fmt.Sprintf("cross_trace_%d", time.Now().UnixNano()),
		bridge.createLayerAnalysisFromActiveTraces(),
	)
	defer span.End()

	// Analyze patterns across all active traces
	patterns := bridge.extractCrossTracePatterns()

	// Generate cluster-level insights if patterns are significant
	if len(patterns) > 0 {
		clusterInsight := bridge.createClusterInsight(patterns)

		// Send to correlation engine
		select {
		case bridge.insights <- clusterInsight:
			bridge.metrics.mu.Lock()
			bridge.metrics.InsightsGenerated++
			bridge.metrics.mu.Unlock()
		default:
			// Drop if buffer full
		}
	}
}

// collectMetrics collects and reports bridge metrics
func (bridge *CorrelationBridge[T]) collectMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-bridge.ctx.Done():
			return

		case <-ticker.C:
			bridge.reportMetrics()
		}
	}
}

// Helper methods

func (bridge *CorrelationBridge[T]) getTraceState(traceID domain.TraceID) (*TraceCorrelationState, bool) {
	if value, exists := bridge.activeTraces.Load(traceID); exists {
		if state, ok := value.(*TraceCorrelationState); ok {
			return state, true
		}
	}
	return nil, false
}

func (bridge *CorrelationBridge[T]) extractCorrelationPatterns(result interface{}) map[string]int {
	patterns := make(map[string]int)

	// Extract patterns from OTEL query result
	// This would analyze spans, events, and attributes for correlation patterns
	// Implementation depends on the specific T type and query result structure

	return patterns
}

func (bridge *CorrelationBridge[T]) calculateCorrelationConfidence(patterns map[string]int) float64 {
	if len(patterns) == 0 {
		return 0.0
	}

	// Calculate confidence based on pattern strength
	totalPatterns := 0
	for _, count := range patterns {
		totalPatterns += count
	}

	// Simple confidence calculation - can be made more sophisticated
	confidence := float64(totalPatterns) / 10.0 // Normalize to 0-1 range
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func (bridge *CorrelationBridge[T]) createInsightFromTrace(
	trace *TraceCorrelationState,
	patterns map[string]int,
	confidence float64,
) collector.Insight {

	return collector.Insight{
		ID:          fmt.Sprintf("otel_insight_%s_%d", trace.TraceID.String(), time.Now().UnixNano()),
		Timestamp:   time.Now(),
		Type:        "otel_correlation",
		Severity:    bridge.calculateSeverityFromConfidence(confidence),
		Title:       fmt.Sprintf("OTEL Correlation Pattern Detected in %s", trace.ServiceName),
		Description: bridge.buildInsightDescription(trace, patterns),
		Resources: []collector.AffectedResource{{
			Type:      "trace",
			Name:      trace.TraceID.String(),
			Namespace: trace.Environment,
		}},
		Actions: bridge.generateActionsFromPatterns(patterns),
	}
}

// Additional helper methods would be implemented here...

func applyBridgeDefaults(config *BridgeConfig) {
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if config.ProcessingInterval == 0 {
		config.ProcessingInterval = 5 * time.Second
	}
	if config.CorrelationWindow == 0 {
		config.CorrelationWindow = 30 * time.Minute
	}
	if config.InsightThreshold == 0 {
		config.InsightThreshold = 0.7
	}
	if config.MinConfidenceScore == 0 {
		config.MinConfidenceScore = 0.5
	}
	if config.MaxCorrelationAge == 0 {
		config.MaxCorrelationAge = time.Hour
	}
}

// Shutdown gracefully shuts down the correlation bridge
func (bridge *CorrelationBridge[T]) Shutdown(ctx context.Context) error {
	bridge.shutdownOnce.Do(func() {
		bridge.cancel()
		close(bridge.insights)
	})
	return nil
}

// Supporting types and interfaces would be defined here...

// TraceQuery represents a query for trace data
type TraceQuery[T domain.TraceData] struct {
	QueryID   string
	TraceID   domain.TraceID
	StartTime time.Time
	EndTime   time.Time
}

func (q *TraceQuery[T]) GetQueryID() string           { return q.QueryID }
func (q *TraceQuery[T]) GetQueryType() cqrs.QueryType { return "get_trace_correlation" }
func (q *TraceQuery[T]) GetParameters() map[string]any {
	return map[string]any{
		"trace_id":   q.TraceID,
		"start_time": q.StartTime,
		"end_time":   q.EndTime,
	}
}
func (q *TraceQuery[T]) GetFilters() []cqrs.QueryFilter  { return nil }
func (q *TraceQuery[T]) GetSorting() []cqrs.SortCriteria { return nil }
func (q *TraceQuery[T]) GetPagination() *cqrs.Pagination { return nil }
func (q *TraceQuery[T]) GetProjection() []string         { return nil }
func (q *TraceQuery[T]) GetTimeRange() *cqrs.TimeRange {
	return &cqrs.TimeRange{Start: q.StartTime, End: q.EndTime}
}
func (q *TraceQuery[T]) Validate() error                          { return nil }
func (q *TraceQuery[T]) EstimateComplexity() cqrs.QueryComplexity { return cqrs.QueryComplexityMedium }

// CorrelationCommand represents a command to record correlation data
type CorrelationCommand[T domain.TraceData] struct {
	CommandID    string
	TraceID      domain.TraceID
	ResourceName string
	ResourceType string
	InsightType  string
	Confidence   float64
	Events       []domain.TraceEvent
	Timestamp    time.Time
}

func (c *CorrelationCommand[T]) GetCommandID() string             { return c.CommandID }
func (c *CorrelationCommand[T]) GetCommandType() cqrs.CommandType { return "record_correlation" }
func (c *CorrelationCommand[T]) GetAggregateID() string           { return c.TraceID.String() }
func (c *CorrelationCommand[T]) GetTraceID() domain.TraceID       { return c.TraceID }
func (c *CorrelationCommand[T]) GetTimestamp() time.Time          { return c.Timestamp }
func (c *CorrelationCommand[T]) GetMetadata() map[string]any {
	return map[string]any{
		"resource_name": c.ResourceName,
		"resource_type": c.ResourceType,
		"insight_type":  c.InsightType,
		"confidence":    c.Confidence,
	}
}
func (c *CorrelationCommand[T]) Validate() error { return nil }
func (c *CorrelationCommand[T]) GetPayload() any { return c.Events }

// Helper function implementations would continue here...
