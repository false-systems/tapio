package core

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/domain"
)

// CoreEngine implements the core correlation engine with no external dependencies
type CoreEngine struct {
	// Configuration
	config domain.Config
	
	// Dependencies injected via constructor
	eventStore       domain.EventStore
	resultHandlers   []domain.ResultHandler
	metricsCollector domain.MetricsCollector
	logger           domain.Logger
	
	// Rules management
	rules   map[string]domain.Rule
	rulesMu sync.RWMutex
	
	// Statistics
	stats   domain.Stats
	statsMu sync.RWMutex
	
	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	
	// Performance optimization
	contextPool *sync.Pool
	
	// Rate limiting
	ruleCooldowns map[string]time.Time
	cooldownMu    sync.RWMutex
}

// NewCoreEngine creates a new core correlation engine
func NewCoreEngine(
	config domain.Config,
	eventStore domain.EventStore,
	metricsCollector domain.MetricsCollector,
	logger domain.Logger,
) *CoreEngine {
	if logger == nil {
		logger = &noOpLogger{}
	}
	
	engine := &CoreEngine{
		config:           config,
		eventStore:       eventStore,
		metricsCollector: metricsCollector,
		logger:           logger,
		rules:            make(map[string]domain.Rule),
		ruleCooldowns:    make(map[string]time.Time),
		resultHandlers:   make([]domain.ResultHandler, 0),
		stats: domain.Stats{
			RuleExecutionTime: make(map[string]time.Duration),
		},
	}
	
	// Initialize context pool for performance
	engine.contextPool = &sync.Pool{
		New: func() interface{} {
			return &domain.Context{
				Metadata: make(map[string]string),
			}
		},
	}
	
	return engine
}

// RegisterRule registers a correlation rule
func (e *CoreEngine) RegisterRule(rule domain.Rule) error {
	if rule == nil {
		return fmt.Errorf("rule cannot be nil")
	}
	
	if rule.ID() == "" {
		return fmt.Errorf("rule ID cannot be empty")
	}
	
	if rule.Name() == "" {
		return fmt.Errorf("rule name cannot be empty")
	}
	
	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()
	
	if _, exists := e.rules[rule.ID()]; exists {
		return fmt.Errorf("rule with ID '%s' already exists", rule.ID())
	}
	
	e.rules[rule.ID()] = rule
	
	e.statsMu.Lock()
	e.stats.RulesActive++
	e.statsMu.Unlock()
	
	e.logger.Info("registered correlation rule", "rule_id", rule.ID(), "rule_name", rule.Name())
	
	return nil
}

// UnregisterRule removes a correlation rule
func (e *CoreEngine) UnregisterRule(ruleID string) error {
	e.rulesMu.Lock()
	defer e.rulesMu.Unlock()
	
	if _, exists := e.rules[ruleID]; !exists {
		return fmt.Errorf("rule with ID '%s' not found", ruleID)
	}
	
	delete(e.rules, ruleID)
	
	e.statsMu.Lock()
	e.stats.RulesActive--
	delete(e.stats.RuleExecutionTime, ruleID)
	e.statsMu.Unlock()
	
	// Remove cooldown
	e.cooldownMu.Lock()
	delete(e.ruleCooldowns, ruleID)
	e.cooldownMu.Unlock()
	
	e.logger.Info("unregistered correlation rule", "rule_id", ruleID)
	
	return nil
}

// GetRules returns all registered rules
func (e *CoreEngine) GetRules() []domain.Rule {
	e.rulesMu.RLock()
	defer e.rulesMu.RUnlock()
	
	rules := make([]domain.Rule, 0, len(e.rules))
	for _, rule := range e.rules {
		rules = append(rules, rule)
	}
	
	// Sort by rule ID for consistent ordering
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].ID() < rules[j].ID()
	})
	
	return rules
}

// AddResultHandler adds a result handler
func (e *CoreEngine) AddResultHandler(handler domain.ResultHandler) {
	e.resultHandlers = append(e.resultHandlers, handler)
}

