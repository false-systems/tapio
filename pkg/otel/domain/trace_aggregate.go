package domain

import (
	"context"
	"fmt"
	"time"
	"sync"
	"sync/atomic"
)

// TraceAggregate represents the main aggregate root in the trace domain
// Implements DDD patterns with strong consistency boundaries and business rules
type TraceAggregate[T TraceData] struct {
	// Aggregate identity
	traceID      TraceID
	version      int64  // For optimistic locking
	
	// Aggregate state
	rootSpan     *SpanEntity[T]
	spans        map[SpanID]*SpanEntity[T]
	spanOrder    []SpanID  // Maintains span creation order
	
	// Domain state
	status       TraceStatus
	startTime    time.Time
	endTime      *time.Time
	duration     *time.Duration
	
	// Metadata and context
	serviceName  string
	serviceVersion string
	environment  string
	attributes   map[string]T
	tags         map[string]string
	
	// Sampling and configuration
	samplingRate float64
	sampled      bool
	
	// Business rules and policies
	policies     []TracePolicy[T]
	
	// Event sourcing
	uncommittedEvents []TraceEvent
	committedVersion  int64
	
	// Concurrency control
	mutex        sync.RWMutex
	lastModified time.Time
	
	// Performance tracking
	metrics      *TraceMetrics
	
	// Domain services (injected)
	correlationService TraceCorrelationService[T]
	samplingService    TraceSamplingService[T]
	
	// Validation and business rules
	validator    *TraceValidator[T]
	rules        *BusinessRuleEngine[T]
}

// SpanEntity represents a span as a domain entity within the trace aggregate
type SpanEntity[T TraceData] struct {
	// Entity identity
	spanID       SpanID
	parentSpanID *SpanID
	
	// Span data
	name         string
	kind         SpanKind
	status       SpanStatus
	startTime    time.Time
	endTime      *time.Time
	duration     *time.Duration
	
	// Span content
	attributes   map[string]T
	events       []*SpanEventVO[T]
	links        []*SpanLinkVO[T]
	
	// Entity state
	isRecording  bool
	isFinished   bool
	version      int64
	
	// Child spans tracking
	childSpans   []SpanID
	childCount   int32  // Atomic counter
	
	// Business rules
	policies     []SpanPolicy[T]
	
	// Domain events
	events       []SpanEvent
	
	// Validation
	validator    *SpanValidator[T]
}

// Value Objects for immutable domain concepts

// SpanEventVO represents an immutable span event value object
type SpanEventVO[T TraceData] struct {
	name       string
	timestamp  time.Time
	attributes map[string]T
}

// SpanLinkVO represents an immutable span link value object
type SpanLinkVO[T TraceData] struct {
	traceID    TraceID
	spanID     SpanID
	attributes map[string]T
}

// TraceMetricsVO represents immutable trace metrics
type TraceMetricsVO struct {
	spanCount        int
	errorCount       int
	averageDuration  time.Duration
	criticalPath     []SpanID
	bottleneckSpans  []SpanID
}

// Domain Services

// TraceAggregateFactory creates trace aggregates following DDD patterns
type TraceAggregateFactory[T TraceData] struct {
	// Configuration
	config           TraceAggregateConfig
	
	// Domain services
	correlationService TraceCorrelationService[T]
	samplingService    TraceSamplingService[T]
	
	// Policies and validators
	policyRegistry   *PolicyRegistry[T]
	validatorFactory *ValidatorFactory[T]
	
	// ID generation
	idGenerator      IDGenerator
	
	// Business rules engine
	rulesEngine      *BusinessRuleEngine[T]
}

// TraceRepository defines the repository interface for trace aggregates
type TraceRepository[T TraceData] interface {
	// Aggregate persistence
	Save(ctx context.Context, aggregate *TraceAggregate[T]) error
	Load(ctx context.Context, traceID TraceID) (*TraceAggregate[T], error)
	Delete(ctx context.Context, traceID TraceID) error
	
	// Optimistic locking support
	SaveWithVersion(ctx context.Context, aggregate *TraceAggregate[T], expectedVersion int64) error
	
	// Event sourcing support
	LoadFromEvents(ctx context.Context, traceID TraceID) (*TraceAggregate[T], error)
	SaveEvents(ctx context.Context, traceID TraceID, events []TraceEvent, expectedVersion int64) error
	
	// Query operations
	FindByServiceName(ctx context.Context, serviceName string, timeRange TimeRange) ([]*TraceAggregate[T], error)
	FindByAttributes(ctx context.Context, attributes map[string]T) ([]*TraceAggregate[T], error)
}

