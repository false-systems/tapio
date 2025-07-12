package correlation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/ebpf"
	"github.com/yairfalse/tapio/pkg/journald"
	"github.com/yairfalse/tapio/pkg/systemd"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	// CircuitBreakerClosed allows requests through
	CircuitBreakerClosed CircuitBreakerState = iota
	// CircuitBreakerOpen blocks requests
	CircuitBreakerOpen
	// CircuitBreakerHalfOpen allows limited requests for testing
	CircuitBreakerHalfOpen
)

// CircuitBreaker protects data sources from repeated failures
type CircuitBreaker struct {
	name           string
	state          CircuitBreakerState
	failureCount   int
	successCount   int
	lastFailure    time.Time
	lastSuccess    time.Time
	failureThreshold int
	timeout        time.Duration
	mutex          sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, failureThreshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:             name,
		state:            CircuitBreakerClosed,
		failureThreshold: failureThreshold,
		timeout:          timeout,
	}
}

// CanExecute checks if a request can be executed
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	switch cb.state {
	case CircuitBreakerClosed:
		return true
	case CircuitBreakerOpen:
		// Check if timeout has passed to transition to half-open
		if time.Since(cb.lastFailure) > cb.timeout {
			return true
		}
		return false
	case CircuitBreakerHalfOpen:
		return true
	default:
		return false
	}
}

// OnSuccess records a successful execution
func (cb *CircuitBreaker) OnSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.successCount++
	cb.lastSuccess = time.Now()

	if cb.state == CircuitBreakerHalfOpen {
		// Reset to closed after successful execution in half-open state
		cb.state = CircuitBreakerClosed
		cb.failureCount = 0
	}
}

// OnFailure records a failed execution
func (cb *CircuitBreaker) OnFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.failureCount++
	cb.lastFailure = time.Now()

	if cb.state == CircuitBreakerClosed && cb.failureCount >= cb.failureThreshold {
		cb.state = CircuitBreakerOpen
	} else if cb.state == CircuitBreakerHalfOpen {
		cb.state = CircuitBreakerOpen
	}
}

// GetState returns the current state
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	
	// Transition from open to half-open if timeout has passed
	if cb.state == CircuitBreakerOpen && time.Since(cb.lastFailure) > cb.timeout {
		cb.state = CircuitBreakerHalfOpen
	}
	
	return cb.state
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	return map[string]interface{}{
		"name":          cb.name,
		"state":         cb.state,
		"failure_count": cb.failureCount,
		"success_count": cb.successCount,
		"last_failure":  cb.lastFailure,
		"last_success":  cb.lastSuccess,
	}
}

// EnhancedEngine provides advanced multi-source correlation capabilities
type EnhancedEngine struct {
	sources        map[SourceType]DataSource
	timeline       *Timeline
	correlators    []Correlator
	analyzers      []Analyzer
	
	// Processing state
	isRunning      bool
	processingRate float64
	errorCount     uint64
	
	// Configuration
	config         *EnhancedEngineConfig
	
	// Circuit breakers for data sources
	circuitBreakers map[SourceType]*CircuitBreaker
	
	// Channels
	eventChan      chan TimelineEvent
	resultChan     chan CorrelationResult
	
	// Lifecycle
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	mutex          sync.RWMutex
}

// EnhancedEngineConfig configures the enhanced correlation engine
type EnhancedEngineConfig struct {
	// Timeline settings
	MaxTimelineEvents    int
	CorrelationWindow    time.Duration
	
	// Processing settings
	EventBufferSize      int
	ProcessingWorkers    int
	BatchSize            int
	ProcessingTimeout    time.Duration
	
	// Analysis settings
	EnablePatternAnalysis bool
	EnableAnomalyDetection bool
	EnablePrediction      bool
	
	// Performance settings
	MaxEventsPerSecond   int
	EnableThrottling     bool
	
	// Circuit breaker settings
	EnableCircuitBreaker bool
	FailureThreshold     int
	RecoveryTimeout      time.Duration
}

