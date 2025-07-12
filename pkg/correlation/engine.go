package correlation

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ExecutionMode defines how the engine executes rules
type ExecutionMode int

const (
	// ExecutionModeSequential executes rules one by one
	ExecutionModeSequential ExecutionMode = iota
	// ExecutionModeParallel executes rules in parallel
	ExecutionModeParallel
	// ExecutionModeAdaptive chooses execution mode based on rule characteristics
	ExecutionModeAdaptive
)

// EngineConfig configures the correlation engine
type EngineConfig struct {
	ExecutionMode       ExecutionMode `json:"execution_mode"`
	MaxWorkers          int           `json:"max_workers"`
	Timeout             time.Duration `json:"timeout"`
	CacheTTL            time.Duration `json:"cache_ttl"`
	EnableMetrics       bool          `json:"enable_metrics"`
	RetryAttempts       int           `json:"retry_attempts"`
	RetryDelay          time.Duration `json:"retry_delay"`
	MaxHistoryEntries   int           `json:"max_history_entries"`
	HistoryRetentionTTL time.Duration `json:"history_retention_ttl"`
}

// DefaultEngineConfig returns a default engine configuration
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		ExecutionMode:       ExecutionModeAdaptive,
		MaxWorkers:          10,
		Timeout:             30 * time.Second,
		CacheTTL:            5 * time.Minute,
		EnableMetrics:       true,
		RetryAttempts:       3,
		RetryDelay:          time.Second,
		MaxHistoryEntries:   100,
		HistoryRetentionTTL: 24 * time.Hour,
	}
}