// NewTraceAggregate creates a new trace aggregate with proper initialization
func NewTraceAggregate[T TraceData](
	traceID TraceID,
	serviceName string,
	rootSpanName string,
	spanKind SpanKind,
	attributes map[string]T,
	correlationService TraceCorrelationService[T],
	samplingService TraceSamplingService[T],
) (*TraceAggregate[T], error) {
	
	// Validate input parameters
	if traceID == (TraceID{}) {
		return nil, NewDomainError("invalid_trace_id", "trace ID cannot be empty")
	}
	
	if serviceName == "" {
		return nil, NewDomainError("invalid_service_name", "service name cannot be empty")
	}
	
	if rootSpanName == "" {
		return nil, NewDomainError("invalid_span_name", "root span name cannot be empty")
	}
	
	// Make sampling decision
	samplingDecision := SamplingDecision{Sample: true, Rate: 1.0}
	if samplingService != nil {
		samplingDecision = samplingService.ShouldSampleRoot(context.Background(), traceID, rootSpanName)
	}
	
	// Create root span
	rootSpanID := generateSpanID()
	rootSpan := &SpanEntity[T]{
		spanID:      rootSpanID,
		parentSpanID: nil,
		name:        rootSpanName,
		kind:        spanKind,
		status:      SpanStatus{Code: StatusCodeUnset},
		startTime:   time.Now(),
		attributes:  copyAttributes(attributes),
		events:      make([]*SpanEventVO[T], 0),
		links:       make([]*SpanLinkVO[T], 0),
		isRecording: samplingDecision.Sample,
		isFinished:  false,
		version:     1,
		childSpans:  make([]SpanID, 0),
		validator:   NewSpanValidator[T](),
	}
	
	// Create trace aggregate
	aggregate := &TraceAggregate[T]{
		traceID:           traceID,
		version:           1,
		rootSpan:          rootSpan,
		spans:             make(map[SpanID]*SpanEntity[T]),
		spanOrder:         []SpanID{rootSpanID},
		status:            TraceStatusActive,
		startTime:         rootSpan.startTime,
		serviceName:       serviceName,
		attributes:        copyAttributes(attributes),
		tags:              make(map[string]string),
		samplingRate:      samplingDecision.Rate,
		sampled:           samplingDecision.Sample,
		policies:          make([]TracePolicy[T], 0),
		uncommittedEvents: make([]TraceEvent, 0),
		lastModified:      time.Now(),
		metrics:           NewTraceMetrics(),
		correlationService: correlationService,
		samplingService:   samplingService,
		validator:         NewTraceValidator[T](),
		rules:             NewBusinessRuleEngine[T](),
	}
	
	// Add root span to spans map
	aggregate.spans[rootSpanID] = rootSpan
	
	// Apply domain event
	aggregate.addUncommittedEvent(NewTraceStartedEvent(traceID, serviceName, rootSpanName, attributes))
	
	// Validate the created aggregate
	if err := aggregate.validate(); err != nil {
		return nil, fmt.Errorf("invalid trace aggregate: %w", err)
	}
	
	return aggregate, nil
}

// CreateChildSpan creates a new child span within the trace aggregate
func (ta *TraceAggregate[T]) CreateChildSpan(
	ctx context.Context,
	parentSpanID SpanID,
	spanName string,
	spanKind SpanKind,
	attributes map[string]T,
) (*SpanEntity[T], error) {
	
	ta.mutex.Lock()
	defer ta.mutex.Unlock()
	
	// Validate trace state
	if err := ta.validateForSpanCreation(); err != nil {
		return nil, err
	}
	
	// Validate parent span exists
	parentSpan, exists := ta.spans[parentSpanID]
	if !exists {
		return nil, NewDomainError("parent_span_not_found", fmt.Sprintf("parent span %s not found", parentSpanID))
	}
	
	// Validate parent span is still recording
	if !parentSpan.isRecording {
		return nil, NewDomainError("parent_span_not_recording", "cannot create child span on non-recording parent")
	}
	
	// Apply business rules
	if err := ta.rules.ValidateSpanCreation(ta, parentSpan, spanName, spanKind); err != nil {
		return nil, fmt.Errorf("business rule validation failed: %w", err)
	}
	
	// Generate new span ID
	spanID := generateSpanID()
	
	// Create child span entity
	childSpan := &SpanEntity[T]{
		spanID:       spanID,
		parentSpanID: &parentSpanID,
		name:         spanName,
		kind:         spanKind,
		status:       SpanStatus{Code: StatusCodeUnset},
		startTime:    time.Now(),
		attributes:   copyAttributes(attributes),
		events:       make([]*SpanEventVO[T], 0),
		links:        make([]*SpanLinkVO[T], 0),
		isRecording:  ta.sampled, // Inherit sampling decision
		isFinished:   false,
		version:      1,
		childSpans:   make([]SpanID, 0),
		validator:    NewSpanValidator[T](),
	}
	
	// Validate the new span
	if err := childSpan.validate(); err != nil {
		return nil, fmt.Errorf("invalid child span: %w", err)
	}
	
	// Add to trace aggregate
	ta.spans[spanID] = childSpan
	ta.spanOrder = append(ta.spanOrder, spanID)
	
	// Update parent span
	parentSpan.childSpans = append(parentSpan.childSpans, spanID)
	atomic.AddInt32(&parentSpan.childCount, 1)
	
	// Update trace version
	ta.incrementVersion()
	
	// Add domain event
	ta.addUncommittedEvent(NewSpanCreatedEvent(ta.traceID, spanID, parentSpanID, spanName))
	
	// Update metrics
	ta.metrics.IncrementSpanCount()
	
	return childSpan, nil
}

