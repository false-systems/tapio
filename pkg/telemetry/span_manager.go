package telemetry

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/yairfalse/tapio/pkg/resilience"
)

// SpanEvent represents an event that occurred during a span
type SpanEvent struct {
	Name       string
	Attributes []attribute.KeyValue
	Time       time.Time
}

// SpanManager handles span creation, lifecycle, and export with full resilience
type SpanManager struct {
	exporter        *OpenTelemetryExporter
	circuitBreaker  *resilience.CircuitBreaker
	timeoutManager  *resilience.TimeoutManager
	validator       *resilience.SchemaValidator
	boundedExecutor *resilience.BoundedExecutor

	// Resource efficiency and pooling
	spanPool       *sync.Pool
	attributePool  *sync.Pool
	activeSpans    sync.Map // map[string]*ManagedSpan
	spanQueue      chan *SpanRequest
	batchProcessor *BatchProcessor

	// Metrics and monitoring
	spansCreated    atomic.Int64
	spansCompleted  atomic.Int64
	spansExported   atomic.Int64
	spansFailed     atomic.Int64
	batchesExported atomic.Int64

	// Configuration
	config SpanManagerConfig

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// SpanManagerConfig configures the span manager
type SpanManagerConfig struct {
	MaxConcurrentSpans int
	BatchSize          int
	BatchTimeout       time.Duration
	ExportTimeout      time.Duration
	MaxQueueSize       int
	EnableValidation   bool
	ResourceLimits     ResourceLimits
}

// ManagedSpan wraps OpenTelemetry spans with resilience features
type ManagedSpan struct {
	trace.Span
	id         string
	startTime  time.Time
	attributes []attribute.KeyValue
	events     []SpanEvent
	mu         sync.RWMutex
	exported   bool
	failed     bool
}

// SpanRequest represents a span creation request
type SpanRequest struct {
	name       string
	attributes []attribute.KeyValue
	options    []trace.SpanStartOption
	ctx        context.Context
	resultCh   chan SpanResult
}

// SpanResult contains the result of span creation
type SpanResult struct {
	span *ManagedSpan
	err  error
}

// BatchProcessor handles efficient span batching and export
type BatchProcessor struct {
	spans      []*ManagedSpan
	batchSize  int
	timeout    time.Duration
	lastFlush  time.Time
	mu         sync.Mutex
	flushTimer *time.Timer
}

// NewSpanManager creates a new resilient span manager
func NewSpanManager(exporter *OpenTelemetryExporter, config SpanManagerConfig) (*SpanManager, error) {
	// Set defaults
	if config.MaxConcurrentSpans == 0 {
		config.MaxConcurrentSpans = 1000
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 5 * time.Second
	}
	if config.ExportTimeout == 0 {
		config.ExportTimeout = 10 * time.Second
	}
	if config.MaxQueueSize == 0 {
		config.MaxQueueSize = 10000
	}

	// Create resilience components
	circuitBreaker := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		Name:             "span-export",
		MaxFailures:      5,
		ResetTimeout:     30 * time.Second,
		HalfOpenMaxCalls: 2,
		OnStateChange: func(oldState, newState resilience.State) {
			fmt.Printf("[SPAN-MGR] Export circuit breaker: %s -> %s\n", oldState, newState)
		},
	})

	timeoutManager := resilience.NewTimeoutManager(resilience.TimeoutConfig{
		Timeout: config.ExportTimeout,
		RetryStrategy: &resilience.ExponentialBackoff{
			InitialDelay: 100 * time.Millisecond,
			MaxDelay:     2 * time.Second,
			Multiplier:   2.0,
			Jitter:       true,
		},
		MaxRetries:     3,
		CircuitBreaker: circuitBreaker,
	})

	// Validation rules for spans
	validator, err := resilience.NewSchemaValidator("span-data", []resilience.ValidationRule{
		{
			Field:    "name",
			Required: true,
			Type:     "string",
			Pattern:  "^tapio\\.[a-z_\\.]+$",
		},
		{
			Field: "kind",
			Type:  "string",
			Enum:  []interface{}{"internal", "server", "client", "producer", "consumer"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create span validator: %w", err)
	}

	// Bounded executor for concurrent span processing
	boundedExecutor := resilience.NewBoundedExecutor(config.MaxConcurrentSpans, config.ExportTimeout)

	// Object pools for zero-allocation hot paths
	spanPool := &sync.Pool{
		New: func() interface{} {
			return &ManagedSpan{
				attributes: make([]attribute.KeyValue, 0, 10),
				events:     make([]SpanEvent, 0, 5),
			}
		},
	}

	attributePool := &sync.Pool{
		New: func() interface{} {
			return make([]attribute.KeyValue, 0, 10)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	sm := &SpanManager{
		exporter:        exporter,
		circuitBreaker:  circuitBreaker,
		timeoutManager:  timeoutManager,
		validator:       validator,
		boundedExecutor: boundedExecutor,
		spanPool:        spanPool,
		attributePool:   attributePool,
		spanQueue:       make(chan *SpanRequest, config.MaxQueueSize),
		batchProcessor: &BatchProcessor{
			spans:     make([]*ManagedSpan, 0, config.BatchSize),
			batchSize: config.BatchSize,
			timeout:   config.BatchTimeout,
		},
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}

	// Start background processors
	sm.startProcessors()

	return sm, nil
}

// CreateSpan creates a new managed span with resilience features
func (sm *SpanManager) CreateSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (*ManagedSpan, error) {
	// Validate span name if enabled
	if sm.config.EnableValidation {
		spanData := map[string]interface{}{
			"name": name,
			"kind": "internal", // Default kind
		}
		if err := sm.validator.Validate(ctx, spanData); err != nil {
			sm.spansFailed.Add(1)
			return nil, fmt.Errorf("span validation failed: %w", err)
		}
	}

	// Use bounded executor for span creation
	var managedSpan *ManagedSpan
	err := sm.boundedExecutor.Execute(ctx, func() error {
		// Get span from pool
		managedSpan = sm.spanPool.Get().(*ManagedSpan)
		managedSpan.id = fmt.Sprintf("span-%d", sm.spansCreated.Add(1))
		managedSpan.startTime = time.Now()
		managedSpan.exported = false
		managedSpan.failed = false

		// Create the actual OpenTelemetry span
		_, span := sm.exporter.tracer.Start(ctx, name, opts...)
		managedSpan.Span = span

		// Store in active spans
		sm.activeSpans.Store(managedSpan.id, managedSpan)

		return nil
	})

	if err != nil {
		sm.spansFailed.Add(1)
		return nil, fmt.Errorf("failed to create span: %w", err)
	}

	return managedSpan, nil
}

// FinishSpan completes a span and queues it for export
func (sm *SpanManager) FinishSpan(span *ManagedSpan) {
	span.mu.Lock()
	defer span.mu.Unlock()

	if span.exported {
		return // Already finished
	}

	// End the OpenTelemetry span
	span.End()

	// Remove from active spans
	sm.activeSpans.Delete(span.id)
	sm.spansCompleted.Add(1)

	// Add to batch processor
	sm.batchProcessor.addSpan(span)
}

// addSpan adds a span to the batch processor
func (bp *BatchProcessor) addSpan(span *ManagedSpan) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	bp.spans = append(bp.spans, span)

	// Check if we should flush
	shouldFlush := len(bp.spans) >= bp.batchSize ||
		time.Since(bp.lastFlush) >= bp.timeout

	if shouldFlush {
		bp.flush()
	} else if bp.flushTimer == nil {
		// Set timer for timeout-based flush
		bp.flushTimer = time.AfterFunc(bp.timeout, func() {
			bp.mu.Lock()
			defer bp.mu.Unlock()
			bp.flush()
		})
	}
}

// flush exports the current batch of spans
func (bp *BatchProcessor) flush() {
	if len(bp.spans) == 0 {
		return
	}

	spans := bp.spans
	bp.spans = make([]*ManagedSpan, 0, bp.batchSize)
	bp.lastFlush = time.Now()

	if bp.flushTimer != nil {
		bp.flushTimer.Stop()
		bp.flushTimer = nil
	}

	// Export spans asynchronously
	go func() {
		// This would integrate with the main exporter's export logic
		// For now, just mark as exported
		for _, span := range spans {
			span.mu.Lock()
			span.exported = true
			span.mu.Unlock()
		}
	}()
}

// SetAttribute adds an attribute to a managed span with validation
func (ms *ManagedSpan) SetAttribute(key string, value interface{}) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Convert to OpenTelemetry attribute
	var attr attribute.KeyValue
	switch v := value.(type) {
	case string:
		attr = attribute.String(key, v)
	case int:
		attr = attribute.Int(key, v)
	case int64:
		attr = attribute.Int64(key, v)
	case float64:
		attr = attribute.Float64(key, v)
	case bool:
		attr = attribute.Bool(key, v)
	default:
		attr = attribute.String(key, fmt.Sprintf("%v", v))
	}

	ms.attributes = append(ms.attributes, attr)
	ms.SetAttributes(attr)
}

// AddEvent adds an event to the managed span
func (ms *ManagedSpan) AddEvent(name string, attrs ...attribute.KeyValue) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	event := SpanEvent{
		Name:       name,
		Attributes: attrs,
		Time:       time.Now(),
	}

	ms.events = append(ms.events, event)
	ms.Span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetStatus sets the status of the managed span
func (ms *ManagedSpan) SetStatus(code codes.Code, description string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.Span.SetStatus(code, description)
}

// ExportBatch exports a batch of spans with circuit breaker protection
func (sm *SpanManager) ExportBatch(ctx context.Context, spans []*ManagedSpan) error {
	startTime := time.Now()

	// Use circuit breaker for export
	err := sm.circuitBreaker.Execute(ctx, func() error {
		// Use timeout manager for the export operation
		return sm.timeoutManager.Execute(ctx, "span-batch-export", func(ctx context.Context) error {
			// Convert managed spans to trace spans
			traceSpans := make([]trace.Span, len(spans))
			for i, span := range spans {
				traceSpans[i] = span.Span
			}

			// Export through the main exporter
			if err := sm.exporter.ExportSpans(ctx, traceSpans); err != nil {
				// Mark spans as failed
				for _, span := range spans {
					span.mu.Lock()
					span.failed = true
					span.mu.Unlock()
				}
				return err
			}

			// Mark spans as exported
			for _, span := range spans {
				span.mu.Lock()
				span.exported = true
				span.mu.Unlock()
			}

			sm.spansExported.Add(int64(len(spans)))
			sm.batchesExported.Add(1)

			return nil
		})
	})

	// Record metrics
	exportDuration := time.Since(startTime)
	if sm.exporter.config.EnableMetrics {
		sm.exporter.spanExportDuration.Record(ctx, exportDuration.Seconds())
		sm.exporter.batchSize.Record(ctx, int64(len(spans)))
	}

	return err
}

// startProcessors starts background span processing
func (sm *SpanManager) startProcessors() {
	// Start batch processor
	sm.wg.Add(1)
	go func() {
		defer sm.wg.Done()
		sm.runBatchProcessor()
	}()

	// Start metrics collector
	sm.wg.Add(1)
	go func() {
		defer sm.wg.Done()
		sm.runMetricsCollector()
	}()
}

// runBatchProcessor processes span batches
func (sm *SpanManager) runBatchProcessor() {
	ticker := time.NewTicker(sm.config.BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return
		case <-ticker.C:
			sm.batchProcessor.mu.Lock()
			if len(sm.batchProcessor.spans) > 0 {
				sm.batchProcessor.flush()
			}
			sm.batchProcessor.mu.Unlock()
		}
	}
}

// runMetricsCollector collects and reports span manager metrics
func (sm *SpanManager) runMetricsCollector() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return
		case <-ticker.C:
			sm.collectMetrics()
		}
	}
}