// Correlator defines the interface for event correlation
type Correlator interface {
	Name() string
	Correlate(events []TimelineEvent) []CorrelationResult
}

// Analyzer defines the interface for event analysis
type Analyzer interface {
	Name() string
	Analyze(timeline *Timeline) []AnalysisResult
}

// CorrelationResult represents the result of correlation analysis
type CorrelationResult struct {
	ID          string
	Type        string
	Confidence  float64
	Events      []string // Event IDs
	Description string
	Severity    string
	Impact      ImpactAssessment
	Remediation []RemediationStep
	Metadata    map[string]interface{}
}

// ImpactAssessment describes the impact of correlated events
type ImpactAssessment struct {
	Scope       string   // pod, service, namespace, cluster
	Affected    []string // List of affected entities
	Severity    string   // low, medium, high, critical
	Description string
}

// RemediationStep describes a remediation action
type RemediationStep struct {
	Action      string
	Target      string
	Description string
	Automated   bool
	Priority    int
}

// AnalysisResult represents the result of timeline analysis
type AnalysisResult struct {
	Type        string
	Summary     string
	Details     map[string]interface{}
	Insights    []string
	Timestamp   time.Time
}

// NewEnhancedEngine creates a new enhanced correlation engine
func NewEnhancedEngine(config *EnhancedEngineConfig) *EnhancedEngine {
	if config == nil {
		config = DefaultEnhancedEngineConfig()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	engine := &EnhancedEngine{
		sources:         make(map[SourceType]DataSource),
		timeline:        NewTimeline(config.MaxTimelineEvents),
		correlators:     make([]Correlator, 0),
		analyzers:       make([]Analyzer, 0),
		config:          config,
		circuitBreakers: make(map[SourceType]*CircuitBreaker),
		eventChan:       make(chan TimelineEvent, config.EventBufferSize),
		resultChan:      make(chan CorrelationResult, 1000),
		ctx:             ctx,
		cancel:          cancel,
	}
	
	// Initialize default correlators
	engine.initializeDefaultCorrelators()
	
	// Initialize default analyzers
	engine.initializeDefaultAnalyzers()
	
	return engine
}

// DefaultEnhancedEngineConfig returns the default configuration
func DefaultEnhancedEngineConfig() *EnhancedEngineConfig {
	return &EnhancedEngineConfig{
		MaxTimelineEvents:     100000,
		CorrelationWindow:     5 * time.Minute,
		EventBufferSize:       50000,
		ProcessingWorkers:     4,
		BatchSize:             100,
		ProcessingTimeout:     1 * time.Second,
		EnablePatternAnalysis: true,
		EnableAnomalyDetection: true,
		EnablePrediction:      false,
		MaxEventsPerSecond:    10000,
		EnableThrottling:      true,
		EnableCircuitBreaker:  true,
		FailureThreshold:      5,
		RecoveryTimeout:       30 * time.Second,
	}
}

// AddSource adds a data source to the engine
func (e *EnhancedEngine) AddSource(sourceType SourceType, source DataSource) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	
	e.sources[sourceType] = source
	
	// Initialize circuit breaker for this source if enabled
	if e.config.EnableCircuitBreaker {
		e.circuitBreakers[sourceType] = NewCircuitBreaker(
			string(sourceType),
			e.config.FailureThreshold,
			e.config.RecoveryTimeout,
		)
	}
}

// AddCorrelator adds a custom correlator
func (e *EnhancedEngine) AddCorrelator(correlator Correlator) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	
	e.correlators = append(e.correlators, correlator)
}

// AddAnalyzer adds a custom analyzer
func (e *EnhancedEngine) AddAnalyzer(analyzer Analyzer) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	
	e.analyzers = append(e.analyzers, analyzer)
}