// ProcessEvents processes a batch of events through correlation rules
func (e *CoreEngine) ProcessEvents(ctx context.Context, events []domain.Event) ([]*domain.Result, error) {
	if len(events) == 0 {
		return nil, nil
	}
	
	startTime := time.Now()
	defer func() {
		e.statsMu.Lock()
		e.stats.ProcessingLatency = time.Since(startTime)
		e.stats.LastProcessedAt = time.Now()
		e.stats.EventsProcessed += uint64(len(events))
		e.statsMu.Unlock()
		
		if e.metricsCollector != nil {
			e.metricsCollector.RecordEngineStats(e.GetStats())
		}
	}()
	
	// Calculate time window from events
	window := e.calculateTimeWindow(events)
	
	// Get enabled rules
	enabledRules := e.getEnabledRules()
	if len(enabledRules) == 0 {
		return nil, nil
	}
	
	e.logger.Debug("processing events", "event_count", len(events), "rule_count", len(enabledRules))
	
	// Execute rules concurrently
	results := e.executeRules(ctx, enabledRules, window, events)
	
	// Handle results
	for _, result := range results {
		if result != nil {
			e.handleResult(ctx, result)
		}
	}
	
	return results, nil
}

// calculateTimeWindow calculates the time window from events
func (e *CoreEngine) calculateTimeWindow(events []domain.Event) domain.TimeWindow {
	if len(events) == 0 {
		now := time.Now()
		return domain.TimeWindow{
			Start: now.Add(-e.config.WindowSize),
			End:   now,
		}
	}
	
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
	
	// Extend window to ensure we capture correlations
	return domain.TimeWindow{
		Start: start.Add(-e.config.WindowSize / 2),
		End:   end.Add(e.config.WindowSize / 2),
	}
}

// getEnabledRules returns all enabled rules not in cooldown
func (e *CoreEngine) getEnabledRules() []domain.Rule {
	e.rulesMu.RLock()
	defer e.rulesMu.RUnlock()
	
	e.cooldownMu.RLock()
	defer e.cooldownMu.RUnlock()
	
	now := time.Now()
	var enabledRules []domain.Rule
	
	for _, rule := range e.rules {
		if !rule.IsEnabled() {
			continue
		}
		
		// Check cooldown
		if lastExecution, inCooldown := e.ruleCooldowns[rule.ID()]; inCooldown {
			if now.Sub(lastExecution) < rule.GetCooldown() {
				continue
			}
		}
		
		enabledRules = append(enabledRules, rule)
	}
	
	return enabledRules
}

// executeRules executes rules concurrently
func (e *CoreEngine) executeRules(ctx context.Context, rules []domain.Rule, window domain.TimeWindow, events []domain.Event) []*domain.Result {
	// Create semaphore for concurrency control
	semaphore := make(chan struct{}, e.config.MaxConcurrentRules)
	
	// Results channel
	resultsChan := make(chan *domain.Result, len(rules))
	
	// Execute rules
	var wg sync.WaitGroup
	for _, rule := range rules {
		wg.Add(1)
		go func(r domain.Rule) {
			defer wg.Done()
			
			// Acquire semaphore
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			
			// Execute rule
			result := e.executeRule(ctx, r, window, events)
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
	var results []*domain.Result
	for result := range resultsChan {
		results = append(results, result)
	}
	
	return results
}

// executeRule executes a single rule
func (e *CoreEngine) executeRule(ctx context.Context, rule domain.Rule, window domain.TimeWindow, events []domain.Event) *domain.Result {
	startTime := time.Now()
	
	// Create context from pool
	corrCtx := e.contextPool.Get().(*domain.Context)
	defer e.contextPool.Put(corrCtx)
	
	// Reset and initialize context
	corrCtx.Window = window
	corrCtx.Events = events
	corrCtx.RuleID = rule.ID()
	corrCtx.CorrelationID = fmt.Sprintf("corr-%d", time.Now().UnixNano())
	
	// Clear metadata
	for k := range corrCtx.Metadata {
		delete(corrCtx.Metadata, k)
	}
	
	// Execute with timeout and panic recovery
	var result *domain.Result
	var err error
	
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("rule %s panicked: %v", rule.ID(), r)
			}
			close(done)
		}()
		
		result = rule.Evaluate(corrCtx)
	}()
	
	// Wait for completion with timeout
	select {
	case <-done:
		// Rule completed
	case <-ctx.Done():
		err = ctx.Err()
	case <-time.After(30 * time.Second):
		err = fmt.Errorf("rule %s timed out", rule.ID())
	}
	
	duration := time.Since(startTime)
	
	// Update statistics
	e.updateRuleStats(rule, duration, result != nil, err)
	
	// Update cooldown if result generated
	if result != nil && err == nil {
		e.cooldownMu.Lock()
		e.ruleCooldowns[rule.ID()] = time.Now()
		e.cooldownMu.Unlock()
	}
	
	// Validate result
	if result != nil && err == nil {
		if result.Confidence < rule.GetMinConfidence() {
			e.logger.Debug("rule result below confidence threshold", 
				"rule_id", rule.ID(), 
				"confidence", result.Confidence, 
				"min_confidence", rule.GetMinConfidence())
			return nil
		}
		
		// Set result metadata
		result.RuleID = rule.ID()
		result.RuleName = rule.Name()
		result.Timestamp = time.Now()
		
		e.logger.Debug("rule generated result", 
			"rule_id", rule.ID(), 
			"confidence", result.Confidence,
			"duration", duration)
		
		return result
	}
	
	if err != nil {
		e.logger.Error("rule execution failed", "rule_id", rule.ID(), "error", err)
	}
	
	return nil
}

