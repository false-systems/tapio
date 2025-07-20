package cqrs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/integrations/otel/domain"
)

// QueryBus orchestrates query processing in the CQRS pattern
// Provides optimized read operations with caching, projections, and real-time capabilities
type QueryBus[T domain.TraceData] struct {
	// Query handlers registry
	handlers    map[QueryType]QueryHandler[T]
	handlersMux sync.RWMutex

	// Middleware pipeline for queries
	middleware []QueryMiddleware[T]

	// Read model management
	readModelManager  *ReadModelManager[T]
	projectionManager *ProjectionManager[T]

	// Caching layer
	cacheManager *QueryCacheManager[T]

	// Real-time query support
	streamManager *QueryStreamManager[T]

	// Performance optimization
	queryOptimizer *QueryOptimizer[T]
	metrics        *QueryMetrics

	// Configuration
	config QueryBusConfig

	// Circuit breaker for resilience
	circuitBreaker *CircuitBreaker

	// Query validation
	validator *QueryValidator[T]

	// Materialized views
	viewManager *MaterializedViewManager[T]
}

// QueryHandler defines the interface for handling specific queries
type QueryHandler[T domain.TraceData] interface {
	Handle(ctx context.Context, query Query[T]) (*QueryResult[T], error)
	CanHandle(queryType QueryType) bool
	GetHandlerInfo() QueryHandlerInfo
	GetCacheKey(query Query[T]) string
	GetCacheTTL(query Query[T]) time.Duration
}

// Query represents a query in the CQRS pattern with type safety
type Query[T domain.TraceData] interface {
	GetQueryID() string
	GetQueryType() QueryType
	GetParameters() map[string]any
	GetFilters() []QueryFilter
	GetSorting() []SortCriteria
	GetPagination() *Pagination
	GetProjection() []string
	GetTimeRange() *TimeRange
	Validate() error
	EstimateComplexity() QueryComplexity
}

// QueryResult represents the result of query execution
type QueryResult[T domain.TraceData] struct {
	QueryID       string
	Data          any
	TotalCount    int64
	HasMore       bool
	NextCursor    string
	ExecutionTime time.Duration
	CacheHit      bool
	Source        QuerySource
	Metadata      map[string]any
}

// QueryMiddleware defines middleware for query processing pipeline
type QueryMiddleware[T domain.TraceData] interface {
	Execute(ctx context.Context, query Query[T], next QueryHandler[T]) (*QueryResult[T], error)
	GetMiddlewareName() string
	GetOrder() int
}

// QueryBusConfig configures the query bus behavior
type QueryBusConfig struct {
	// Caching configuration
	EnableCaching           bool
	DefaultCacheTTL         time.Duration
	MaxCacheSize            int64
	CacheCompressionEnabled bool

	// Performance configuration
	QueryTimeout            time.Duration
	SlowQueryThreshold      time.Duration
	MaxConcurrentQueries    int
	EnableQueryOptimization bool

	// Real-time configuration
	EnableStreaming  bool
	StreamBufferSize int
	StreamTimeout    time.Duration

	// Read model configuration
	EnableReadModels         bool
	ReadModelRefreshInterval time.Duration

	// Materialized views
	EnableMaterializedViews bool
	ViewRefreshInterval     time.Duration

	// Circuit breaker configuration
	EnableCircuitBreaker bool
	FailureThreshold     int
	RecoveryTimeout      time.Duration

	// Monitoring configuration
	EnableMetrics      bool
	EnableQueryLogging bool
}

