package application

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/integrations/otel/domain"
	"github.com/yairfalse/tapio/pkg/integrations/otel/ports"
)

// TraceApplicationServiceImpl implements the primary ports of hexagonal architecture
// This is the application core that orchestrates business logic using injected ports
type TraceApplicationServiceImpl[T domain.TraceData] struct {
	// Injected secondary ports (driven adapters)
	repository      ports.TraceRepositoryPort[T]
	eventStore      ports.TraceEventStorePort
	cache           ports.TraceCachePort[T]
	publisher       ports.TracePublisherPort
	metrics         ports.MetricsCollectorPort
	config          ports.ConfigurationPort
	logger          ports.LoggingPort
	externalService ports.ExternalServicePort

	// Domain services
	correlationService ports.TraceCorrelationService[T]
	samplingService    ports.TraceSamplingService[T]

	// Application state and coordination
	activeTraces sync.Map // map[domain.TraceID]*TraceSession[T]
	commandBus   *CommandBus[T]
	queryBus     *QueryBus[T]
	eventBus     *EventBus

	// Performance optimization
	spanPool    sync.Pool
	contextPool sync.Pool

	// Configuration and policies
	tracingPolicy  *TracingPolicy
	retryPolicy    *RetryPolicy
	circuitBreaker *CircuitBreaker

	// Health and monitoring
	healthMonitor *HealthMonitor
	metrics       *ServiceMetrics

	// Background processing
	backgroundTasks *BackgroundTaskManager
	shutdownChan    chan struct{}
	shutdownOnce    sync.Once
}

// NewTraceApplicationService creates a new trace application service with dependency injection
func NewTraceApplicationService[T domain.TraceData](
	repository ports.TraceRepositoryPort[T],
	eventStore ports.TraceEventStorePort,
	cache ports.TraceCachePort[T],
	publisher ports.TracePublisherPort,
	metricsCollector ports.MetricsCollectorPort,
	config ports.ConfigurationPort,
	logger ports.LoggingPort,
	externalService ports.ExternalServicePort,
) *TraceApplicationServiceImpl[T] {

	service := &TraceApplicationServiceImpl[T]{
		repository:      repository,
		eventStore:      eventStore,
		cache:           cache,
		publisher:       publisher,
		metrics:         metricsCollector,
		config:          config,
		logger:          logger,
		externalService: externalService,
		shutdownChan:    make(chan struct{}),
	}

	// Initialize pools for performance
	service.initializePools()

	// Initialize command and query buses
	service.commandBus = NewCommandBus[T](service)
	service.queryBus = NewQueryBus[T](service)
	service.eventBus = NewEventBus(service)

	// Initialize policies and strategies
	service.initializePolicies()

	// Initialize monitoring
	service.healthMonitor = NewHealthMonitor(service)
	service.metrics = NewServiceMetrics()

	// Initialize background task manager
	service.backgroundTasks = NewBackgroundTaskManager()

	// Start background processes
	go service.startBackgroundProcessing()

	return service
}

// StartTrace implements the primary port for starting a new trace
func (s *TraceApplicationServiceImpl[T]) StartTrace(
	ctx context.Context,
	request ports.StartTraceRequest[T],
) (*ports.TraceSession[T], error) {

	startTime := time.Now()
	defer func() {
		s.metrics.RecordOperation("start_trace", time.Since(startTime))
	}()

	// Validate request
	if err := s.validateStartTraceRequest(request); err != nil {
		s.logger.LogError(ctx, ErrorLogEvent{
			Operation: "start_trace",
			Error:     err,
			Request:   request,
		})
		return nil, fmt.Errorf("invalid start trace request: %w", err)
	}

	// Apply sampling decision
	samplingDecision := s.makeSamplingDecision(ctx, request)
	if !samplingDecision.Sample {
		s.metrics.RecordSamplingDecision("rejected", samplingDecision.Rate)
		return s.createNonSampledSession(ctx, request), nil
	}

	// Generate trace ID
	traceID := s.generateTraceID()

	// Create root span
	rootSpan, err := s.createRootSpan(ctx, traceID, request)
	if err != nil {
		s.logger.LogError(ctx, ErrorLogEvent{
			Operation: "create_root_span",
			TraceID:   traceID,
			Error:     err,
		})
		return nil, fmt.Errorf("failed to create root span: %w", err)
	}

	// Create trace session
	session := &ports.TraceSession[T]{
		TraceID:      traceID,
		RootSpan:     rootSpan,
		Context:      ctx,
		StartTime:    startTime,
		Metadata:     request.Metadata,
		SamplingRate: samplingDecision.Rate,
	}

	// Store active session
	s.activeTraces.Store(traceID, session)

	// Publish trace started event
	s.publishTraceEvent(ctx, TraceEventMessage{
		EventType: "trace_started",
		TraceID:   traceID,
		Payload:   s.serializeTraceSession(session),
		Timestamp: startTime,
	})

	// Record metrics
	s.metrics.RecordTraceStarted(ctx, TraceStartedInfo{
		TraceID:      traceID,
		ServiceName:  request.ServiceName,
		SamplingRate: samplingDecision.Rate,
	})

	s.logger.LogTrace(ctx, TraceLogEvent{
		Operation: "trace_started",
		TraceID:   traceID,
		Metadata: map[string]any{
			"service_name":   request.ServiceName,
			"sampling_rate":  samplingDecision.Rate,
			"root_span_name": request.TraceName,
		},
	})

	return session, nil
}

