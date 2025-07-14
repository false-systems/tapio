package correlation

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"
)

// BaseCorrelationEngine implements the Engine interface
type BaseCorrelationEngine struct {
	// Configuration
	windowSize         time.Duration
	processingInterval time.Duration
	maxConcurrentRules int
	enableMetrics      bool

	// Rules management
	rules   map[string]*Rule
	rulesMu sync.RWMutex

	// Event store
	eventStore EventStore

	// Metrics and monitoring
	stats            Stats
	statsMu          sync.RWMutex
	metricsCollector MetricsCollector

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Performance optimization
	ruleExecutionPool *sync.Pool
	contextPool       *sync.Pool

	// Rate limiting and cooldowns
	ruleCooldowns map[string]time.Time
	cooldownMu    sync.RWMutex

	// Result handlers
	resultHandlers []ResultHandler
	resultChan     chan *Result
}

// NewEngine creates a new correlation engine
func NewEngine(eventStore EventStore, opts ...EngineOption) *BaseCorrelationEngine {
	engine := &BaseCorrelationEngine{
		windowSize:         5 * time.Minute,
		processingInterval: 30 * time.Second,
		maxConcurrentRules: runtime.NumCPU() * 2,
		enableMetrics:      true,
		rules:              make(map[string]*Rule),
		eventStore:         eventStore,
		stats: Stats{
			RuleExecutionTime: make(map[string]time.Duration),
		},
		ruleCooldowns:  make(map[string]time.Time),
		resultHandlers: make([]ResultHandler, 0),
		resultChan:     make(chan *Result, 1000), // Buffered channel
	}

	// Apply options
	for _, opt := range opts {
		opt(engine)
	}

	// Initialize object pools for performance
	engine.ruleExecutionPool = &sync.Pool{
		New: func() interface{} {
			return &ruleExecution{}
		},
	}

	engine.contextPool = &sync.Pool{
		New: func() interface{} {
			return &Context{
				metrics:        make(map[string]MetricSeries),
				eventsBySource: make(map[EventSource][]Event),
				eventsByType:   make(map[string][]Event),
				eventsByEntity: make(map[string][]Event),
				Metadata:       make(map[string]string),
			}
		},
	}

	return engine
}

// EngineOption configures the correlation engine
type EngineOption func(*BaseCorrelationEngine)

// WithWindowSize sets the correlation window size
func WithWindowSize(duration time.Duration) EngineOption {
	return func(e *BaseCorrelationEngine) {
		e.windowSize = duration
	}
}

// WithProcessingInterval sets how often to run correlations
func WithProcessingInterval(interval time.Duration) EngineOption {
	return func(e *BaseCorrelationEngine) {
		e.processingInterval = interval
	}
}

// WithMaxConcurrentRules sets the maximum number of rules to run concurrently
func WithMaxConcurrentRules(limit int) EngineOption {
	return func(e *BaseCorrelationEngine) {
		e.maxConcurrentRules = limit
	}
}

// WithMetricsCollector sets the metrics collector
func WithMetricsCollector(collector MetricsCollector) EngineOption {
	return func(e *BaseCorrelationEngine) {
		e.metricsCollector = collector
	}
}

// WithResultHandler adds a result handler
func WithResultHandler(handler ResultHandler) EngineOption {
	return func(e *BaseCorrelationEngine) {
		e.resultHandlers = append(e.resultHandlers, handler)
	}
}

// ruleExecution represents a single rule execution
type ruleExecution struct {
	rule      *Rule
	context   *Context
	startTime time.Time
	result    *Result
	err       error
	duration  time.Duration
}

// RegisterRule registers a new correlation rule
func (e *BaseCorrelationEngine) RegisterRule(rule *Rule) error {
	if rule == nil {
		return fmt.Errorf("rule cannot be nil")
	}

	if rule.ID == "" {
		return fmt.Errorf("rule ID cannot be empty")
	}

	if rule.Evaluate == nil {
		return fmt.Errorf("rule must have an Evaluate function")
	}

	// Set defaults
	if rule.MinConfidence <= 0 {
		rule.MinConfidence = 0.5
	}

	if rule.Cooldown == 0 {
		rule.Cooldown = 5 * time.Minute
	}

	if rule.TTL == 0 {
		rule.TTL = 24 * time.Hour
	}

	if rule.Category == "" {
		rule.Category = CategoryReliability
	}

	rule.Enabled = true // Enable by default

	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()

	// Check if rule already exists
	if _, exists := e.rules[rule.ID]; exists {
		return fmt.Errorf("rule with ID '%s' already exists", rule.ID)
	}

	e.rules[rule.ID] = rule

	// Update stats
	e.statsMu.Lock()
	e.stats.RulesRegistered++
	e.statsMu.Unlock()

	return nil
}