// FinishSpan marks a span as finished and applies business rules
func (ta *TraceAggregate[T]) FinishSpan(
	ctx context.Context,
	spanID SpanID,
	endTime time.Time,
	status SpanStatus,
) error {
	
	ta.mutex.Lock()
	defer ta.mutex.Unlock()
	
	// Find the span
	span, exists := ta.spans[spanID]
	if !exists {
		return NewDomainError("span_not_found", fmt.Sprintf("span %s not found", spanID))
	}
	
	// Validate span can be finished
	if span.isFinished {
		return NewDomainError("span_already_finished", "span is already finished")
	}
	
	// Apply business rules for span finishing
	if err := ta.rules.ValidateSpanFinishing(ta, span, endTime, status); err != nil {
		return fmt.Errorf("business rule validation failed: %w", err)
	}
	
	// Finish the span
	span.endTime = &endTime
	duration := endTime.Sub(span.startTime)
	span.duration = &duration
	span.status = status
	span.isFinished = true
	span.isRecording = false
	span.version++
	
	// Update trace version
	ta.incrementVersion()
	
	// Add domain event
	ta.addUncommittedEvent(NewSpanFinishedEvent(ta.traceID, spanID, endTime, duration))
	
	// Update metrics
	ta.metrics.RecordSpanDuration(duration)
	if status.Code == StatusCodeError {
		ta.metrics.IncrementErrorCount()
	}
	
	// Check if trace is complete
	if ta.isTraceComplete() {
		ta.finishTrace(endTime)
	}
	
	return nil
}

// AddSpanEvent adds an event to a specific span
func (ta *TraceAggregate[T]) AddSpanEvent(
	spanID SpanID,
	eventName string,
	timestamp time.Time,
	attributes map[string]T,
) error {
	
	ta.mutex.Lock()
	defer ta.mutex.Unlock()
	
	// Find the span
	span, exists := ta.spans[spanID]
	if !exists {
		return NewDomainError("span_not_found", fmt.Sprintf("span %s not found", spanID))
	}
	
	// Validate span is recording
	if !span.isRecording {
		return NewDomainError("span_not_recording", "cannot add event to non-recording span")
	}
	
	// Create event value object
	event := &SpanEventVO[T]{
		name:       eventName,
		timestamp:  timestamp,
		attributes: copyAttributes(attributes),
	}
	
	// Add event to span
	span.events = append(span.events, event)
	span.version++
	
	// Update trace version
	ta.incrementVersion()
	
	// Add domain event
	ta.addUncommittedEvent(NewSpanEventAddedEvent(ta.traceID, spanID, eventName, timestamp))
	
	return nil
}

// SetSpanAttributes sets attributes on a specific span
func (ta *TraceAggregate[T]) SetSpanAttributes(
	spanID SpanID,
	attributes map[string]T,
) error {
	
	ta.mutex.Lock()
	defer ta.mutex.Unlock()
	
	// Find the span
	span, exists := ta.spans[spanID]
	if !exists {
		return NewDomainError("span_not_found", fmt.Sprintf("span %s not found", spanID))
	}
	
	// Validate span is recording
	if !span.isRecording {
		return NewDomainError("span_not_recording", "cannot set attributes on non-recording span")
	}
	
	// Apply business rules for attribute setting
	if err := ta.rules.ValidateSpanAttributes(ta, span, attributes); err != nil {
		return fmt.Errorf("business rule validation failed: %w", err)
	}
	
	// Merge attributes
	for key, value := range attributes {
		span.attributes[key] = value
	}
	
	span.version++
	ta.incrementVersion()
	
	// Add domain event
	ta.addUncommittedEvent(NewSpanAttributesSetEvent(ta.traceID, spanID, attributes))
	
	return nil
}