// CreateSpan implements the primary port for creating spans within a trace
func (s *TraceApplicationServiceImpl[T]) CreateSpan(
	ctx context.Context,
	request ports.CreateSpanRequest[T],
) (domain.Span[T], error) {

	startTime := time.Now()
	defer func() {
		s.metrics.RecordOperation("create_span", time.Since(startTime))
	}()

	// Validate request
	if err := s.validateCreateSpanRequest(request); err != nil {
		return nil, fmt.Errorf("invalid create span request: %w", err)
	}

	// Check if trace exists and is sampled
	session, exists := s.getTraceSession(request.TraceID)
	if !exists {
		// Create span in non-sampled context
		return s.createNonSampledSpan(ctx, request), nil
	}

	// Create span using domain logic
	span, err := s.createSpanInContext(ctx, session, request)
	if err != nil {
		s.logger.LogError(ctx, ErrorLogEvent{
			Operation: "create_span",
			TraceID:   request.TraceID,
			Error:     err,
		})
		return nil, fmt.Errorf("failed to create span: %w", err)
	}

	// Apply correlation analysis
	go s.analyzeSpanCorrelations(ctx, span)

	// Publish span created event
	s.publishTraceEvent(ctx, TraceEventMessage{
		EventType: "span_created",
		TraceID:   request.TraceID,
		SpanID:    &span.GetSpanID(),
		Payload:   s.serializeSpan(span),
		Timestamp: startTime,
	})

	s.logger.LogSpan(ctx, SpanLogEvent{
		Operation: "span_created",
		TraceID:   request.TraceID,
		SpanID:    span.GetSpanID(),
		SpanName:  request.SpanName,
	})

	return span, nil
}

// CreateSpanBatch implements efficient batch span creation
func (s *TraceApplicationServiceImpl[T]) CreateSpanBatch(
	ctx context.Context,
	requests []ports.CreateSpanRequest[T],
) ([]domain.Span[T], error) {

	if len(requests) == 0 {
		return nil, nil
	}

	startTime := time.Now()
	defer func() {
		s.metrics.RecordBatchOperation("create_span_batch", len(requests), time.Since(startTime))
	}()

	// Group requests by trace ID for efficient processing
	requestsByTrace := s.groupRequestsByTrace(requests)

	var spans []domain.Span[T]
	var errors []error

	// Process each trace group
	for traceID, traceRequests := range requestsByTrace {
		traceSpans, err := s.createSpansForTrace(ctx, traceID, traceRequests)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to create spans for trace %s: %w", traceID, err))
			continue
		}
		spans = append(spans, traceSpans...)
	}

	// Handle partial failures
	if len(errors) > 0 && len(spans) == 0 {
		return nil, fmt.Errorf("failed to create any spans: %v", errors)
	}

	// Publish batch event
	s.publishTraceEvent(ctx, TraceEventMessage{
		EventType: "span_batch_created",
		Payload:   s.serializeSpanBatch(spans),
		Timestamp: startTime,
		Metadata: map[string]string{
			"batch_size":    fmt.Sprintf("%d", len(requests)),
			"success_count": fmt.Sprintf("%d", len(spans)),
			"error_count":   fmt.Sprintf("%d", len(errors)),
		},
	})

	return spans, nil
}