// NewQueryBus creates a new query bus with configuration
func NewQueryBus[T domain.TraceData](
	config QueryBusConfig,
	cacheManager *QueryCacheManager[T],
	readModelManager *ReadModelManager[T],
) *QueryBus[T] {

	applyQueryBusDefaults(&config)

	bus := &QueryBus[T]{
		handlers:         make(map[QueryType]QueryHandler[T]),
		middleware:       make([]QueryMiddleware[T], 0),
		readModelManager: readModelManager,
		cacheManager:     cacheManager,
		config:           config,
		metrics:          NewQueryMetrics(),
		validator:        NewQueryValidator[T](),
		queryOptimizer:   NewQueryOptimizer[T](),
	}

	// Initialize projection manager if read models enabled
	if config.EnableReadModels {
		bus.projectionManager = NewProjectionManager[T](ProjectionConfig{
			RefreshInterval: config.ReadModelRefreshInterval,
		})
	}

	// Initialize stream manager if streaming enabled
	if config.EnableStreaming {
		bus.streamManager = NewQueryStreamManager[T](StreamConfig{
			BufferSize: config.StreamBufferSize,
			Timeout:    config.StreamTimeout,
		})
	}

	// Initialize materialized view manager if enabled
	if config.EnableMaterializedViews {
		bus.viewManager = NewMaterializedViewManager[T](ViewConfig{
			RefreshInterval: config.ViewRefreshInterval,
		})
	}

	// Initialize circuit breaker if enabled
	if config.EnableCircuitBreaker {
		bus.circuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
			FailureThreshold: config.FailureThreshold,
			RecoveryTimeout:  config.RecoveryTimeout,
		})
	}

	// Register default middleware
	bus.registerDefaultMiddleware()

	return bus
}

// RegisterHandler registers a query handler for a specific query type
func (bus *QueryBus[T]) RegisterHandler(queryType QueryType, handler QueryHandler[T]) error {
	bus.handlersMux.Lock()
	defer bus.handlersMux.Unlock()

	if _, exists := bus.handlers[queryType]; exists {
		return fmt.Errorf("handler already registered for query type: %s", queryType)
	}

	bus.handlers[queryType] = handler

	return nil
}

// Execute executes a query through the optimized pipeline
func (bus *QueryBus[T]) Execute(ctx context.Context, query Query[T]) (*QueryResult[T], error) {
	startTime := time.Now()
	queryID := query.GetQueryID()
	queryType := query.GetQueryType()

	// Record query execution attempt
	bus.metrics.RecordQueryAttempt(queryType)

	// Validate query
	if err := bus.validator.Validate(query); err != nil {
		bus.metrics.RecordQueryFailure(queryType, "validation_error")
		return &QueryResult[T]{
			QueryID:  queryID,
			Metadata: map[string]any{"error": err.Error()},
		}, fmt.Errorf("query validation failed: %w", err)
	}

	// Optimize query if enabled
	optimizedQuery := query
	if bus.config.EnableQueryOptimization {
		var err error
		optimizedQuery, err = bus.queryOptimizer.Optimize(ctx, query)
		if err != nil {
			// Continue with original query if optimization fails
			optimizedQuery = query
		}
	}

	// Use circuit breaker if enabled
	if bus.config.EnableCircuitBreaker {
		return bus.executeWithCircuitBreaker(ctx, optimizedQuery, startTime)
	}

	// Execute query through middleware pipeline
	return bus.executeWithMiddleware(ctx, optimizedQuery, startTime)
}

// ExecuteStream executes a streaming query for real-time data
func (bus *QueryBus[T]) ExecuteStream(
	ctx context.Context,
	query StreamQuery[T],
) (<-chan *QueryResult[T], error) {

	if !bus.config.EnableStreaming {
		return nil, fmt.Errorf("streaming not enabled")
	}

	if bus.streamManager == nil {
		return nil, fmt.Errorf("stream manager not initialized")
	}

	// Validate streaming query
	if err := bus.validator.ValidateStreamQuery(query); err != nil {
		return nil, fmt.Errorf("stream query validation failed: %w", err)
	}

	// Start streaming query
	resultChan, err := bus.streamManager.StartStream(ctx, query)
	if err != nil {
		bus.metrics.RecordStreamFailure(query.GetQueryType(), "start_failed")
		return nil, fmt.Errorf("failed to start stream: %w", err)
	}

	bus.metrics.RecordStreamStarted(query.GetQueryType())
	return resultChan, nil
}

