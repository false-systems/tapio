package opinions

import (
	"fmt"
	"strings"
	"time"
)

// OpinionValidator validates opinion configurations
type OpinionValidator struct {
	strictMode bool
}

// NewOpinionValidator creates a new validator
func NewOpinionValidator() *OpinionValidator {
	return &OpinionValidator{
		strictMode: false,
	}
}

// NewStrictValidator creates a validator with strict mode enabled
func NewStrictValidator() *OpinionValidator {
	return &OpinionValidator{
		strictMode: true,
	}
}

// Validate checks if an OpinionConfig is valid
func (v *OpinionValidator) Validate(config *OpinionConfig) error {
	result := v.ValidateWithDetails(config)
	
	if !result.Valid {
		// Return first error
		if len(result.Errors) > 0 {
			return fmt.Errorf("%s: %v", result.Errors[0].Field, result.Errors[0].Message)
		}
	}
	
	return nil
}

// ValidateWithDetails provides detailed validation results
func (v *OpinionValidator) ValidateWithDetails(config *OpinionConfig) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
	}

	// Validate thresholds
	v.validateThresholds(config, result)

	// Validate correlation windows
	v.validateCorrelationWindows(config, result)

	// Validate behavioral config
	v.validateBehavioralConfig(config, result)

	// Validate prediction config
	v.validatePredictionConfig(config, result)

	// Validate service limits
	v.validateServiceLimits(config, result)

	// Validate weights
	v.validateWeights(config, result)

	// Validate time-based rules
	v.validateTimeBasedRules(config, result)

	// Validate dependencies
	v.validateDependencies(config, result)

	// Cross-field validation
	v.validateCrossFields(config, result)

	// Set overall validity
	result.Valid = len(result.Errors) == 0

	return result
}

// validateThresholds checks anomaly thresholds
func (v *OpinionValidator) validateThresholds(config *OpinionConfig, result *ValidationResult) {
	for metric, threshold := range config.AnomalyThresholds {
		// Check range
		if threshold < 0 || threshold > 1 {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("anomaly_thresholds.%s", metric),
				Message: "must be between 0 and 1",
				Value:   threshold,
			})
		}

		// Warn about extreme values
		if threshold < 0.5 && v.strictMode {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   fmt.Sprintf("anomaly_thresholds.%s", metric),
				Message: "very low threshold may cause excessive alerts",
				Value:   threshold,
			})
		}
		if threshold > 0.95 {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   fmt.Sprintf("anomaly_thresholds.%s", metric),
				Message: "very high threshold may miss important events",
				Value:   threshold,
			})
		}

		// Validate known metrics
		switch metric {
		case "memory_usage", "cpu_usage", "disk_usage":
			// Valid percentage metrics
		case "error_rate":
			if threshold > 0.5 {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   fmt.Sprintf("anomaly_thresholds.%s", metric),
					Message: "error rate threshold above 50% seems too high",
					Value:   threshold,
				})
			}
		default:
			if v.strictMode {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   fmt.Sprintf("anomaly_thresholds.%s", metric),
					Message: "unrecognized metric name",
					Value:   metric,
				})
			}
		}
	}
}

// validateCorrelationWindows checks time windows
func (v *OpinionValidator) validateCorrelationWindows(config *OpinionConfig, result *ValidationResult) {
	for event, window := range config.CorrelationWindows {
		// Check minimum
		if window < 1*time.Second {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("correlation_windows.%s", event),
				Message: "must be at least 1 second",
				Value:   window,
			})
		}

		// Check maximum
		if window > 1*time.Hour {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   fmt.Sprintf("correlation_windows.%s", event),
				Message: "correlation window over 1 hour may be too long",
				Value:   window,
			})
		}

		// Validate specific windows
		switch event {
		case "oom_restart":
			if window < 10*time.Second {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   fmt.Sprintf("correlation_windows.%s", event),
					Message: "OOM restart window under 10s may miss correlations",
					Value:   window,
				})
			}
		case "cascade_failure":
			if window < 30*time.Second {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   fmt.Sprintf("correlation_windows.%s", event),
					Message: "cascade failure window under 30s may miss propagation",
					Value:   window,
				})
			}
		}
	}
}

