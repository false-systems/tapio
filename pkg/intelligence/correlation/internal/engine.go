package internal
import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/algorithms"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/core"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/patterns"
)
// correlationEngine implements the core.CorrelationEngine interface
type correlationEngine struct {
	// Configuration
	config core.EngineConfig
	// State management
	started atomic.Bool
	stopped atomic.Bool
	// Components
	buffer           core.EventBuffer
	patternMatcher   core.PatternMatcher
	temporalAnalyzer core.TemporalAnalyzer
	causalAnalyzer   core.CausalAnalyzer
	confidenceCalc   core.ConfidenceCalculator
	eventProcessor   core.EventProcessor
	// Pattern registry
	patterns     map[string]core.CorrelationPattern
	patternMutex sync.RWMutex
	// Algorithm registry
	algorithms     map[string]core.CorrelationAlgorithm
	algorithmMutex sync.RWMutex
	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// Event processing
	eventChan      chan domain.Event
	correlationChan chan domain.Correlation
	// Statistics
	stats struct {
		eventsProcessed   atomic.Uint64
		correlationsFound atomic.Uint64
		patternsMatched   atomic.Uint64
		processingErrors  atomic.Uint64
		lastEventTime     atomic.Value // time.Time
		startTime         time.Time
	}
	// Health tracking
	lastProcessingTime atomic.Value // time.Time
	errorCount         atomic.Uint64
}
// NewCorrelationEngine creates a new correlation engine
func NewCorrelationEngine(config core.EngineConfig) (core.CorrelationEngine, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	engine := &correlationEngine{
		config:          config,
		patterns:        make(map[string]core.CorrelationPattern),
		algorithms:      make(map[string]core.CorrelationAlgorithm),
		eventChan:       make(chan domain.Event, config.EventBufferSize),
		correlationChan: make(chan domain.Correlation, config.OutputBufferSize),
	}
	// Initialize components
	if err := engine.initializeComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}
	// Register default patterns
	if err := engine.registerDefaultPatterns(); err != nil {
		return nil, fmt.Errorf("failed to register default patterns: %w", err)
	}
	// Register default algorithms
	if err := engine.registerDefaultAlgorithms(); err != nil {
		return nil, fmt.Errorf("failed to register default algorithms: %w", err)
	}
	engine.stats.startTime = time.Now()
	engine.stats.lastEventTime.Store(time.Time{})
	engine.lastProcessingTime.Store(time.Now())
	return engine, nil
}
// Start starts the correlation engine
func (e *correlationEngine) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return fmt.Errorf("correlation engine is disabled")
	}
	if e.started.Load() {
		return core.ErrEngineAlreadyStarted
	}
	// Create cancellable context
	e.ctx, e.cancel = context.WithCancel(ctx)
	// Start background workers
	e.wg.Add(3)
	go e.eventProcessingWorker()
	go e.correlationWorker()
	go e.maintenanceWorker()
	// Mark as started
	e.started.Store(true)
	return nil
}
// Stop gracefully stops the correlation engine
func (e *correlationEngine) Stop() error {
	if !e.started.Load() {
		return core.ErrEngineNotStarted
	}
	if e.stopped.Load() {
		return nil
	}
	// Mark as stopping
	e.stopped.Store(true)
	// Cancel context
	if e.cancel != nil {
		e.cancel()
	}
	// Wait for workers to finish
	e.wg.Wait()
	// Close channels
	close(e.eventChan)
	close(e.correlationChan)
	return nil
}
// ProcessEvent processes a single event
func (e *correlationEngine) ProcessEvent(ctx context.Context, event domain.Event) error {
	if !e.started.Load() {
		return core.ErrEngineNotStarted
	}
	if e.stopped.Load() {
		return core.ErrEngineShuttingDown
	}
	// Validate and preprocess event
	processedEvent, err := e.eventProcessor.Preprocess(event)
	if err != nil {
		e.errorCount.Add(1)
		return fmt.Errorf("event preprocessing failed: %w", err)
	}
	if err := e.eventProcessor.Validate(processedEvent); err != nil {
		e.errorCount.Add(1)
		return fmt.Errorf("event validation failed: %w", err)
	}
	// Try to send event to processing channel
	select {
	case e.eventChan <- processedEvent:
		e.stats.eventsProcessed.Add(1)
		e.stats.lastEventTime.Store(time.Now())
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		e.errorCount.Add(1)
		return core.ErrEventBufferFull
	}
}
// ProcessEvents processes multiple events and returns correlations
func (e *correlationEngine) ProcessEvents(ctx context.Context, events []domain.Event) ([]domain.Correlation, error) {
	if !e.started.Load() {
		return nil, core.ErrEngineNotStarted
	}
	var correlations []domain.Correlation
	// Process each event
	for _, event := range events {
		if err := e.ProcessEvent(ctx, event); err != nil {
			// Log error but continue processing other events
			e.stats.processingErrors.Add(1)
		}
	}
	// Add events to buffer for pattern analysis
	for _, event := range events {
		if err := e.buffer.Add(event); err != nil {
			e.stats.processingErrors.Add(1)
		}
	}
	// Run correlation analysis on the events
	batchCorrelations, err := e.analyzeEvents(ctx, events)
	if err != nil {
		return correlations, fmt.Errorf("correlation analysis failed: %w", err)
	}
	correlations = append(correlations, batchCorrelations...)
	return correlations, nil
}
// RegisterPattern registers a correlation pattern
func (e *correlationEngine) RegisterPattern(pattern core.CorrelationPattern) error {
	if pattern == nil {
		return fmt.Errorf("pattern cannot be nil")
	}
	e.patternMutex.Lock()
	defer e.patternMutex.Unlock()
	if _, exists := e.patterns[pattern.ID()]; exists {
		return core.ErrPatternAlreadyExists
	}
	e.patterns[pattern.ID()] = pattern
	return nil
}
// UnregisterPattern unregisters a correlation pattern
func (e *correlationEngine) UnregisterPattern(patternID string) error {
	e.patternMutex.Lock()
	defer e.patternMutex.Unlock()
	if _, exists := e.patterns[patternID]; !exists {
		return core.ErrPatternNotFound
	}
	delete(e.patterns, patternID)
	return nil
}
// ListPatterns returns all registered patterns
func (e *correlationEngine) ListPatterns() []core.CorrelationPattern {
	e.patternMutex.RLock()
	defer e.patternMutex.RUnlock()
	patterns := make([]core.CorrelationPattern, 0, len(e.patterns))
	for _, pattern := range e.patterns {
		patterns = append(patterns, pattern)
	}
	return patterns
}
// GetCorrelations retrieves correlations based on criteria
func (e *correlationEngine) GetCorrelations(ctx context.Context, criteria core.CorrelationCriteria) ([]domain.Correlation, error) {
	if err := criteria.Validate(); err != nil {
		return nil, fmt.Errorf("invalid criteria: %w", err)
	}
	// Get events from buffer based on criteria
	events, err := e.getEventsForCriteria(criteria)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	if len(events) == 0 {
		return []domain.Correlation{}, nil
	}
	// Analyze events for correlations
	correlations, err := e.analyzeEvents(ctx, events)
	if err != nil {
		return nil, fmt.Errorf("correlation analysis failed: %w", err)
	}
	// Filter correlations based on criteria
	filteredCorrelations := e.filterCorrelations(correlations, criteria)
	return filteredCorrelations, nil
}
// AnalyzeTimeWindow analyzes events within a specific time window
func (e *correlationEngine) AnalyzeTimeWindow(ctx context.Context, start, end time.Time) ([]domain.Correlation, error) {
	if end.Before(start) {
		return nil, core.ErrInvalidTimeRange
	}
	// Get events from the time window
	events, err := e.buffer.GetByTimeRange(start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to get events from time range: %w", err)
	}
	if len(events) == 0 {
		return []domain.Correlation{}, nil
	}
	// Analyze events for correlations
	correlations, err := e.analyzeEvents(ctx, events)
	if err != nil {
		return nil, fmt.Errorf("correlation analysis failed: %w", err)
	}
	return correlations, nil
}
// Health returns engine health status
func (e *correlationEngine) Health() core.EngineHealth {
	status := core.HealthStatusHealthy
	message := "Correlation engine is healthy"
	if !e.started.Load() {
		status = core.HealthStatusUnknown
		message = "Engine not started"
	} else if e.stopped.Load() {
		status = core.HealthStatusUnhealthy
		message = "Engine stopped"
	} else {
		// Check for error conditions
		errorCount := e.errorCount.Load()
		if errorCount > 100 {
			status = core.HealthStatusDegraded
			message = fmt.Sprintf("High error count: %d", errorCount)
		}
		// Check buffer utilization
		bufferUtil := float64(e.buffer.Size()) / float64(e.buffer.Capacity())
		if bufferUtil > 0.9 {
			status = core.HealthStatusDegraded
			message = "High buffer utilization"
		}
		// Check processing latency
		lastProcessing := e.lastProcessingTime.Load().(time.Time)
		if time.Since(lastProcessing) > 5*time.Minute {
			status = core.HealthStatusDegraded
			message = "High processing latency"
		}
	}
	lastEventTime := e.stats.lastEventTime.Load()
	if lastEventTime == nil {
		lastEventTime = time.Time{}
	}
	bufferUtilization := 0.0
	if e.buffer.Capacity() > 0 {
		bufferUtilization = float64(e.buffer.Size()) / float64(e.buffer.Capacity())
	}
	var processingLatency time.Duration
	lastProcessing := e.lastProcessingTime.Load().(time.Time)
	if !lastProcessing.IsZero() {
		processingLatency = time.Since(lastProcessing)
	}
	return core.EngineHealth{
		Status:            status,
		Message:           message,
		LastEventTime:     lastEventTime.(time.Time),
		EventsProcessed:   e.stats.eventsProcessed.Load(),
		CorrelationsFound: e.stats.correlationsFound.Load(),
		ErrorCount:        e.errorCount.Load(),
		BufferUtilization: bufferUtilization,
		ProcessingLatency: processingLatency,
		ActivePatterns:    len(e.patterns),
		Metrics: map[string]float64{
			"events_per_second":      e.getEventsPerSecond(),
			"correlations_per_hour":  e.getCorrelationsPerHour(),
			"error_rate":             e.getErrorRate(),
			"buffer_utilization":     bufferUtilization,
			"processing_latency_ms":  float64(processingLatency.Milliseconds()),
		},
	}
}
// Statistics returns engine statistics
func (e *correlationEngine) Statistics() core.EngineStatistics {
	uptime := time.Since(e.stats.startTime)
	patternStats := make(map[string]core.PatternStats)
	e.patternMutex.RLock()
	for id, _ := range e.patterns {
		// In a real implementation, we'd track these stats
		patternStats[id] = core.PatternStats{
			MatchCount:        0, // Would be tracked during operation
			AverageConfidence: 0.8,
			LastMatchTime:     time.Now(),
			ProcessingTime:    time.Millisecond * 10,
			SuccessRate:       0.95,
			ErrorCount:        0,
		}
	}
	e.patternMutex.RUnlock()
	algorithmMetrics := make(map[string]core.AlgorithmMetrics)
	e.algorithmMutex.RLock()
	for name, _ := range e.algorithms {
		// In a real implementation, we'd track these metrics
		algorithmMetrics[name] = core.AlgorithmMetrics{
			ExecutionCount:        0, // Would be tracked during operation
			AverageExecutionTime:  time.Millisecond * 5,
			SuccessRate:           0.95,
			ErrorCount:            0,
			CorrelationsFound:     0,
			AverageConfidence:     0.8,
		}
	}
	e.algorithmMutex.RUnlock()
	return core.EngineStatistics{
		StartTime:           e.stats.startTime,
		EventsProcessed:     e.stats.eventsProcessed.Load(),
		CorrelationsFound:   e.stats.correlationsFound.Load(),
		PatternsMatched:     e.stats.patternsMatched.Load(),
		ProcessingErrors:    e.stats.processingErrors.Load(),
		AverageLatency:      time.Millisecond * 50, // Would be calculated from actual measurements
		EventsPerSecond:     e.getEventsPerSecond(),
		CorrelationsPerHour: e.getCorrelationsPerHour(),
		PatternStatistics:   patternStats,
		AlgorithmMetrics:    algorithmMetrics,
		Custom: map[string]interface{}{
			"uptime_seconds":     uptime.Seconds(),
			"buffer_size":        e.buffer.Size(),
			"buffer_capacity":    e.buffer.Capacity(),
			"active_patterns":    len(e.patterns),
			"active_algorithms":  len(e.algorithms),
			"error_rate":         e.getErrorRate(),
		},
	}
}
// Configure updates the engine configuration
func (e *correlationEngine) Configure(config core.EngineConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	e.config = config
	return nil
}
// Worker methods
// eventProcessingWorker processes events from the event channel
func (e *correlationEngine) eventProcessingWorker() {
	defer e.wg.Done()
	for {
		select {
		case <-e.ctx.Done():
			return
		case event, ok := <-e.eventChan:
			if !ok {
				return
			}
			// Add event to buffer
			if err := e.buffer.Add(event); err != nil {
				e.stats.processingErrors.Add(1)
				continue
			}
			// Update processing time
			e.lastProcessingTime.Store(time.Now())
		}
	}
}
// correlationWorker performs correlation analysis
func (e *correlationEngine) correlationWorker() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.config.ProcessingTimeout)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			// Perform periodic correlation analysis
			e.performPeriodicAnalysis()
		}
	}
}
// maintenanceWorker performs maintenance tasks
func (e *correlationEngine) maintenanceWorker() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.config.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			// Perform cleanup
			e.performCleanup()
		}
	}
}
// Helper methods
// initializeComponents initializes engine components
func (e *correlationEngine) initializeComponents() error {
	// Initialize event buffer
	e.buffer = NewEventBuffer(e.config.EventBufferSize)
	// Initialize algorithms
	algorithmConfig := core.AlgorithmConfig{
		TimeWindow:    e.config.DefaultTimeWindow,
		MinConfidence: e.config.MinConfidenceScore,
		MaxEvents:     e.config.MaxConcurrentEvents,
		Parameters:    make(map[string]interface{}),
		Weights:       e.config.AlgorithmWeights,
	}
	e.patternMatcher = algorithms.NewPatternMatcher(algorithmConfig)
	e.temporalAnalyzer = algorithms.NewTemporalAnalyzer(algorithmConfig)
	e.causalAnalyzer = algorithms.NewCausalAnalyzer(algorithmConfig)
	e.confidenceCalc = NewConfidenceCalculator()
	e.eventProcessor = NewEventProcessor()
	return nil
}
// registerDefaultPatterns registers default correlation patterns
func (e *correlationEngine) registerDefaultPatterns() error {
	defaultPatterns := []core.CorrelationPattern{
		patterns.NewMemoryLeakPattern(),
		patterns.NewCascadeFailurePattern(),
		patterns.NewOOMPredictionPattern(),
		patterns.NewNetworkFailurePattern(),
	}
	for _, pattern := range defaultPatterns {
		if err := e.RegisterPattern(pattern); err != nil {
			return fmt.Errorf("failed to register pattern %s: %w", pattern.ID(), err)
		}
	}
	return nil
}
// registerDefaultAlgorithms registers default correlation algorithms
func (e *correlationEngine) registerDefaultAlgorithms() error {
	algorithmConfig := core.AlgorithmConfig{
		TimeWindow:    e.config.DefaultTimeWindow,
		MinConfidence: e.config.MinConfidenceScore,
		MaxEvents:     e.config.MaxConcurrentEvents,
		Parameters:    make(map[string]interface{}),
		Weights:       e.config.AlgorithmWeights,
	}
	e.algorithmMutex.Lock()
	defer e.algorithmMutex.Unlock()
	e.algorithms["pattern_matching"] = algorithms.NewPatternMatchingAlgorithm(e.ListPatterns(), algorithmConfig)
	e.algorithms["statistical"] = algorithms.NewStatisticalAlgorithm(algorithmConfig)
	return nil
}
// analyzeEvents performs correlation analysis on events
func (e *correlationEngine) analyzeEvents(ctx context.Context, events []domain.Event) ([]domain.Correlation, error) {
	var allCorrelations []domain.Correlation
	// Run pattern matching
	patterns := e.ListPatterns()
	correlations, err := e.patternMatcher.FindPatterns(ctx, events, patterns)
	if err != nil {
		return nil, fmt.Errorf("pattern matching failed: %w", err)
	}
	allCorrelations = append(allCorrelations, correlations...)
	e.stats.patternsMatched.Add(uint64(len(correlations)))
	// Run temporal analysis
	temporalChains, err := e.temporalAnalyzer.FindCausalChains(events)
	if err == nil {
		for _, chain := range temporalChains {
			// Convert causal chain to correlation
			correlation := e.chainToCorrelation(chain, events)
			allCorrelations = append(allCorrelations, correlation)
		}
	}
	// Run causal analysis
	causalChains, err := e.causalAnalyzer.FindCausalChains(events)
	if err == nil {
		for _, chain := range causalChains {
			// Convert causal chain to correlation
			correlation := e.causalChainToCorrelation(chain, events)
			allCorrelations = append(allCorrelations, correlation)
		}
	}
	// Update statistics
	e.stats.correlationsFound.Add(uint64(len(allCorrelations)))
	return allCorrelations, nil
}
// getEventsForCriteria retrieves events matching the criteria
func (e *correlationEngine) getEventsForCriteria(criteria core.CorrelationCriteria) ([]domain.Event, error) {
	// Start with time range query
	events, err := e.buffer.GetByTimeRange(criteria.StartTime, criteria.EndTime)
	if err != nil {
		return nil, err
	}
	// Apply additional filters
	var filtered []domain.Event
	for _, event := range events {
		if e.eventMatchesCriteria(event, criteria) {
			filtered = append(filtered, event)
		}
	}
	// Limit results
	if criteria.MaxResults > 0 && len(filtered) > criteria.MaxResults {
		filtered = filtered[:criteria.MaxResults]
	}
	return filtered, nil
}
// eventMatchesCriteria checks if an event matches the criteria
func (e *correlationEngine) eventMatchesCriteria(event domain.Event, criteria core.CorrelationCriteria) bool {
	// Check sources
	if len(criteria.Sources) > 0 {
		sourceMatch := false
		for _, source := range criteria.Sources {
			if event.Source == source {
				sourceMatch = true
				break
			}
		}
		if !sourceMatch {
			return false
		}
	}
	// Check event types
	if len(criteria.EventTypes) > 0 {
		typeMatch := false
		for _, eventType := range criteria.EventTypes {
			if event.Type == eventType {
				typeMatch = true
				break
			}
		}
		if !typeMatch {
			return false
		}
	}
	// Check severities
	if len(criteria.Severities) > 0 {
		severityMatch := false
		for _, severity := range criteria.Severities {
			if event.Severity == severity {
				severityMatch = true
				break
			}
		}
		if !severityMatch {
			return false
		}
	}
	// Check minimum confidence
	if event.Confidence < criteria.MinConfidence {
		return false
	}
	return true
}
// filterCorrelations filters correlations based on criteria
func (e *correlationEngine) filterCorrelations(correlations []domain.Correlation, criteria core.CorrelationCriteria) []domain.Correlation {
	var filtered []domain.Correlation
	for _, correlation := range correlations {
		if correlation.Confidence.Overall >= criteria.MinConfidence {
			filtered = append(filtered, correlation)
		}
	}
	// Limit results
	if criteria.MaxResults > 0 && len(filtered) > criteria.MaxResults {
		filtered = filtered[:criteria.MaxResults]
	}
	return filtered
}
// performPeriodicAnalysis performs periodic correlation analysis
func (e *correlationEngine) performPeriodicAnalysis() {
	// Get recent events
	now := time.Now()
	since := now.Add(-e.config.DefaultTimeWindow)
	events, err := e.buffer.GetByTimeRange(since, now)
	if err != nil || len(events) == 0 {
		return
	}
	// Analyze events
	ctx, cancel := context.WithTimeout(context.Background(), e.config.ProcessingTimeout)
	defer cancel()
	correlations, err := e.analyzeEvents(ctx, events)
	if err != nil {
		e.stats.processingErrors.Add(1)
		return
	}
	// Send correlations to output channel
	for _, correlation := range correlations {
		select {
		case e.correlationChan <- correlation:
		default:
			// Channel full, drop correlation
			break
		}
	}
}
// performCleanup performs maintenance cleanup
func (e *correlationEngine) performCleanup() {
	// Clean up old events from buffer
	cutoff := time.Now().Add(-e.config.EventRetentionTime)
	_, err := e.buffer.Expire(cutoff)
	if err != nil {
		e.stats.processingErrors.Add(1)
	}
}
// Metric calculation methods
func (e *correlationEngine) getEventsPerSecond() float64 {
	uptime := time.Since(e.stats.startTime).Seconds()
	if uptime == 0 {
		return 0
	}
	return float64(e.stats.eventsProcessed.Load()) / uptime
}
func (e *correlationEngine) getCorrelationsPerHour() float64 {
	uptime := time.Since(e.stats.startTime).Hours()
	if uptime == 0 {
		return 0
	}
	return float64(e.stats.correlationsFound.Load()) / uptime
}
func (e *correlationEngine) getErrorRate() float64 {
	processed := e.stats.eventsProcessed.Load()
	errors := e.stats.processingErrors.Load()
	if processed == 0 {
		return 0
	}
	return float64(errors) / float64(processed)
}
// Conversion methods
func (e *correlationEngine) chainToCorrelation(chain core.CausalChain, events []domain.Event) domain.Correlation {
	// Convert a temporal causal chain to a correlation
	// Convert EventIDs to EventReferences
	eventRefs := make([]domain.EventReference, len(chain.Events))
	for i, eventID := range chain.Events {
		eventRefs[i] = domain.EventReference{
			EventID:      eventID,
			Role:         "participant",
			Relationship: "temporal",
			Weight:       1.0,
		}
	}
	// Convert confidence to ConfidenceScore
	confidenceScore := domain.ConfidenceScore{
		Overall:     chain.Confidence,
		Temporal:    chain.Confidence,
		Causal:      chain.Confidence * 0.8, // Lower causal confidence for temporal chains
		Pattern:     chain.Confidence * 0.7,
		Statistical: chain.Confidence * 0.6,
	}
	return domain.Correlation{
		ID:          domain.CorrelationID(chain.ID),
		Type:        domain.CorrelationTypeTemporal,
		Events:      eventRefs,
		Confidence:  confidenceScore,
		Description: fmt.Sprintf("Temporal correlation chain with %d events", len(chain.Events)),
		Timestamp:   time.Now(),
		Metadata: domain.CorrelationMetadata{
			SchemaVersion: "1.0",
			ProcessedAt:   time.Now(),
			ProcessedBy:   "temporal_analyzer",
			Annotations: map[string]string{
				"chain_id":      chain.ID,
				"chain_category": string(chain.Category),
				"event_count":   fmt.Sprintf("%d", len(chain.Events)),
			},
		},
	}
}
func (e *correlationEngine) causalChainToCorrelation(chain core.CausalChain, events []domain.Event) domain.Correlation {
	// Convert a causal chain to a correlation
	// Convert EventIDs to EventReferences
	eventRefs := make([]domain.EventReference, len(chain.Events))
	for i, eventID := range chain.Events {
		eventRefs[i] = domain.EventReference{
			EventID:      eventID,
			Role:         "participant",
			Relationship: "causal",
			Weight:       1.0,
		}
	}
	// Convert confidence to ConfidenceScore
	confidenceScore := domain.ConfidenceScore{
		Overall:     chain.Confidence,
		Temporal:    chain.Confidence * 0.8,
		Causal:      chain.Confidence,
		Pattern:     chain.Confidence * 0.9,
		Statistical: chain.Confidence * 0.7,
	}
	return domain.Correlation{
		ID:          domain.CorrelationID(chain.ID),
		Type:        domain.CorrelationTypeCausal,
		Events:      eventRefs,
		Confidence:  confidenceScore,
		Description: fmt.Sprintf("Causal correlation chain with %d events", len(chain.Events)),
		Timestamp:   time.Now(),
		Metadata: domain.CorrelationMetadata{
			SchemaVersion: "1.0",
			ProcessedAt:   time.Now(),
			ProcessedBy:   "causal_analyzer",
			Annotations: map[string]string{
				"chain_id":      chain.ID,
				"chain_category": string(chain.Category),
				"event_count":   fmt.Sprintf("%d", len(chain.Events)),
			},
		},
	}
}