// GetSpan retrieves a span from the aggregate
func (ta *TraceAggregate[T]) GetSpan(spanID SpanID) (*SpanEntity[T], error) {
	ta.mutex.RLock()
	defer ta.mutex.RUnlock()
	
	span, exists := ta.spans[spanID]
	if !exists {
		return nil, NewDomainError("span_not_found", fmt.Sprintf("span %s not found", spanID))
	}
	
	return span, nil
}

// GetRootSpan returns the root span of the trace
func (ta *TraceAggregate[T]) GetRootSpan() *SpanEntity[T] {
	ta.mutex.RLock()
	defer ta.mutex.RUnlock()
	
	return ta.rootSpan
}

// GetAllSpans returns all spans in the trace
func (ta *TraceAggregate[T]) GetAllSpans() []*SpanEntity[T] {
	ta.mutex.RLock()
	defer ta.mutex.RUnlock()
	
	spans := make([]*SpanEntity[T], 0, len(ta.spans))
	for _, spanID := range ta.spanOrder {
		if span, exists := ta.spans[spanID]; exists {
			spans = append(spans, span)
		}
	}
	
	return spans
}

// GetTraceMetrics returns computed trace metrics
func (ta *TraceAggregate[T]) GetTraceMetrics() *TraceMetricsVO {
	ta.mutex.RLock()
	defer ta.mutex.RUnlock()
	
	// Calculate metrics
	spanCount := len(ta.spans)
	errorCount := 0
	totalDuration := time.Duration(0)
	
	for _, span := range ta.spans {
		if span.status.Code == StatusCodeError {
			errorCount++
		}
		if span.duration != nil {
			totalDuration += *span.duration
		}
	}
	
	averageDuration := time.Duration(0)
	if spanCount > 0 {
		averageDuration = totalDuration / time.Duration(spanCount)
	}
	
	return &TraceMetricsVO{
		spanCount:       spanCount,
		errorCount:      errorCount,
		averageDuration: averageDuration,
		criticalPath:    ta.calculateCriticalPath(),
		bottleneckSpans: ta.identifyBottlenecks(),
	}
}

// Domain Event Methods

// GetUncommittedEvents returns events that haven't been persisted
func (ta *TraceAggregate[T]) GetUncommittedEvents() []TraceEvent {
	ta.mutex.RLock()
	defer ta.mutex.RUnlock()
	
	// Return a copy to prevent external mutation
	events := make([]TraceEvent, len(ta.uncommittedEvents))
	copy(events, ta.uncommittedEvents)
	
	return events
}

// MarkEventsAsCommitted marks events as committed and clears uncommitted events
func (ta *TraceAggregate[T]) MarkEventsAsCommitted() {
	ta.mutex.Lock()
	defer ta.mutex.Unlock()
	
	ta.committedVersion = ta.version
	ta.uncommittedEvents = ta.uncommittedEvents[:0] // Clear without allocating
}

// LoadFromHistory reconstructs the aggregate from domain events
func (ta *TraceAggregate[T]) LoadFromHistory(events []TraceEvent) error {
	ta.mutex.Lock()
	defer ta.mutex.Unlock()
	
	// Reset aggregate state
	ta.resetState()
	
	// Apply events in order
	for _, event := range events {
		if err := ta.applyEvent(event); err != nil {
			return fmt.Errorf("failed to apply event %s: %w", event.GetEventID(), err)
		}
	}
	
	// Mark all events as committed
	ta.committedVersion = ta.version
	ta.uncommittedEvents = ta.uncommittedEvents[:0]
	
	return nil
}

// Private helper methods

func (ta *TraceAggregate[T]) validateForSpanCreation() error {
	if ta.status == TraceStatusFinished {
		return NewDomainError("trace_finished", "cannot create spans on finished trace")
	}
	
	if len(ta.spans) >= 10000 { // Configurable limit
		return NewDomainError("too_many_spans", "trace has too many spans")
	}
	
	return nil
}

func (ta *TraceAggregate[T]) validate() error {
	return ta.validator.ValidateAggregate(ta)
}