// validateBehavioralConfig checks behavioral settings
func (v *OpinionValidator) validateBehavioralConfig(config *OpinionConfig, result *ValidationResult) {
	bc := &config.BehavioralConfig

	// Learning window
	if bc.LearningWindow > 0 {
		if bc.LearningWindow < 1*time.Hour {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "behavioral.learning_window",
				Message: "must be at least 1 hour",
				Value:   bc.LearningWindow,
			})
		}
		if bc.LearningWindow > 30*24*time.Hour {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "behavioral.learning_window",
				Message: "learning window over 30 days may include stale patterns",
				Value:   bc.LearningWindow,
			})
		}
	}

	// Minimum samples
	if bc.MinSamplesRequired > 0 {
		if bc.MinSamplesRequired < 10 {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "behavioral.min_samples_required",
				Message: "less than 10 samples may not be statistically significant",
				Value:   bc.MinSamplesRequired,
			})
		}
		if bc.MinSamplesRequired > 10000 {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "behavioral.min_samples_required",
				Message: "requiring over 10000 samples may delay detection",
				Value:   bc.MinSamplesRequired,
			})
		}
	}

	// Deviation sensitivity
	if bc.DeviationSensitivity > 0 {
		if bc.DeviationSensitivity < 0 || bc.DeviationSensitivity > 1 {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "behavioral.deviation_sensitivity",
				Message: "must be between 0 and 1",
				Value:   bc.DeviationSensitivity,
			})
		}
		if bc.DeviationSensitivity < 0.3 {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "behavioral.deviation_sensitivity",
				Message: "very low sensitivity may miss important changes",
				Value:   bc.DeviationSensitivity,
			})
		}
	}

	// Trend window
	if bc.TrendWindow > 0 {
		if bc.TrendWindow < 1*time.Minute {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "behavioral.trend_window",
				Message: "must be at least 1 minute",
				Value:   bc.TrendWindow,
			})
		}
		if bc.TrendWindow > bc.LearningWindow {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "behavioral.trend_window",
				Message: "cannot be longer than learning window",
				Value:   bc.TrendWindow,
			})
		}
	}
}

// validatePredictionConfig checks prediction settings
func (v *OpinionValidator) validatePredictionConfig(config *OpinionConfig, result *ValidationResult) {
	pc := &config.PredictionConfig

	// Prediction horizon
	if pc.PredictionHorizon > 0 {
		if pc.PredictionHorizon < 30*time.Second {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "prediction.prediction_horizon",
				Message: "prediction horizon under 30s may not provide enough warning",
				Value:   pc.PredictionHorizon,
			})
		}
		if pc.PredictionHorizon > 1*time.Hour {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "prediction.prediction_horizon",
				Message: "prediction horizon over 1 hour may be unreliable",
				Value:   pc.PredictionHorizon,
			})
		}
	}

	// Confidence threshold
	if pc.MinConfidenceThreshold > 0 {
		if pc.MinConfidenceThreshold < 0 || pc.MinConfidenceThreshold > 1 {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "prediction.min_confidence_threshold",
				Message: "must be between 0 and 1",
				Value:   pc.MinConfidenceThreshold,
			})
		}
		if pc.MinConfidenceThreshold < 0.5 {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "prediction.min_confidence_threshold",
				Message: "low confidence threshold may produce many false positives",
				Value:   pc.MinConfidenceThreshold,
			})
		}
		if pc.MinConfidenceThreshold > 0.95 {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   "prediction.min_confidence_threshold",
				Message: "very high confidence threshold may miss valid predictions",
				Value:   pc.MinConfidenceThreshold,
			})
		}
	}

	// Validate prediction windows
	for pred, window := range pc.PredictionWindows {
		if window < 10*time.Second {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("prediction.prediction_windows.%s", pred),
				Message: "must be at least 10 seconds",
				Value:   window,
			})
		}
		if window > pc.PredictionHorizon && pc.PredictionHorizon > 0 {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("prediction.prediction_windows.%s", pred),
				Message: "cannot be longer than prediction horizon",
				Value:   window,
			})
		}
	}
}