// UnregisterRule removes a correlation rule
func (e *BaseCorrelationEngine) UnregisterRule(ruleID string) error {
	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()

	if _, exists := e.rules[ruleID]; !exists {
		return fmt.Errorf("rule with ID '%s' not found", ruleID)
	}

	delete(e.rules, ruleID)

	// Update stats
	e.statsMu.Lock()
	e.stats.RulesRegistered--
	delete(e.stats.RuleExecutionTime, ruleID)
	e.statsMu.Unlock()

	// Remove cooldown
	e.cooldownMu.Lock()
	delete(e.ruleCooldowns, ruleID)
	e.cooldownMu.Unlock()

	return nil
}

// GetRule retrieves a rule by ID
func (e *BaseCorrelationEngine) GetRule(ruleID string) (*Rule, bool) {
	e.rulesMu.RLock()
	defer e.rulesMu.RUnlock()

	rule, exists := e.rules[ruleID]
	return rule, exists
}

// ListRules returns all registered rules
func (e *BaseCorrelationEngine) ListRules() []*Rule {
	e.rulesMu.RLock()
	defer e.rulesMu.RUnlock()

	rules := make([]*Rule, 0, len(e.rules))
	for _, rule := range e.rules {
		rules = append(rules, rule)
	}

	// Sort by name for consistent ordering
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Name < rules[j].Name
	})

	return rules
}

// EnableRule enables a rule
func (e *BaseCorrelationEngine) EnableRule(ruleID string) error {
	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()

	rule, exists := e.rules[ruleID]
	if !exists {
		return fmt.Errorf("rule with ID '%s' not found", ruleID)
	}

	rule.Enabled = true
	return nil
}

// DisableRule disables a rule
func (e *BaseCorrelationEngine) DisableRule(ruleID string) error {
	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()

	rule, exists := e.rules[ruleID]
	if !exists {
		return fmt.Errorf("rule with ID '%s' not found", ruleID)
	}

	rule.Enabled = false
	return nil
}

// ProcessEvents processes a batch of events through all rules
func (e *BaseCorrelationEngine) ProcessEvents(ctx context.Context, events []Event) ([]*Result, error) {
	if len(events) == 0 {
		return nil, nil
	}

	// Calculate time window from events
	start := events[0].Timestamp
	end := events[0].Timestamp

	for _, event := range events {
		if event.Timestamp.Before(start) {
			start = event.Timestamp
		}
		if event.Timestamp.After(end) {
			end = event.Timestamp
		}
	}

	// Extend window to ensure we don't miss correlations
	window := TimeWindow{
		Start: start.Add(-e.windowSize / 2),
		End:   end.Add(e.windowSize / 2),
	}

	return e.ProcessWindow(ctx, window, events)
}

// ProcessWindow processes events within a specific time window
func (e *BaseCorrelationEngine) ProcessWindow(ctx context.Context, window TimeWindow, events []Event) ([]*Result, error) {
	start := time.Now()
	defer func() {
		e.statsMu.Lock()
		e.stats.ProcessingLatency = time.Since(start)
		e.stats.LastProcessedAt = time.Now()
		e.stats.EventsProcessed += uint64(len(events))
		e.statsMu.Unlock()
	}()

	// Get enabled rules
	enabledRules := e.getEnabledRules()
	if len(enabledRules) == 0 {
		return nil, nil
	}

	// Create correlation context
	corrCtx := e.createContext(window, events)
	defer e.releaseContext(corrCtx)

	// Execute rules concurrently
	results := e.executeRules(ctx, enabledRules, corrCtx)

	// Process results
	for _, result := range results {
		if result != nil {
			e.handleResult(ctx, result)
		}
	}

	return results, nil
}

// getEnabledRules returns all enabled rules that are not in cooldown
func (e *BaseCorrelationEngine) getEnabledRules() []*Rule {
	e.rulesMu.RLock()
	defer e.rulesMu.RUnlock()

	e.cooldownMu.RLock()
	defer e.cooldownMu.RUnlock()

	now := time.Now()
	var enabledRules []*Rule

	for _, rule := range e.rules {
		if !rule.Enabled {
			continue
		}

		// Check cooldown
		if lastExecution, inCooldown := e.ruleCooldowns[rule.ID]; inCooldown {
			if now.Sub(lastExecution) < rule.Cooldown {
				continue
			}
		}

		enabledRules = append(enabledRules, rule)
	}

	return enabledRules
}