// ProcessSpanBatch implements batch processing for completed spans
func (s *TraceApplicationServiceImpl[T]) ProcessSpanBatch(
	ctx context.Context,
	spans []domain.SpanSnapshot[T],
) (*ports.BatchProcessingResult, error) {

	if len(spans) == 0 {
		return &ports.BatchProcessingResult{}, nil
	}

	startTime := time.Now()
	defer func() {
		s.metrics.RecordBatchOperation("process_span_batch", len(spans), time.Since(startTime))
	}()

	result := &ports.BatchProcessingResult{
		ProcessedCount: 0,
		FailedCount:    0,
		Errors:         []ProcessingError{},
		Duration:       0,
	}

	// Use circuit breaker for resilience
	err := s.circuitBreaker.Execute(func() error {
		return s.processBatchWithRetry(ctx, spans, result)
	})

	if err != nil {
		s.logger.LogError(ctx, ErrorLogEvent{
			Operation: "process_span_batch",
			Error:     err,
			Metadata: map[string]any{
				"batch_size": len(spans),
			},
		})
		return result, fmt.Errorf("batch processing failed: %w", err)
	}

	result.Duration = time.Since(startTime)
	result.ThroughputOps = float64(result.ProcessedCount) / result.Duration.Seconds()

	// Update trace sessions for completed spans
	s.updateTraceSessions(ctx, spans)

	// Trigger correlation analysis
	go s.analyzeBatchCorrelations(ctx, spans)

	return result, nil
}

// Query operations (CQRS read side)

// GetTrace implements trace retrieval with caching and optimization
func (s *TraceApplicationServiceImpl[T]) GetTrace(
	ctx context.Context,
	traceID domain.TraceID,
) (*ports.TraceAggregateView[T], error) {

	startTime := time.Now()
	defer func() {
		s.metrics.RecordOperation("get_trace", time.Since(startTime))
	}()

	// Check cache first
	if cachedTrace, err := s.cache.GetTrace(ctx, traceID); err == nil {
		s.metrics.RecordCacheHit("get_trace")
		return cachedTrace, nil
	}
	s.metrics.RecordCacheMiss("get_trace")

	// Query repository for spans
	query := ports.SpanQuery{
		TraceIDs: []domain.TraceID{traceID},
		Limit:    1000, // Reasonable limit for single trace
	}

	spans, err := s.repository.FindSpans(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to find spans for trace: %w", err)
	}

	if len(spans) == 0 {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}

	// Build aggregate view
	aggregateView := s.buildTraceAggregateView(ctx, traceID, spans)

	// Cache the result
	cacheTTL, _ := s.config.GetConfiguration(ctx, "trace_cache_ttl")
	s.cache.SetTrace(ctx, traceID, aggregateView, cacheTTL.Value.(time.Duration))

	return aggregateView, nil
}

// QuerySpans implements optimized span querying with pagination
func (s *TraceApplicationServiceImpl[T]) QuerySpans(
	ctx context.Context,
	query ports.SpanQuery,
) (*ports.SpanQueryResult[T], error) {

	startTime := time.Now()
	defer func() {
		s.metrics.RecordOperation("query_spans", time.Since(startTime))
	}()

	// Validate and optimize query
	optimizedQuery, err := s.optimizeSpanQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to optimize query: %w", err)
	}

	// Execute query with timeout
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	spans, err := s.repository.FindSpans(queryCtx, optimizedQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute span query: %w", err)
	}

	// Count total results for pagination
	totalCount, err := s.repository.CountSpans(ctx, convertToSpanFilter(optimizedQuery))
	if err != nil {
		s.logger.LogError(ctx, ErrorLogEvent{
			Operation: "count_spans",
			Error:     err,
		})
		// Continue without total count
		totalCount = int64(len(spans))
	}

	// Apply post-processing filters
	filteredSpans := s.applyPostProcessingFilters(ctx, spans, query)

	result := &ports.SpanQueryResult[T]{
		Spans:      filteredSpans,
		TotalCount: totalCount,
		HasMore:    int64(len(filteredSpans)) < totalCount,
		NextCursor: s.generateNextCursor(query, filteredSpans),
		QueryTime:  time.Since(startTime),
	}

	return result, nil
}

// Health and monitoring