// ExecuteBatch executes multiple queries efficiently
func (bus *QueryBus[T]) ExecuteBatch(
	ctx context.Context,
	queries []Query[T],
) (*BatchQueryResult[T], error) {

	if len(queries) == 0 {
		return &BatchQueryResult[T]{}, nil
	}

	startTime := time.Now()
	batchID := generateQueryBatchID()

	bus.metrics.RecordBatchQueryAttempt(len(queries))

	result := &BatchQueryResult[T]{
		BatchID:   batchID,
		Queries:   len(queries),
		Results:   make([]*QueryResult[T], 0, len(queries)),
		StartTime: startTime,
	}

	// Group queries by type for optimization
	queryGroups := bus.groupQueriesByType(queries)

	// Execute query groups concurrently
	resultsChan := make(chan *QueryResult[T], len(queries))
	errorsChan := make(chan error, len(queries))

	var wg sync.WaitGroup
	for queryType, typeQueries := range queryGroups {
		wg.Add(1)
		go func(qt QueryType, qs []Query[T]) {
			defer wg.Done()
			bus.executeBatchGroup(ctx, qt, qs, resultsChan, errorsChan)
		}(queryType, typeQueries)
	}

	// Wait for all groups to complete
	go func() {
		wg.Wait()
		close(resultsChan)
		close(errorsChan)
	}()

	// Collect results
	for queryResult := range resultsChan {
		result.Results = append(result.Results, queryResult)
		result.SuccessCount++
	}

	// Collect errors
	for err := range errorsChan {
		if err != nil {
			result.FailureCount++
		}
	}

	result.Duration = time.Since(startTime)
	bus.metrics.RecordBatchQueryComplete(result.SuccessCount, result.FailureCount, result.Duration)

	return result, nil
}

// GetReadModel retrieves a read model for optimized queries
func (bus *QueryBus[T]) GetReadModel(
	ctx context.Context,
	modelName string,
	version *int64,
) (*ReadModel[T], error) {

	if !bus.config.EnableReadModels {
		return nil, fmt.Errorf("read models not enabled")
	}

	if bus.readModelManager == nil {
		return nil, fmt.Errorf("read model manager not initialized")
	}

	return bus.readModelManager.GetModel(ctx, modelName, version)
}

// RefreshMaterializedView refreshes a materialized view
func (bus *QueryBus[T]) RefreshMaterializedView(
	ctx context.Context,
	viewName string,
) error {

	if !bus.config.EnableMaterializedViews {
		return fmt.Errorf("materialized views not enabled")
	}

	if bus.viewManager == nil {
		return fmt.Errorf("view manager not initialized")
	}

	return bus.viewManager.RefreshView(ctx, viewName)
}

// Private implementation methods

func (bus *QueryBus[T]) getHandler(queryType QueryType) (QueryHandler[T], error) {
	bus.handlersMux.RLock()
	defer bus.handlersMux.RUnlock()

	handler, exists := bus.handlers[queryType]
	if !exists {
		return nil, fmt.Errorf("no handler registered for query type: %s", queryType)
	}

	return handler, nil
}

func (bus *QueryBus[T]) executeWithCircuitBreaker(
	ctx context.Context,
	query Query[T],
	startTime time.Time,
) (*QueryResult[T], error) {

	var result *QueryResult[T]
	var err error

	cbErr := bus.circuitBreaker.Execute(func() error {
		result, err = bus.executeWithMiddleware(ctx, query, startTime)
		return err
	})

	if cbErr != nil {
		bus.metrics.RecordQueryFailure(query.GetQueryType(), "circuit_breaker_open")
		return &QueryResult[T]{
			QueryID:  query.GetQueryID(),
			Metadata: map[string]any{"error": cbErr.Error()},
		}, cbErr
	}

	return result, err
}

func (bus *QueryBus[T]) executeWithMiddleware(
	ctx context.Context,
	query Query[T],
	startTime time.Time,
) (*QueryResult[T], error) {

	// Get handler
	handler, err := bus.getHandler(query.GetQueryType())
	if err != nil {
		bus.metrics.RecordQueryFailure(query.GetQueryType(), "no_handler")
		return &QueryResult[T]{
			QueryID:  query.GetQueryID(),
			Metadata: map[string]any{"error": err.Error()},
		}, err
	}

	// Build middleware chain
	var finalHandler QueryHandler[T] = &finalQueryHandler[T]{
		handler:   handler,
		bus:       bus,
		startTime: startTime,
	}

	// Apply middleware in reverse order
	for i := len(bus.middleware) - 1; i >= 0; i-- {
		finalHandler = &queryMiddlewareHandler[T]{
			middleware: bus.middleware[i],
			next:       finalHandler,
		}
	}

	// Execute query through middleware chain
	result, err := finalHandler.Handle(ctx, query)

	// Record execution metrics
	executionTime := time.Since(startTime)
	bus.metrics.RecordQueryExecution(query.GetQueryType(), executionTime, err == nil)

	// Log slow queries
	if executionTime > bus.config.SlowQueryThreshold {
		bus.logSlowQuery(query, executionTime)
	}

	if result != nil {
		result.ExecutionTime = executionTime
	}

	return result, err
}