// collectMetrics collects current span manager metrics
func (sm *SpanManager) collectMetrics() {
	if !sm.exporter.config.EnableMetrics {
		return
	}

	ctx := context.Background()

	// Count active spans
	activeCount := int64(0)
	sm.activeSpans.Range(func(key, value interface{}) bool {
		activeCount++
		return true
	})

	// Record metrics
	sm.exporter.resourceUtilization.Record(ctx, float64(activeCount)/float64(sm.config.MaxConcurrentSpans)*100)

	// Record circuit breaker state
	cbState := int64(sm.circuitBreaker.GetState())
	sm.exporter.circuitBreakerState.Record(ctx, cbState)
}

// GetMetrics returns span manager metrics
func (sm *SpanManager) GetMetrics() SpanManagerMetrics {
	activeCount := int64(0)
	sm.activeSpans.Range(func(key, value interface{}) bool {
		activeCount++
		return true
	})

	return SpanManagerMetrics{
		SpansCreated:    sm.spansCreated.Load(),
		SpansCompleted:  sm.spansCompleted.Load(),
		SpansExported:   sm.spansExported.Load(),
		SpansFailed:     sm.spansFailed.Load(),
		BatchesExported: sm.batchesExported.Load(),
		ActiveSpans:     activeCount,
		QueueSize:       int64(len(sm.spanQueue)),
		CircuitBreaker:  sm.circuitBreaker.GetMetrics(),
		TimeoutManager:  sm.timeoutManager.GetMetrics(),
		BoundedExecutor: sm.boundedExecutor.GetMetrics(),
	}
}