// GetServiceHealth implements health checking across all components
func (s *TraceApplicationServiceImpl[T]) GetServiceHealth(
	ctx context.Context,
) (*ports.ServiceHealth, error) {

	healthChecks := []HealthCheck{
		{Name: "repository", Checker: s.repository},
		{Name: "event_store", Checker: s.eventStore},
		{Name: "cache", Checker: s.cache},
		{Name: "publisher", Checker: s.publisher},
		{Name: "external_service", Checker: s.externalService},
	}

	return s.healthMonitor.CheckHealth(ctx, healthChecks)
}

// GetPerformanceMetrics implements performance monitoring
func (s *TraceApplicationServiceImpl[T]) GetPerformanceMetrics(
	ctx context.Context,
) (*ports.PerformanceMetrics, error) {

	return s.metrics.GetPerformanceMetrics(ctx)
}

// Private implementation methods

func (s *TraceApplicationServiceImpl[T]) validateStartTraceRequest(
	request ports.StartTraceRequest[T],
) error {
	if request.TraceName == "" {
		return fmt.Errorf("trace name is required")
	}
	if request.ServiceName == "" {
		return fmt.Errorf("service name is required")
	}
	return nil
}

func (s *TraceApplicationServiceImpl[T]) validateCreateSpanRequest(
	request ports.CreateSpanRequest[T],
) error {
	if request.TraceID == (domain.TraceID{}) {
		return fmt.Errorf("trace ID is required")
	}
	if request.SpanName == "" {
		return fmt.Errorf("span name is required")
	}
	return nil
}

func (s *TraceApplicationServiceImpl[T]) makeSamplingDecision(
	ctx context.Context,
	request ports.StartTraceRequest[T],
) *ports.SamplingDecision {

	if s.samplingService != nil {
		return s.samplingService.ShouldSampleRoot(ctx, s.generateTraceID(), request.TraceName)
	}

	// Default sampling decision
	return &ports.SamplingDecision{
		Sample: true,
		Rate:   1.0,
		Reason: "default_sampling",
	}
}

func (s *TraceApplicationServiceImpl[T]) createRootSpan(
	ctx context.Context,
	traceID domain.TraceID,
	request ports.StartTraceRequest[T],
) (domain.Span[T], error) {

	spanRequest := ports.CreateSpanRequest[T]{
		TraceID:    traceID,
		SpanName:   request.TraceName,
		SpanKind:   request.SpanKind,
		Attributes: request.Attributes,
		StartTime:  &request.StartTime,
	}

	return s.createSpanInContext(ctx, nil, spanRequest)
}

func (s *TraceApplicationServiceImpl[T]) getTraceSession(
	traceID domain.TraceID,
) (*ports.TraceSession[T], bool) {

	value, exists := s.activeTraces.Load(traceID)
	if !exists {
		return nil, false
	}

	session, ok := value.(*ports.TraceSession[T])
	return session, ok
}

func (s *TraceApplicationServiceImpl[T]) generateTraceID() domain.TraceID {
	// Implementation would generate a proper trace ID
	var traceID domain.TraceID
	// Use crypto/rand or similar to generate random bytes
	return traceID
}

func (s *TraceApplicationServiceImpl[T]) initializePools() {
	s.spanPool = sync.Pool{
		New: func() interface{} {
			return &mockSpan[T]{}
		},
	}

	s.contextPool = sync.Pool{
		New: func() interface{} {
			return make(map[string]any)
		},
	}
}

func (s *TraceApplicationServiceImpl[T]) initializePolicies() {
	s.tracingPolicy = NewTracingPolicy()
	s.retryPolicy = NewRetryPolicy()
	s.circuitBreaker = NewCircuitBreaker()
}

func (s *TraceApplicationServiceImpl[T]) startBackgroundProcessing() {
	// Background tasks like cleanup, health monitoring, etc.
	s.backgroundTasks.Start([]BackgroundTask{
		NewTraceCleanupTask(s),
		NewHealthMonitoringTask(s),
		NewMetricsAggregationTask(s),
	})
}

// Additional private methods would be implemented here...

// Mock implementations for compilation
type mockSpan[T domain.TraceData] struct{}

func (m *mockSpan[T]) GetSpanID() domain.SpanID   { return domain.SpanID{} }
func (m *mockSpan[T]) GetTraceID() domain.TraceID { return domain.TraceID{} }

// Shutdown implements graceful shutdown
func (s *TraceApplicationServiceImpl[T]) Shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() {
		close(s.shutdownChan)
		s.backgroundTasks.Stop()

		// Flush pending operations
		s.flushPendingOperations(ctx)
	})
	return nil
}

// Additional supporting types and methods would be defined here...