// Start starts the correlation engine
func (e *EnhancedEngine) Start(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	
	if e.isRunning {
		return fmt.Errorf("engine already running")
	}
	
	// Start processing workers
	for i := 0; i < e.config.ProcessingWorkers; i++ {
		e.wg.Add(1)
		go e.processEvents()
	}
	
	// Start source collectors
	e.wg.Add(1)
	go e.collectFromSources()
	
	// Start correlation processor
	e.wg.Add(1)
	go e.processCorrelations()
	
	// Start analysis processor
	if e.config.EnablePatternAnalysis {
		e.wg.Add(1)
		go e.processAnalysis()
	}
	
	e.isRunning = true
	return nil
}

// Stop stops the correlation engine
func (e *EnhancedEngine) Stop() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	
	if !e.isRunning {
		return nil
	}
	
	e.cancel()
	e.wg.Wait()
	
	close(e.eventChan)
	close(e.resultChan)
	
	e.isRunning = false
	return nil
}

// collectFromSources collects events from all data sources
func (e *EnhancedEngine) collectFromSources() {
	defer e.wg.Done()
	
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.collectSourceData()
		}
	}
}

// collectSourceData collects data from all sources and converts to timeline events
func (e *EnhancedEngine) collectSourceData() {
	// Collect from eBPF source
	if source, exists := e.sources[SourceEBPF]; exists && source.IsAvailable() {
		e.collectFromSource(SourceEBPF, source, "events", e.processEBPFEvents)
	}
	
	// Collect from systemd source
	if source, exists := e.sources[SourceSystemd]; exists && source.IsAvailable() {
		e.collectFromSource(SourceSystemd, source, "events", e.processSystemdEvents)
	}
	
	// Collect from journald source
	if source, exists := e.sources[SourceJournald]; exists && source.IsAvailable() {
		e.collectFromSource(SourceJournald, source, "events", e.processJournaldEvents)
	}
	
	// Collect from Kubernetes source
	if source, exists := e.sources[SourceKubernetes]; exists && source.IsAvailable() {
		e.collectFromSource(SourceKubernetes, source, "events", e.processKubernetesEvents)
	}
}

// collectFromSource collects data from a specific source with circuit breaker protection
func (e *EnhancedEngine) collectFromSource(sourceType SourceType, source DataSource, dataType string, processFunc func(interface{})) {
	// Check circuit breaker if enabled
	if e.config.EnableCircuitBreaker {
		if cb, exists := e.circuitBreakers[sourceType]; exists {
			if !cb.CanExecute() {
				// Circuit breaker is open, skip this source
				return
			}
		}
	}
	
	// Attempt to get data
	data, err := source.GetData(e.ctx, dataType, nil)
	
	// Record success/failure with circuit breaker
	if e.config.EnableCircuitBreaker {
		if cb, exists := e.circuitBreakers[sourceType]; exists {
			if err != nil {
				cb.OnFailure()
				e.errorCount++
			} else {
				cb.OnSuccess()
				// Process data if successful
				processFunc(data)
			}
		}
	} else {
		// No circuit breaker, process data if successful
		if err == nil {
			processFunc(data)
		} else {
			e.errorCount++
		}
	}
}

// processEBPFEvents converts eBPF events to timeline events
func (e *EnhancedEngine) processEBPFEvents(data interface{}) {
	events, ok := data.([]ebpf.SystemEvent)
	if !ok {
		return
	}
	
	for _, event := range events {
		timelineEvent := TimelineEvent{
			Timestamp: event.Timestamp,
			Source:    SourceEBPF,
			EventType: event.Type,
			Severity:  e.mapEBPFSeverity(event),
			Message:   fmt.Sprintf("eBPF event: %s", event.Type),
			Entity: EntityReference{
				Type: "process",
				Name: fmt.Sprintf("pid_%d", event.PID),
				UID:  fmt.Sprintf("%d", event.PID),
			},
			Metadata: map[string]interface{}{
				"pid":        event.PID,
				"event_data": event.Data,
			},
		}
		
		select {
		case e.eventChan <- timelineEvent:
		case <-e.ctx.Done():
			return
		}
	}
}