// createContext creates a correlation context from the pool
func (e *BaseCorrelationEngine) createContext(window TimeWindow, events []Event) *Context {
	ctx := e.contextPool.Get().(*Context)

	// Reset and initialize
	ctx.Window = window
	ctx.events = events
	ctx.CorrelationID = fmt.Sprintf("corr-%d", time.Now().UnixNano())

	// Clear maps
	for k := range ctx.metrics {
		delete(ctx.metrics, k)
	}
	for k := range ctx.eventsBySource {
		delete(ctx.eventsBySource, k)
	}
	for k := range ctx.eventsByType {
		delete(ctx.eventsByType, k)
	}
	for k := range ctx.eventsByEntity {
		delete(ctx.eventsByEntity, k)
	}
	for k := range ctx.Metadata {
		delete(ctx.Metadata, k)
	}

	// Rebuild indices
	ctx.buildIndices()

	return ctx
}

// releaseContext returns a context to the pool
func (e *BaseCorrelationEngine) releaseContext(ctx *Context) {
	e.contextPool.Put(ctx)
}

// executeRules executes rules concurrently with rate limiting
func (e *BaseCorrelationEngine) executeRules(ctx context.Context, rules []*Rule, corrCtx *Context) []*Result {
	// Create semaphore for concurrency control
	semaphore := make(chan struct{}, e.maxConcurrentRules)

	// Results channel
	resultsChan := make(chan *Result, len(rules))

	// Execute rules
	var wg sync.WaitGroup
	for _, rule := range rules {
		wg.Add(1)
		go func(r *Rule) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}

			// Execute rule
			result := e.executeRule(ctx, r, corrCtx)
			if result != nil {
				resultsChan <- result
			}
		}(rule)
	}

	// Wait for completion
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	var results []*Result
	for result := range resultsChan {
		results = append(results, result)
	}

	return results
}

// executeRule executes a single rule
func (e *BaseCorrelationEngine) executeRule(ctx context.Context, rule *Rule, corrCtx *Context) *Result {
	execution := e.ruleExecutionPool.Get().(*ruleExecution)
	defer e.ruleExecutionPool.Put(execution)

	execution.rule = rule
	execution.context = corrCtx
	execution.startTime = time.Now()
	execution.result = nil
	execution.err = nil

	// Set rule context
	corrCtx.RuleID = rule.ID

	// Execute with timeout and panic recovery
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				execution.err = fmt.Errorf("rule %s panicked: %v", rule.ID, r)
			}
			close(done)
		}()

		execution.result = rule.Evaluate(corrCtx)
	}()

	// Wait for completion with timeout
	select {
	case <-done:
		// Rule completed
	case <-ctx.Done():
		execution.err = ctx.Err()
	case <-time.After(30 * time.Second): // Rule timeout
		execution.err = fmt.Errorf("rule %s timed out", rule.ID)
	}

	execution.duration = time.Since(execution.startTime)

	// Update rule performance metrics
	e.updateRulePerformance(rule, execution)

	// Update cooldown
	if execution.result != nil {
		e.cooldownMu.Lock()
		e.ruleCooldowns[rule.ID] = time.Now()
		e.cooldownMu.Unlock()
	}

	// Validate result
	if execution.result != nil && execution.err == nil {
		if execution.result.Confidence < rule.MinConfidence {
			return nil // Result doesn't meet confidence threshold
		}

		// Set metadata
		execution.result.RuleID = rule.ID
		execution.result.RuleName = rule.Name
		execution.result.Category = rule.Category
		execution.result.Timestamp = time.Now()
		execution.result.TTL = rule.TTL

		return execution.result
	}

	return nil
}