// updateRuleStats updates rule performance statistics
func (e *CoreEngine) updateRuleStats(rule domain.Rule, duration time.Duration, success bool, err error) {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	
	e.stats.RulesExecuted++
	
	if success {
		e.stats.RulesMatched++
		e.stats.CorrelationsFound++
	}
	
	if err != nil {
		e.stats.RulesFailed++
	}
	
	// Update rule-specific timing
	e.stats.RuleExecutionTime[rule.ID()] = duration
	
	if e.metricsCollector != nil {
		e.metricsCollector.RecordRuleExecution(rule.ID(), duration, success)
	}
}

// handleResult processes a correlation result
func (e *CoreEngine) handleResult(ctx context.Context, result *domain.Result) {
	// Process through all handlers
	for _, handler := range e.resultHandlers {
		if err := handler.HandleResult(ctx, result); err != nil {
			e.logger.Error("result handler failed", 
				"handler_type", handler.GetHandlerType(),
				"result_id", result.ID,
				"error", err)
		}
	}
}

// Start starts the correlation engine
func (e *CoreEngine) Start(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)
	
	e.logger.Info("starting correlation engine", 
		"window_size", e.config.WindowSize,
		"processing_interval", e.config.ProcessingInterval)
	
	// Start metrics collection if enabled
	if e.config.EnableMetrics && e.metricsCollector != nil {
		e.wg.Add(1)
		go e.collectMetrics()
	}
	
	return nil
}

// Stop stops the correlation engine
func (e *CoreEngine) Stop() error {
	e.logger.Info("stopping correlation engine")
	
	if e.cancel != nil {
		e.cancel()
	}
	
	e.wg.Wait()
	
	return nil
}

// collectMetrics collects and reports metrics periodically
func (e *CoreEngine) collectMetrics() {
	defer e.wg.Done()
	
	ticker := time.NewTicker(e.config.MetricsInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			stats := e.GetStats()
			if e.metricsCollector != nil {
				e.metricsCollector.RecordEngineStats(stats)
			}
		}
	}
}

// GetStats returns current engine statistics
func (e *CoreEngine) GetStats() domain.Stats {
	e.statsMu.RLock()
	defer e.statsMu.RUnlock()
	
	stats := e.stats
	stats.MemoryUsage = e.getMemoryUsage()
	
	return stats
}

// getMemoryUsage returns current memory usage
func (e *CoreEngine) getMemoryUsage() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}

// Health checks the engine health
func (e *CoreEngine) Health() error {
	if e.ctx != nil && e.ctx.Err() != nil {
		return fmt.Errorf("engine is stopped: %w", e.ctx.Err())
	}
	
	return nil
}

// noOpLogger is a no-op logger implementation
type noOpLogger struct{}

func (l *noOpLogger) Debug(msg string, fields ...interface{}) {}
func (l *noOpLogger) Info(msg string, fields ...interface{})  {}
func (l *noOpLogger) Warn(msg string, fields ...interface{})  {}
func (l *noOpLogger) Error(msg string, fields ...interface{}) {}
func (l *noOpLogger) With(fields ...interface{}) domain.Logger { return l }