// processSystemdEvents converts systemd events to timeline events
func (e *EnhancedEngine) processSystemdEvents(data interface{}) {
	events, ok := data.([]*systemd.ServiceEvent)
	if !ok {
		return
	}
	
	for _, event := range events {
		severity := "info"
		if event.EventType == systemd.ServiceEventFailure {
			severity = "error"
		} else if event.EventType == systemd.ServiceEventRestart {
			severity = "warning"
		}
		
		timelineEvent := TimelineEvent{
			Timestamp: event.Timestamp,
			Source:    SourceSystemd,
			EventType: event.EventType.String(),
			Severity:  severity,
			Message:   fmt.Sprintf("Service %s: %s -> %s", event.ServiceName, event.OldState, event.NewState),
			Entity: EntityReference{
				Type: "service",
				Name: event.ServiceName,
			},
			Metadata: map[string]interface{}{
				"old_state":  event.OldState,
				"new_state":  event.NewState,
				"reason":     event.Reason,
				"properties": event.Properties,
			},
		}
		
		select {
		case e.eventChan <- timelineEvent:
		case <-e.ctx.Done():
			return
		}
	}
}

// processJournaldEvents converts journald events to timeline events
func (e *EnhancedEngine) processJournaldEvents(data interface{}) {
	events, ok := data.([]*journald.LogEvent)
	if !ok {
		return
	}
	
	for _, event := range events {
		severity := e.mapJournaldSeverity(event.Priority)
		
		timelineEvent := TimelineEvent{
			Timestamp: event.Timestamp,
			Source:    SourceJournald,
			EventType: "log",
			Severity:  severity,
			Message:   event.Message,
			Entity: EntityReference{
				Type: "service",
				Name: event.Service,
			},
			Metadata: map[string]interface{}{
				"priority":          event.Priority,
				"matched_patterns":  event.MatchedPatterns,
				"classification":    event.Classification,
				"fields":           event.Fields,
			},
		}
		
		select {
		case e.eventChan <- timelineEvent:
		case <-e.ctx.Done():
			return
		}
	}
}

// processKubernetesEvents converts Kubernetes events to timeline events
func (e *EnhancedEngine) processKubernetesEvents(data interface{}) {
	// Implementation for Kubernetes events
	// This would process pod events, node events, etc.
}

// processEvents processes events from the event channel
func (e *EnhancedEngine) processEvents() {
	defer e.wg.Done()
	
	batch := make([]TimelineEvent, 0, e.config.BatchSize)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-e.ctx.Done():
			// Process remaining batch
			if len(batch) > 0 {
				e.processBatch(batch)
			}
			return
			
		case event := <-e.eventChan:
			batch = append(batch, event)
			if len(batch) >= e.config.BatchSize {
				e.processBatch(batch)
				batch = batch[:0]
			}
			
		case <-ticker.C:
			if len(batch) > 0 {
				e.processBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

// processBatch processes a batch of events
func (e *EnhancedEngine) processBatch(events []TimelineEvent) {
	// Add events to timeline
	for _, event := range events {
		e.timeline.AddEvent(event)
	}
	
	// Run correlations on the batch
	for _, correlator := range e.correlators {
		results := correlator.Correlate(events)
		for _, result := range results {
			select {
			case e.resultChan <- result:
			default:
				// Drop if channel is full
			}
		}
	}
}

// processCorrelations processes correlation results
func (e *EnhancedEngine) processCorrelations() {
	defer e.wg.Done()
	
	for {
		select {
		case <-e.ctx.Done():
			return
		case result := <-e.resultChan:
			e.handleCorrelationResult(result)
		}
	}
}

// handleCorrelationResult handles a correlation result
func (e *EnhancedEngine) handleCorrelationResult(result CorrelationResult) {
	// Update timeline with correlations
	for i, eventID := range result.Events {
		for j, otherID := range result.Events {
			if i != j {
				// Add correlation between events
				e.addEventCorrelation(eventID, otherID)
			}
		}
	}
	
	// Generate alerts for high-severity correlations
	if result.Severity == "critical" || result.Severity == "high" {
		// In a real implementation, this would trigger alerts
	}
}

// addEventCorrelation adds a correlation between two events
func (e *EnhancedEngine) addEventCorrelation(eventID1, eventID2 string) {
	// This would update the timeline events with correlation information
	// For now, it's a placeholder
}

// processAnalysis runs periodic analysis on the timeline
func (e *EnhancedEngine) processAnalysis() {
	defer e.wg.Done()
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.runAnalysis()
		}
	}
}