func (ta *TraceAggregate[T]) incrementVersion() {
	ta.version++
	ta.lastModified = time.Now()
}

func (ta *TraceAggregate[T]) addUncommittedEvent(event TraceEvent) {
	ta.uncommittedEvents = append(ta.uncommittedEvents, event)
}

func (ta *TraceAggregate[T]) isTraceComplete() bool {
	// Trace is complete when all spans are finished
	for _, span := range ta.spans {
		if !span.isFinished {
			return false
		}
	}
	return true
}

func (ta *TraceAggregate[T]) finishTrace(endTime time.Time) {
	ta.endTime = &endTime
	duration := endTime.Sub(ta.startTime)
	ta.duration = &duration
	ta.status = TraceStatusFinished
	
	// Add domain event
	ta.addUncommittedEvent(NewTraceFinishedEvent(ta.traceID, endTime, duration))
}

func (ta *TraceAggregate[T]) calculateCriticalPath() []SpanID {
	// Implementation would calculate the critical path through the trace
	// This is a simplified placeholder
	return []SpanID{}
}

func (ta *TraceAggregate[T]) identifyBottlenecks() []SpanID {
	// Implementation would identify performance bottlenecks
	// This is a simplified placeholder
	return []SpanID{}
}

func (ta *TraceAggregate[T]) resetState() {
	ta.spans = make(map[SpanID]*SpanEntity[T])
	ta.spanOrder = []SpanID{}
	ta.version = 0
	ta.uncommittedEvents = []TraceEvent{}
}

func (ta *TraceAggregate[T]) applyEvent(event TraceEvent) error {
	// Apply event to aggregate state based on event type
	switch event.GetEventType() {
	case TraceEventTypeSpanStarted:
		return ta.applySpanStartedEvent(event)
	case TraceEventTypeSpanEnded:
		return ta.applySpanEndedEvent(event)
	case TraceEventTypeSpanExported:
		return ta.applySpanExportedEvent(event)
	case TraceEventTypeTraceCompleted:
		return ta.applyTraceCompletedEvent(event)
	case TraceEventTypeError:
		return ta.applyErrorEvent(event)
	default:
		return fmt.Errorf("unknown event type: %d", event.GetEventType())
	}
}

func (ta *TraceAggregate[T]) applySpanStartedEvent(event TraceEvent) error {
	// Implementation would apply span started event
	ta.incrementVersion()
	return nil
}

func (ta *TraceAggregate[T]) applySpanEndedEvent(event TraceEvent) error {
	// Implementation would apply span ended event
	ta.incrementVersion()
	return nil
}

func (ta *TraceAggregate[T]) applySpanExportedEvent(event TraceEvent) error {
	// Implementation would apply span exported event
	ta.incrementVersion()
	return nil
}

func (ta *TraceAggregate[T]) applyTraceCompletedEvent(event TraceEvent) error {
	// Implementation would apply trace completed event
	ta.incrementVersion()
	return nil
}

func (ta *TraceAggregate[T]) applyErrorEvent(event TraceEvent) error {
	// Implementation would apply error event
	ta.incrementVersion()
	return nil
}

// SpanEntity methods

func (se *SpanEntity[T]) validate() error {
	return se.validator.ValidateSpan(se)
}

// IsRoot returns true if this is the root span
func (se *SpanEntity[T]) IsRoot() bool {
	return se.parentSpanID == nil
}

// GetChildSpanCount returns the number of child spans
func (se *SpanEntity[T]) GetChildSpanCount() int {
	return int(atomic.LoadInt32(&se.childCount))
}

// HasChildren returns true if the span has child spans
func (se *SpanEntity[T]) HasChildren() bool {
	return se.GetChildSpanCount() > 0
}

// Supporting types and constants

type TraceStatus int

const (
	TraceStatusActive TraceStatus = iota
	TraceStatusFinished
	TraceStatusFailed
)

type TraceAggregateConfig struct {
	MaxSpansPerTrace    int
	MaxAttributesPerSpan int
	MaxEventsPerSpan    int
	EnableValidation    bool
	EnableMetrics       bool
}

// Helper functions

func copyAttributes[T TraceData](src map[string]T) map[string]T {
	if src == nil {
		return make(map[string]T)
	}
	
	dst := make(map[string]T, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func generateSpanID() SpanID {
	// Implementation would generate a proper span ID
	var spanID SpanID
	// Use crypto/rand or similar
	return spanID
}

// Additional domain event types and factory functions would be defined here...