// updateRulePerformance updates performance metrics for a rule
func (e *BaseCorrelationEngine) updateRulePerformance(rule *Rule, execution *ruleExecution) {
	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()

	// Update rule performance
	rule.LastExecuted = execution.startTime
	rule.ExecutionCount++

	// Update performance metrics
	perf := &rule.Performance

	if perf.MinExecutionTime == 0 || execution.duration < perf.MinExecutionTime {
		perf.MinExecutionTime = execution.duration
	}

	if execution.duration > perf.MaxExecutionTime {
		perf.MaxExecutionTime = execution.duration
	}

	perf.TotalExecutionTime += execution.duration
	perf.AverageExecutionTime = perf.TotalExecutionTime / time.Duration(rule.ExecutionCount)

	if execution.err == nil && execution.result != nil {
		successCount := float64(rule.ExecutionCount) * perf.SuccessRate
		successCount++ // This execution was successful
		perf.SuccessRate = successCount / float64(rule.ExecutionCount)
	} else {
		successCount := float64(rule.ExecutionCount) * perf.SuccessRate
		perf.SuccessRate = successCount / float64(rule.ExecutionCount)
	}

	// Update global stats
	e.statsMu.Lock()
	e.stats.RuleExecutionTime[rule.ID] = perf.AverageExecutionTime
	if execution.result != nil {
		e.stats.CorrelationsFound++
	}
	e.statsMu.Unlock()

	// Record metrics
	if e.metricsCollector != nil {
		e.metricsCollector.RecordRuleExecution(rule.ID, execution.duration, execution.err == nil)
		if execution.result != nil {
			e.metricsCollector.RecordRuleResult(rule.ID, execution.result)
			e.metricsCollector.RecordCorrelationFound(execution.result.Category, execution.result.Severity)
		}
	}
}

// handleResult processes a correlation result
func (e *BaseCorrelationEngine) handleResult(ctx context.Context, result *Result) {
	// Send to result channel for async processing
	select {
	case e.resultChan <- result:
	default:
		// Channel is full, log warning but don't block
		// In production, you'd want proper logging here
	}
}

// SetWindowSize sets the correlation window size
func (e *BaseCorrelationEngine) SetWindowSize(duration time.Duration) {
	e.windowSize = duration
}

// SetProcessingInterval sets the processing interval
func (e *BaseCorrelationEngine) SetProcessingInterval(interval time.Duration) {
	e.processingInterval = interval
}

// SetMaxConcurrentRules sets the maximum concurrent rules
func (e *BaseCorrelationEngine) SetMaxConcurrentRules(limit int) {
	e.maxConcurrentRules = limit
}

// GetStats returns engine statistics
func (e *BaseCorrelationEngine) GetStats() Stats {
	e.statsMu.RLock()
	defer e.statsMu.RUnlock()

	stats := e.stats
	stats.MemoryUsage = e.getMemoryUsage()

	return stats
}

// GetRuleStats returns performance statistics for a specific rule
func (e *BaseCorrelationEngine) GetRuleStats(ruleID string) (RulePerformance, error) {
	e.rulesMu.RLock()
	defer e.rulesMu.RUnlock()

	rule, exists := e.rules[ruleID]
	if !exists {
		return RulePerformance{}, fmt.Errorf("rule with ID '%s' not found", ruleID)
	}

	return rule.Performance, nil
}

// getMemoryUsage returns current memory usage
func (e *BaseCorrelationEngine) getMemoryUsage() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}

// Start starts the correlation engine
func (e *BaseCorrelationEngine) Start(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)

	// Start result processor
	e.wg.Add(1)
	go e.processResults()

	return nil
}

// Stop stops the correlation engine
func (e *BaseCorrelationEngine) Stop() error {
	if e.cancel != nil {
		e.cancel()
	}

	// Close result channel
	close(e.resultChan)

	// Wait for goroutines to finish
	e.wg.Wait()

	return nil
}

// Health checks the health of the correlation engine
func (e *BaseCorrelationEngine) Health() error {
	// Check if context is cancelled
	if e.ctx != nil && e.ctx.Err() != nil {
		return fmt.Errorf("engine is stopped: %w", e.ctx.Err())
	}

	// Check event store health
	if e.eventStore != nil {
		// You'd implement a health check on the event store
		// For now, we'll just return nil
	}

	return nil
}

// processResults processes correlation results asynchronously
func (e *BaseCorrelationEngine) processResults() {
	defer e.wg.Done()

	for {
		select {
		case result, ok := <-e.resultChan:
			if !ok {
				return // Channel closed
			}

			// Process result through handlers
			for _, handler := range e.resultHandlers {
				if err := handler.HandleResult(e.ctx, result); err != nil {
					// Log error but continue processing
					// In production, you'd want proper logging here
				}
			}

		case <-e.ctx.Done():
			return
		}
	}
}