// runAnalysis runs all analyzers on the timeline
func (e *EnhancedEngine) runAnalysis() {
	for _, analyzer := range e.analyzers {
		results := analyzer.Analyze(e.timeline)
		for range results {
			// Process analysis results
			// In a real implementation, this would store or act on insights
		}
	}
}

// GetTimeline returns the correlation timeline
func (e *EnhancedEngine) GetTimeline() *Timeline {
	return e.timeline
}

// GetCorrelationResults returns recent correlation results
func (e *EnhancedEngine) GetCorrelationResults(limit int) []CorrelationResult {
	// In a real implementation, this would return stored results
	return nil
}

// GetStatistics returns engine statistics
func (e *EnhancedEngine) GetStatistics() map[string]interface{} {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	
	timelineStats := e.timeline.GetStatistics()
	
	stats := map[string]interface{}{
		"is_running":       e.isRunning,
		"processing_rate":  e.processingRate,
		"error_count":      e.errorCount,
		"timeline_events":  timelineStats.TotalEvents,
		"sources":          len(e.sources),
		"correlators":      len(e.correlators),
		"analyzers":        len(e.analyzers),
	}
	
	// Add circuit breaker statistics if enabled
	if e.config.EnableCircuitBreaker {
		circuitBreakerStats := make(map[string]interface{})
		for sourceType, cb := range e.circuitBreakers {
			circuitBreakerStats[string(sourceType)] = cb.GetStats()
		}
		stats["circuit_breakers"] = circuitBreakerStats
	}
	
	return stats
}

// Helper methods

func (e *EnhancedEngine) mapEBPFSeverity(event ebpf.SystemEvent) string {
	if strings.Contains(strings.ToLower(event.Type), "error") {
		return "error"
	}
	if strings.Contains(strings.ToLower(event.Type), "warning") {
		return "warning"
	}
	return "info"
}

func (e *EnhancedEngine) mapJournaldSeverity(priority int) string {
	switch priority {
	case 0, 1, 2:
		return "critical"
	case 3:
		return "error"
	case 4:
		return "warning"
	case 5, 6:
		return "info"
	default:
		return "debug"
	}
}

// initializeDefaultCorrelators initializes default correlators
func (e *EnhancedEngine) initializeDefaultCorrelators() {
	config := DefaultCorrelatorConfig()
	e.correlators = append(e.correlators, NewMemoryPressureCorrelator(config))
	e.correlators = append(e.correlators, NewServiceFailureCorrelator(config))
	e.correlators = append(e.correlators, NewNetworkIssueCorrelator(config))
	e.correlators = append(e.correlators, NewSecurityThreatCorrelator(config))
}

// initializeDefaultAnalyzers initializes default analyzers
func (e *EnhancedEngine) initializeDefaultAnalyzers() {
	config := DefaultCorrelatorConfig()
	e.analyzers = append(e.analyzers, NewPatternAnalyzer(config))
	e.analyzers = append(e.analyzers, NewAnomalyAnalyzer(config))
	e.analyzers = append(e.analyzers, NewTrendAnalyzer(config))
}