// validateServiceLimits checks service-specific limits
func (v *OpinionValidator) validateServiceLimits(config *OpinionConfig, result *ValidationResult) {
	for service, limit := range config.ServiceLimits {
		// Validate memory limit
		if limit.MemoryLimit > 0 {
			if limit.MemoryLimit < 0 || limit.MemoryLimit > 1 {
				result.Errors = append(result.Errors, ValidationError{
					Field:   fmt.Sprintf("service_limits.%s.memory_limit", service),
					Message: "must be between 0 and 1",
					Value:   limit.MemoryLimit,
				})
			}
			
			// Check against general threshold
			if generalThreshold, exists := config.AnomalyThresholds["memory_usage"]; exists {
				if limit.MemoryLimit < generalThreshold {
					result.Warnings = append(result.Warnings, ValidationWarning{
						Field:   fmt.Sprintf("service_limits.%s.memory_limit", service),
						Message: fmt.Sprintf("service limit (%.0f%%) is lower than general threshold (%.0f%%)", limit.MemoryLimit*100, generalThreshold*100),
						Value:   limit.MemoryLimit,
					})
				}
			}
		}

		// Validate CPU limit
		if limit.CPULimit > 0 {
			if limit.CPULimit < 0 || limit.CPULimit > 1 {
				result.Errors = append(result.Errors, ValidationError{
					Field:   fmt.Sprintf("service_limits.%s.cpu_limit", service),
					Message: "must be between 0 and 1",
					Value:   limit.CPULimit,
				})
			}
		}
	}
}

// validateWeights checks importance weights
func (v *OpinionValidator) validateWeights(config *OpinionConfig, result *ValidationResult) {
	for service, weight := range config.ImportanceWeights {
		if weight < 0 || weight > 1 {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("importance_weights.%s", service),
				Message: "must be between 0 and 1",
				Value:   weight,
			})
		}

		// Check service name validity
		if v.strictMode && !isValidServiceName(service) {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   fmt.Sprintf("importance_weights.%s", service),
				Message: "service name contains invalid characters",
				Value:   service,
			})
		}
	}
}

// validateTimeBasedRules checks time-based sensitivity rules
func (v *OpinionValidator) validateTimeBasedRules(config *OpinionConfig, result *ValidationResult) {
	for i, rule := range config.TimeBasedRules {
		// Validate sensitivity
		if rule.Sensitivity < 0 || rule.Sensitivity > 1 {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("time_based_rules[%d].sensitivity", i),
				Message: "must be between 0 and 1",
				Value:   rule.Sensitivity,
			})
		}

		// Validate period
		if rule.Period == "" {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("time_based_rules[%d].period", i),
				Message: "period cannot be empty",
				Value:   rule.Period,
			})
		}

		// Validate time range format
		if rule.TimeRange != "" {
			if !isValidTimeRange(rule.TimeRange) {
				result.Errors = append(result.Errors, ValidationError{
					Field:   fmt.Sprintf("time_based_rules[%d].time_range", i),
					Message: "invalid time range format (expected HH:MM-HH:MM)",
					Value:   rule.TimeRange,
				})
			}
		}

		// Validate weekdays
		for _, day := range rule.Weekdays {
			if !isValidWeekday(day) {
				result.Errors = append(result.Errors, ValidationError{
					Field:   fmt.Sprintf("time_based_rules[%d].weekdays", i),
					Message: fmt.Sprintf("invalid weekday: %s", day),
					Value:   day,
				})
			}
		}
	}
}