// SpanManagerMetrics represents span manager metrics
type SpanManagerMetrics struct {
	SpansCreated    int64
	SpansCompleted  int64
	SpansExported   int64
	SpansFailed     int64
	BatchesExported int64
	ActiveSpans     int64
	QueueSize       int64
	CircuitBreaker  resilience.Metrics
	TimeoutManager  resilience.TimeoutMetrics
	BoundedExecutor resilience.BoundedExecutorMetrics
}

// Shutdown gracefully shuts down the span manager
func (sm *SpanManager) Shutdown(ctx context.Context) error {
	fmt.Println("🛑 Shutting down span manager...")

	// Cancel background processes
	sm.cancel()

	// Wait for processors to finish
	done := make(chan struct{})
	go func() {
		sm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("✅ Span manager processors stopped")
	case <-ctx.Done():
		fmt.Println("⚠️ Span manager shutdown timed out")
		return ctx.Err()
	}

	// Export any remaining spans
	sm.batchProcessor.mu.Lock()
	if len(sm.batchProcessor.spans) > 0 {
		spans := sm.batchProcessor.spans
		sm.batchProcessor.spans = nil
		sm.batchProcessor.mu.Unlock()

		if err := sm.ExportBatch(ctx, spans); err != nil {
			fmt.Printf("⚠️ Failed to export final batch: %v\n", err)
		}
	} else {
		sm.batchProcessor.mu.Unlock()
	}

	// Close remaining active spans
	sm.activeSpans.Range(func(key, value interface{}) bool {
		if span, ok := value.(*ManagedSpan); ok {
			span.End()
		}
		sm.activeSpans.Delete(key)
		return true
	})

	fmt.Println("✅ Span manager shutdown complete")
	return nil
}