func (bus *QueryBus[T]) executeBatchGroup(
	ctx context.Context,
	queryType QueryType,
	queries []Query[T],
	resultsChan chan<- *QueryResult[T],
	errorsChan chan<- error,
) {

	// Get handler for this query type
	handler, err := bus.getHandler(queryType)
	if err != nil {
		for range queries {
			errorsChan <- err
		}
		return
	}

	// Execute queries of same type together for optimization
	for _, query := range queries {
		result, err := bus.executeWithMiddleware(ctx, query, time.Now())
		if err != nil {
			errorsChan <- err
		} else {
			resultsChan <- result
		}
	}
}

func (bus *QueryBus[T]) groupQueriesByType(queries []Query[T]) map[QueryType][]Query[T] {
	groups := make(map[QueryType][]Query[T])

	for _, query := range queries {
		queryType := query.GetQueryType()
		groups[queryType] = append(groups[queryType], query)
	}

	return groups
}

func (bus *QueryBus[T]) registerDefaultMiddleware() {
	// Register built-in middleware
	bus.AddMiddleware(NewQueryLoggingMiddleware[T]())
	bus.AddMiddleware(NewQueryValidationMiddleware[T]())
	bus.AddMiddleware(NewQueryCachingMiddleware[T](bus.cacheManager))
	bus.AddMiddleware(NewQueryMetricsMiddleware[T](bus.metrics))
	bus.AddMiddleware(NewQueryOptimizationMiddleware[T](bus.queryOptimizer))
}

func (bus *QueryBus[T]) logSlowQuery(query Query[T], executionTime time.Duration) {
	if bus.config.EnableQueryLogging {
		// Log slow query for performance analysis
		fmt.Printf("Slow query detected: Type=%s, ID=%s, Duration=%v\n",
			query.GetQueryType(),
			query.GetQueryID(),
			executionTime,
		)
	}
}

// AddMiddleware adds middleware to the query processing pipeline
func (bus *QueryBus[T]) AddMiddleware(middleware QueryMiddleware[T]) {
	bus.middleware = append(bus.middleware, middleware)

	// Sort middleware by order
	bus.sortMiddleware()
}

func (bus *QueryBus[T]) sortMiddleware() {
	// Sort middleware by order (lower numbers execute first)
	for i := 0; i < len(bus.middleware)-1; i++ {
		for j := i + 1; j < len(bus.middleware); j++ {
			if bus.middleware[i].GetOrder() > bus.middleware[j].GetOrder() {
				bus.middleware[i], bus.middleware[j] = bus.middleware[j], bus.middleware[i]
			}
		}
	}
}

// Supporting types for query execution

// finalQueryHandler wraps the actual query handler
type finalQueryHandler[T domain.TraceData] struct {
	handler   QueryHandler[T]
	bus       *QueryBus[T]
	startTime time.Time
}

func (h *finalQueryHandler[T]) Handle(ctx context.Context, query Query[T]) (*QueryResult[T], error) {
	return h.handler.Handle(ctx, query)
}

func (h *finalQueryHandler[T]) CanHandle(queryType QueryType) bool {
	return h.handler.CanHandle(queryType)
}

func (h *finalQueryHandler[T]) GetHandlerInfo() QueryHandlerInfo {
	return h.handler.GetHandlerInfo()
}

func (h *finalQueryHandler[T]) GetCacheKey(query Query[T]) string {
	return h.handler.GetCacheKey(query)
}

func (h *finalQueryHandler[T]) GetCacheTTL(query Query[T]) time.Duration {
	return h.handler.GetCacheTTL(query)
}