// validateDependencies checks service dependencies
func (v *OpinionValidator) validateDependencies(config *OpinionConfig, result *ValidationResult) {
	// Check for cycles
	if cycles := findDependencyCycles(config.ServiceDependencies); len(cycles) > 0 {
		for _, cycle := range cycles {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "service_dependencies",
				Message: fmt.Sprintf("circular dependency detected: %s", strings.Join(cycle, " -> ")),
				Value:   cycle,
			})
		}
	}

	// Validate individual dependencies
	for i, dep := range config.ServiceDependencies {
		if dep.Source == "" {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("service_dependencies[%d].source", i),
				Message: "source cannot be empty",
				Value:   dep.Source,
			})
		}
		if dep.Target == "" {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("service_dependencies[%d].target", i),
				Message: "target cannot be empty",
				Value:   dep.Target,
			})
		}
		if dep.ExpectedDelay < 0 {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("service_dependencies[%d].expected_delay", i),
				Message: "delay cannot be negative",
				Value:   dep.ExpectedDelay,
			})
		}
		if dep.ExpectedDelay > 10*time.Minute {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Field:   fmt.Sprintf("service_dependencies[%d].expected_delay", i),
				Message: "delay over 10 minutes seems unusually long",
				Value:   dep.ExpectedDelay,
			})
		}
		if dep.Confidence > 0 && (dep.Confidence < 0 || dep.Confidence > 1) {
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("service_dependencies[%d].confidence", i),
				Message: "must be between 0 and 1",
				Value:   dep.Confidence,
			})
		}
	}
}

// validateCrossFields checks relationships between different fields
func (v *OpinionValidator) validateCrossFields(config *OpinionConfig, result *ValidationResult) {
	// Check OOM prediction window vs restart window
	if oomWindow, exists := config.PredictionConfig.PredictionWindows["oom"]; exists {
		if restartWindow, exists := config.CorrelationWindows["oom_restart"]; exists {
			if oomWindow < restartWindow {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Field:   "prediction.prediction_windows.oom",
					Message: fmt.Sprintf("OOM prediction window (%s) is shorter than restart correlation window (%s)", oomWindow, restartWindow),
					Value:   oomWindow,
				})
			}
		}
	}

	// Check learning window vs trend window
	if config.BehavioralConfig.LearningWindow > 0 && config.BehavioralConfig.TrendWindow > 0 {
		if config.BehavioralConfig.TrendWindow > config.BehavioralConfig.LearningWindow {
			result.Errors = append(result.Errors, ValidationError{
				Field:   "behavioral.trend_window",
				Message: "cannot be longer than learning window",
				Value:   config.BehavioralConfig.TrendWindow,
			})
		}
	}
}

// Helper functions

func isValidServiceName(name string) bool {
	// Service names should be lowercase with hyphens
	for _, r := range name {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func isValidTimeRange(timeRange string) bool {
	// Expected format: HH:MM-HH:MM
	parts := strings.Split(timeRange, "-")
	if len(parts) != 2 {
		return false
	}
	
	for _, part := range parts {
		timeParts := strings.Split(part, ":")
		if len(timeParts) != 2 {
			return false
		}
		// Simple validation - could be more thorough
		if len(timeParts[0]) != 2 || len(timeParts[1]) != 2 {
			return false
		}
	}
	
	return true
}

func isValidWeekday(day string) bool {
	validDays := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun",
		"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
	
	for _, valid := range validDays {
		if strings.EqualFold(day, valid) {
			return true
		}
	}
	return false
}

func findDependencyCycles(deps []ServiceDependency) [][]string {
	// Build adjacency list
	graph := make(map[string][]string)
	for _, dep := range deps {
		graph[dep.Source] = append(graph[dep.Source], dep.Target)
	}
	
	// Find cycles using DFS
	var cycles [][]string
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	path := []string{}
	
	var dfs func(node string) bool
	dfs = func(node string) bool {
		visited[node] = true
		recStack[node] = true
		path = append(path, node)
		
		for _, neighbor := range graph[node] {
			if !visited[neighbor] {
				if dfs(neighbor) {
					return true
				}
			} else if recStack[neighbor] {
				// Found cycle
				cycleStart := 0
				for i, n := range path {
					if n == neighbor {
						cycleStart = i
						break
					}
				}
				cycle := append([]string{}, path[cycleStart:]...)
				cycle = append(cycle, neighbor)
				cycles = append(cycles, cycle)
				return true
			}
		}
		
		path = path[:len(path)-1]
		recStack[node] = false
		return false
	}
	
	// Check all nodes
	for node := range graph {
		if !visited[node] {
			dfs(node)
		}
	}
	
	return cycles
}