// ExecutionResult represents the result of rule execution
type ExecutionResult struct {
	Rule      Rule          `json:"rule"`
	Findings  []Finding     `json:"findings"`
	Error     error         `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
}

// EngineMetrics tracks engine performance metrics
type EngineMetrics struct {
	TotalExecutions      int64                   `json:"total_executions"`
	SuccessfulExecutions int64                   `json:"successful_executions"`
	FailedExecutions     int64                   `json:"failed_executions"`
	TotalFindings        int64                   `json:"total_findings"`
	AverageExecutionTime time.Duration           `json:"average_execution_time"`
	LastExecutionTime    time.Time               `json:"last_execution_time"`
	RuleMetrics          map[string]*RuleMetrics `json:"rule_metrics"`
}

// RuleMetrics tracks metrics for individual rules
type RuleMetrics struct {
	ExecutionCount       int64         `json:"execution_count"`
	SuccessCount         int64         `json:"success_count"`
	FailureCount         int64         `json:"failure_count"`
	TotalFindings        int64         `json:"total_findings"`
	AverageExecutionTime time.Duration `json:"average_execution_time"`
	LastExecutionTime    time.Time     `json:"last_execution_time"`
	AverageConfidence    float64       `json:"average_confidence"`
}

// Engine is the main correlation engine
type Engine struct {
	config           EngineConfig
	registry         *RuleRegistry
	dataCollection   *DataCollection
	metrics          *EngineMetrics
	findings         []Finding
	findingsMutex    sync.RWMutex
	metricsMutex     sync.RWMutex
	executionHistory []ExecutionResult
	historyMutex     sync.RWMutex
}

// NewEngine creates a new correlation engine
func NewEngine(config EngineConfig, registry *RuleRegistry, dataCollection *DataCollection) *Engine {
	return &Engine{
		config:         config,
		registry:       registry,
		dataCollection: dataCollection,
		metrics: &EngineMetrics{
			RuleMetrics: make(map[string]*RuleMetrics),
		},
		findings:         make([]Finding, 0),
		executionHistory: make([]ExecutionResult, 0),
	}
}

// Execute runs all enabled rules and returns findings
func (e *Engine) Execute(ctx context.Context) ([]Finding, error) {
	startTime := time.Now()

	// Create execution context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	// Get enabled rules
	rules := e.registry.GetEnabledRules()
	if len(rules) == 0 {
		return []Finding{}, nil
	}

	// Filter rules based on available data sources
	availableRules, err := e.filterRulesByRequirements(execCtx, rules)
	if err != nil {
		return nil, fmt.Errorf("failed to filter rules: %w", err)
	}

	// Execute rules based on execution mode
	var results []ExecutionResult
	switch e.config.ExecutionMode {
	case ExecutionModeSequential:
		results = e.executeSequential(execCtx, availableRules)
	case ExecutionModeParallel:
		results = e.executeParallel(execCtx, availableRules)
	case ExecutionModeAdaptive:
		results = e.executeAdaptive(execCtx, availableRules)
	}

	// Process results
	findings := e.processResults(results)

	// Update metrics
	e.updateMetrics(results, time.Since(startTime))

	// Store findings
	e.findingsMutex.Lock()
	e.findings = findings
	e.findingsMutex.Unlock()

	// Store execution history
	e.historyMutex.Lock()
	e.executionHistory = append(e.executionHistory, results...)
	e.cleanupExecutionHistory()
	e.historyMutex.Unlock()

	return findings, nil
}

// ExecuteRule executes a specific rule by ID
func (e *Engine) ExecuteRule(ctx context.Context, ruleID string) ([]Finding, error) {
	rule, exists := e.registry.GetRule(ruleID)
	if !exists {
		return nil, NewRuleNotFoundError(ruleID)
	}

	if !e.registry.IsEnabled(ruleID) {
		return nil, fmt.Errorf("rule %s is disabled", ruleID)
	}

	// Check requirements
	if err := rule.CheckRequirements(ctx, e.dataCollection); err != nil {
		return nil, err
	}

	// Execute rule
	result := e.executeRule(ctx, rule)

	// Update metrics
	e.updateRuleMetrics(result)

	return result.Findings, result.Error
}

// GetFindings returns current findings
func (e *Engine) GetFindings() []Finding {
	e.findingsMutex.RLock()
	defer e.findingsMutex.RUnlock()

	// Return a copy to prevent modifications
	findings := make([]Finding, len(e.findings))
	copy(findings, e.findings)
	return findings
}

// GetFindingsByRule returns findings for a specific rule
func (e *Engine) GetFindingsByRule(ruleID string) []Finding {
	e.findingsMutex.RLock()
	defer e.findingsMutex.RUnlock()

	var ruleFindings []Finding
	for _, finding := range e.findings {
		if finding.RuleID == ruleID {
			ruleFindings = append(ruleFindings, finding)
		}
	}
	return ruleFindings
}

// GetFindingsBySeverity returns findings of a specific severity
func (e *Engine) GetFindingsBySeverity(severity Severity) []Finding {
	e.findingsMutex.RLock()
	defer e.findingsMutex.RUnlock()

	var severityFindings []Finding
	for _, finding := range e.findings {
		if finding.Severity == severity {
			severityFindings = append(severityFindings, finding)
		}
	}
	return severityFindings
}

// GetMetrics returns current engine metrics
func (e *Engine) GetMetrics() EngineMetrics {
	e.metricsMutex.RLock()
	defer e.metricsMutex.RUnlock()

	// Return a copy to prevent modifications
	metrics := *e.metrics
	ruleMetrics := make(map[string]*RuleMetrics)
	for k, v := range e.metrics.RuleMetrics {
		ruleMetrics[k] = &(*v) // Create a copy
	}
	metrics.RuleMetrics = ruleMetrics

	return metrics
}

// GetExecutionHistory returns recent execution history
func (e *Engine) GetExecutionHistory() []ExecutionResult {
	e.historyMutex.RLock()
	defer e.historyMutex.RUnlock()

	// Return a copy to prevent modifications
	history := make([]ExecutionResult, len(e.executionHistory))
	copy(history, e.executionHistory)
	return history
}

// ClearFindings clears all current findings
func (e *Engine) ClearFindings() {
	e.findingsMutex.Lock()
	defer e.findingsMutex.Unlock()
	e.findings = make([]Finding, 0)
}

// filterRulesByRequirements filters rules based on available data sources
func (e *Engine) filterRulesByRequirements(ctx context.Context, rules []Rule) ([]Rule, error) {
	var availableRules []Rule

	for _, rule := range rules {
		if err := rule.CheckRequirements(ctx, e.dataCollection); err != nil {
			// Skip rules with unmet requirements
			continue
		}
		availableRules = append(availableRules, rule)
	}

	return availableRules, nil
}

// executeSequential executes rules sequentially
func (e *Engine) executeSequential(ctx context.Context, rules []Rule) []ExecutionResult {
	results := make([]ExecutionResult, 0, len(rules))

	for _, rule := range rules {
		result := e.executeRule(ctx, rule)
		results = append(results, result)

		// Check for context cancellation
		if ctx.Err() != nil {
			break
		}
	}

	return results
}

// executeParallel executes rules in parallel
func (e *Engine) executeParallel(ctx context.Context, rules []Rule) []ExecutionResult {
	results := make([]ExecutionResult, len(rules))
	var wg sync.WaitGroup

	// Create semaphore for limiting concurrent executions
	semaphore := make(chan struct{}, e.config.MaxWorkers)

	for i, rule := range rules {
		wg.Add(1)
		go func(index int, r Rule) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			results[index] = e.executeRule(ctx, r)
		}(i, rule)
	}

	wg.Wait()
	return results
}

// executeAdaptive chooses execution mode based on rule characteristics
func (e *Engine) executeAdaptive(ctx context.Context, rules []Rule) []ExecutionResult {
	// Simple heuristic: use parallel for multiple rules, sequential for few
	if len(rules) > 3 {
		return e.executeParallel(ctx, rules)
	}
	return e.executeSequential(ctx, rules)
}

// executeRule executes a single rule
func (e *Engine) executeRule(ctx context.Context, rule Rule) ExecutionResult {
	startTime := time.Now()

	// Create rule context
	ruleCtx := &RuleContext{
		DataCollection:   e.dataCollection,
		PreviousFindings: e.GetFindings(),
		ExecutionTime:    startTime,
		Metadata:         make(map[string]interface{}),
	}

	// Execute rule with retries
	var findings []Finding
	var err error

	for attempt := 0; attempt <= e.config.RetryAttempts; attempt++ {
		findings, err = rule.Execute(ctx, ruleCtx)
		if err == nil {
			break
		}

		if attempt < e.config.RetryAttempts {
			select {
			case <-ctx.Done():
				return ExecutionResult{
					Rule:      rule,
					Findings:  nil,
					Error:     ctx.Err(),
					Duration:  time.Since(startTime),
					Timestamp: startTime,
				}
			case <-time.After(e.config.RetryDelay):
				// Continue to next attempt
			}
		}
	}

	return ExecutionResult{
		Rule:      rule,
		Findings:  findings,
		Error:     err,
		Duration:  time.Since(startTime),
		Timestamp: startTime,
	}
}

// processResults processes execution results and consolidates findings
func (e *Engine) processResults(results []ExecutionResult) []Finding {
	var allFindings []Finding

	for _, result := range results {
		if result.Error == nil {
			allFindings = append(allFindings, result.Findings...)
		}
	}

	// Sort findings by severity and confidence
	sort.Slice(allFindings, func(i, j int) bool {
		if allFindings[i].Severity != allFindings[j].Severity {
			return allFindings[i].Severity > allFindings[j].Severity
		}
		return allFindings[i].Confidence > allFindings[j].Confidence
	})

	return allFindings
}

// updateMetrics updates engine metrics
func (e *Engine) updateMetrics(results []ExecutionResult, totalDuration time.Duration) {
	e.metricsMutex.Lock()
	defer e.metricsMutex.Unlock()

	var totalFindings int64
	var successfulExecutions int64
	var failedExecutions int64

	for _, result := range results {
		if result.Error == nil {
			successfulExecutions++
			totalFindings += int64(len(result.Findings))
		} else {
			failedExecutions++
		}

		// Update rule-specific metrics
		e.updateRuleMetricsLocked(result)
	}

	// Update overall metrics
	e.metrics.TotalExecutions += int64(len(results))
	e.metrics.SuccessfulExecutions += successfulExecutions
	e.metrics.FailedExecutions += failedExecutions
	e.metrics.TotalFindings += totalFindings
	e.metrics.LastExecutionTime = time.Now()

	// Update average execution time
	if e.metrics.TotalExecutions > 0 {
		totalTime := int64(e.metrics.AverageExecutionTime) * (e.metrics.TotalExecutions - int64(len(results)))
		totalTime += int64(totalDuration)
		e.metrics.AverageExecutionTime = time.Duration(totalTime / e.metrics.TotalExecutions)
	}
}

// updateRuleMetrics updates metrics for a single rule
func (e *Engine) updateRuleMetrics(result ExecutionResult) {
	e.metricsMutex.Lock()
	defer e.metricsMutex.Unlock()
	e.updateRuleMetricsLocked(result)
}

// updateRuleMetricsLocked updates rule metrics (assumes lock is held)
func (e *Engine) updateRuleMetricsLocked(result ExecutionResult) {
	ruleID := result.Rule.GetMetadata().ID

	if _, exists := e.metrics.RuleMetrics[ruleID]; !exists {
		e.metrics.RuleMetrics[ruleID] = &RuleMetrics{}
	}

	ruleMetrics := e.metrics.RuleMetrics[ruleID]
	ruleMetrics.ExecutionCount++
	ruleMetrics.LastExecutionTime = result.Timestamp

	if result.Error == nil {
		ruleMetrics.SuccessCount++
		ruleMetrics.TotalFindings += int64(len(result.Findings))

		// Update average confidence
		if len(result.Findings) > 0 {
			var totalConfidence float64
			for _, finding := range result.Findings {
				totalConfidence += finding.Confidence
			}
			avgConfidence := totalConfidence / float64(len(result.Findings))

			if ruleMetrics.SuccessCount == 1 {
				ruleMetrics.AverageConfidence = avgConfidence
			} else {
				ruleMetrics.AverageConfidence = (ruleMetrics.AverageConfidence*float64(ruleMetrics.SuccessCount-1) + avgConfidence) / float64(ruleMetrics.SuccessCount)
			}
		}
	} else {
		ruleMetrics.FailureCount++
	}

	// Update average execution time
	if ruleMetrics.ExecutionCount == 1 {
		ruleMetrics.AverageExecutionTime = result.Duration
	} else {
		totalTime := int64(ruleMetrics.AverageExecutionTime) * (ruleMetrics.ExecutionCount - 1)
		totalTime += int64(result.Duration)
		ruleMetrics.AverageExecutionTime = time.Duration(totalTime / ruleMetrics.ExecutionCount)
	}
}

// cleanupExecutionHistory removes old execution history entries based on size and time limits
// This method assumes the historyMutex is already held
func (e *Engine) cleanupExecutionHistory() {
	if len(e.executionHistory) == 0 {
		return
	}

	// Remove entries older than TTL
	if e.config.HistoryRetentionTTL > 0 {
		cutoffTime := time.Now().Add(-e.config.HistoryRetentionTTL)
		validEntries := make([]ExecutionResult, 0, len(e.executionHistory))
		
		for _, result := range e.executionHistory {
			if result.Timestamp.After(cutoffTime) {
				validEntries = append(validEntries, result)
			}
		}
		
		e.executionHistory = validEntries
	}

	// Limit by maximum number of entries
	if e.config.MaxHistoryEntries > 0 && len(e.executionHistory) > e.config.MaxHistoryEntries {
		startIndex := len(e.executionHistory) - e.config.MaxHistoryEntries
		e.executionHistory = e.executionHistory[startIndex:]
	}
}