// queryMiddlewareHandler wraps middleware execution
type queryMiddlewareHandler[T domain.TraceData] struct {
	middleware QueryMiddleware[T]
	next       QueryHandler[T]
}

func (h *queryMiddlewareHandler[T]) Handle(ctx context.Context, query Query[T]) (*QueryResult[T], error) {
	return h.middleware.Execute(ctx, query, h.next)
}

func (h *queryMiddlewareHandler[T]) CanHandle(queryType QueryType) bool {
	return h.next.CanHandle(queryType)
}

func (h *queryMiddlewareHandler[T]) GetHandlerInfo() QueryHandlerInfo {
	return h.next.GetHandlerInfo()
}

func (h *queryMiddlewareHandler[T]) GetCacheKey(query Query[T]) string {
	return h.next.GetCacheKey(query)
}

func (h *queryMiddlewareHandler[T]) GetCacheTTL(query Query[T]) time.Duration {
	return h.next.GetCacheTTL(query)
}

// Supporting types and interfaces

type QueryType string
type QueryComplexity int
type QuerySource string

const (
	QueryTypeGetSpan       QueryType = "get_span"
	QueryTypeGetTrace      QueryType = "get_trace"
	QueryTypeFindSpans     QueryType = "find_spans"
	QueryTypeGetMetrics    QueryType = "get_metrics"
	QueryTypeGetAggregates QueryType = "get_aggregates"
)

const (
	QueryComplexityLow    QueryComplexity = 1
	QueryComplexityMedium QueryComplexity = 2
	QueryComplexityHigh   QueryComplexity = 3
)

const (
	QuerySourceCache     QuerySource = "cache"
	QuerySourceDatabase  QuerySource = "database"
	QuerySourceReadModel QuerySource = "read_model"
	QuerySourceView      QuerySource = "materialized_view"
)

type QueryHandlerInfo struct {
	Name              string
	Version           string
	Description       string
	SupportedQueries  []QueryType
	CachingStrategy   string
	OptimizationHints []string
}

type QueryFilter struct {
	Field    string
	Operator string
	Value    any
}

type SortCriteria struct {
	Field     string
	Direction string // "asc" or "desc"
}

type Pagination struct {
	Offset int
	Limit  int
	Cursor string
}

type TimeRange struct {
	Start time.Time
	End   time.Time
}

type StreamQuery[T domain.TraceData] interface {
	Query[T]
	GetStreamConfig() StreamConfig
	GetUpdateInterval() time.Duration
}

type BatchQueryResult[T domain.TraceData] struct {
	BatchID      string
	Queries      int
	Results      []*QueryResult[T]
	SuccessCount int
	FailureCount int
	Duration     time.Duration
	StartTime    time.Time
}

type ReadModel[T domain.TraceData] struct {
	Name        string
	Version     int64
	Data        any
	LastUpdated time.Time
	Metadata    map[string]any
}

// Helper functions

func applyQueryBusDefaults(config *QueryBusConfig) {
	if config.DefaultCacheTTL == 0 {
		config.DefaultCacheTTL = 5 * time.Minute
	}
	if config.MaxCacheSize == 0 {
		config.MaxCacheSize = 100 * 1024 * 1024 // 100MB
	}
	if config.QueryTimeout == 0 {
		config.QueryTimeout = 30 * time.Second
	}
	if config.SlowQueryThreshold == 0 {
		config.SlowQueryThreshold = time.Second
	}
	if config.MaxConcurrentQueries == 0 {
		config.MaxConcurrentQueries = 100
	}
	if config.StreamBufferSize == 0 {
		config.StreamBufferSize = 1000
	}
	if config.StreamTimeout == 0 {
		config.StreamTimeout = 30 * time.Second
	}
	if config.ReadModelRefreshInterval == 0 {
		config.ReadModelRefreshInterval = time.Minute
	}
	if config.ViewRefreshInterval == 0 {
		config.ViewRefreshInterval = 5 * time.Minute
	}
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}
	if config.RecoveryTimeout == 0 {
		config.RecoveryTimeout = 30 * time.Second
	}
}

func generateQueryBatchID() string {
	return fmt.Sprintf("query_batch_%d", time.Now().UnixNano())
}

// Additional supporting types would be defined here...
