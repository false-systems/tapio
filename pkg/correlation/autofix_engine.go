package correlation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/types"
	"github.com/yairfalse/tapio/pkg/events/opinionated"
)

// AutoFixEngine provides automated remediation capabilities for detected patterns
type AutoFixEngine struct {
	// Core components
	patternRegistry types.PatternRegistry
	actionExecutor  ActionExecutor
	safetyValidator SafetyValidator
	rollbackManager *RollbackManager

	// Configuration
	config *AutoFixConfig

	// Action registry
	actionRegistry map[string]AutoFixAction
	actionMutex    sync.RWMutex

	// Execution tracking
	executionHistory []*AutoFixExecution
	historyMutex     sync.RWMutex

	// Safety controls
	circuitBreaker *CircuitBreaker
	rateLimiter    *RateLimiter

	// State management
	running       bool
	stopChan      chan struct{}
	executionChan chan *AutoFixRequest
}

// AutoFixConfig configures the auto-fix engine
type AutoFixConfig struct {
	// Safety settings
	EnableAutoFix        bool `json:"enable_auto_fix"`
	RequireHumanApproval bool `json:"require_human_approval"`
	MaxActionsPerHour    int  `json:"max_actions_per_hour"`
	MaxActionsPerPattern int  `json:"max_actions_per_pattern"`

	// Confidence thresholds
	MinPatternConfidence float64 `json:"min_pattern_confidence"`
	MinActionConfidence  float64 `json:"min_action_confidence"`
	SafetyScoreThreshold float64 `json:"safety_score_threshold"`

	// Execution settings
	ActionTimeout   time.Duration `json:"action_timeout"`
	DryRunEnabled   bool          `json:"dry_run_enabled"`
	RollbackEnabled bool          `json:"rollback_enabled"`
	RollbackTimeout time.Duration `json:"rollback_timeout"`

	// Pattern-specific settings
	EnableMemoryLeakFixes bool `json:"enable_memory_leak_fixes"`
	EnableNetworkFixes    bool `json:"enable_network_fixes"`
	EnableStorageFixes    bool `json:"enable_storage_fixes"`
	EnableRuntimeFixes    bool `json:"enable_runtime_fixes"`
	EnableDependencyFixes bool `json:"enable_dependency_fixes"`

	// Enterprise settings
	AuditLogging        bool   `json:"audit_logging"`
	ComplianceMode      string `json:"compliance_mode"` // "strict", "moderate", "permissive"
	NotificationWebhook string `json:"notification_webhook"`

	// Circuit breaker settings
	CircuitBreakerThreshold int           `json:"circuit_breaker_threshold"`
	CircuitBreakerWindow    time.Duration `json:"circuit_breaker_window"`
	CircuitBreakerRecovery  time.Duration `json:"circuit_breaker_recovery"`
}

// AutoFixAction defines an automated fix action
type AutoFixAction struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	PatternTypes []string `json:"pattern_types"`

	// Safety classification
	SafetyLevel      string `json:"safety_level"` // "safe", "moderate", "risky"
	RequiresApproval bool   `json:"requires_approval"`
	RequiresDryRun   bool   `json:"requires_dry_run"`

	// Execution details
	ActionType      string            `json:"action_type"` // "kubectl", "script", "api", "workflow"
	Command         string            `json:"command"`
	Parameters      map[string]string `json:"parameters"`
	PreConditions   []string          `json:"pre_conditions"`
	PostValidations []string          `json:"post_validations"`

	// Rollback information
	RollbackSupported  bool              `json:"rollback_supported"`
	RollbackCommand    string            `json:"rollback_command"`
	RollbackParameters map[string]string `json:"rollback_parameters"`

	// Success criteria
	SuccessIndicators []string      `json:"success_indicators"`
	FailureIndicators []string      `json:"failure_indicators"`
	ValidationTimeout time.Duration `json:"validation_timeout"`

	// Impact assessment
	ExpectedImpact    string   `json:"expected_impact"`
	RiskLevel         string   `json:"risk_level"` // "low", "medium", "high"
	AffectedResources []string `json:"affected_resources"`

	// Execution function
	Execute func(ctx context.Context, request *AutoFixRequest) (*AutoFixResult, error) `json:"-"`
}

// AutoFixRequest represents a request to execute an auto-fix action
type AutoFixRequest struct {
	ID            string            `json:"id"`
	PatternResult *PatternResult    `json:"pattern_result"`
	ActionID      string            `json:"action_id"`
	Parameters    map[string]string `json:"parameters"`

	// Request metadata
	RequestedBy      string `json:"requested_by"` // "system", "human", "scheduler"
	Priority         string `json:"priority"`     // "low", "medium", "high", "critical"
	DryRun           bool   `json:"dry_run"`
	RequiresApproval bool   `json:"requires_approval"`

	// Context
	AffectedEntities []Entity  `json:"affected_entities"`
	Timestamp        time.Time `json:"timestamp"`
	Deadline         time.Time `json:"deadline"`

	// Approval tracking
	ApprovalStatus string    `json:"approval_status"` // "pending", "approved", "rejected"
	ApprovedBy     string    `json:"approved_by"`
	ApprovalTime   time.Time `json:"approval_time"`
}

// AutoFixResult represents the result of an auto-fix execution
type AutoFixResult struct {
	RequestID string `json:"request_id"`
	ActionID  string `json:"action_id"`
	Status    string `json:"status"` // "success", "failed", "partial", "rolled_back"

	// Execution details
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Duration  time.Duration `json:"duration"`

	// Results
	CommandOutput string `json:"command_output"`
	ExitCode      int    `json:"exit_code"`
	ErrorMessage  string `json:"error_message,omitempty"`

	// Validation results
	PreConditionResults   map[string]bool `json:"pre_condition_results"`
	PostValidationResults map[string]bool `json:"post_validation_results"`
	SuccessValidation     bool            `json:"success_validation"`

	// Impact assessment
	ActualImpact      string   `json:"actual_impact"`
	UnexpectedChanges []string `json:"unexpected_changes"`

	// Rollback information
	RollbackRequired bool            `json:"rollback_required"`
	RollbackExecuted bool            `json:"rollback_executed"`
	RollbackResult   *RollbackResult `json:"rollback_result,omitempty"`

	// Metadata
	ExecutedBy      string  `json:"executed_by"`
	SafetyScore     float64 `json:"safety_score"`
	ConfidenceScore float64 `json:"confidence_score"`
}

// AutoFixExecution tracks the execution of auto-fix actions
type AutoFixExecution struct {
	ID      string          `json:"id"`
	Request *AutoFixRequest `json:"request"`
	Result  *AutoFixResult  `json:"result"`

	// Tracking
	CreatedAt   time.Time `json:"created_at"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`

	// Pattern context
	PatternID         string  `json:"pattern_id"`
	PatternName       string  `json:"pattern_name"`
	PatternConfidence float64 `json:"pattern_confidence"`

	// Audit trail
	AuditEvents []*AuditEvent `json:"audit_events"`
}

// AuditEvent represents an audit event in the auto-fix process
type AuditEvent struct {
	EventType   string            `json:"event_type"`
	Timestamp   time.Time         `json:"timestamp"`
	Description string            `json:"description"`
	Actor       string            `json:"actor"`
	Details     map[string]string `json:"details"`
}

// RollbackManager manages rollback operations
type RollbackManager struct {
	rollbackActions map[string]*RollbackAction
	rollbackHistory []*RollbackResult
	mutex           sync.RWMutex
}

// RollbackAction defines a rollback action
type RollbackAction struct {
	ExecutionID     string            `json:"execution_id"`
	ActionID        string            `json:"action_id"`
	RollbackCommand string            `json:"rollback_command"`
	Parameters      map[string]string `json:"parameters"`
	CreatedAt       time.Time         `json:"created_at"`
	ExpiresAt       time.Time         `json:"expires_at"`
}

// RollbackResult represents the result of a rollback operation
type RollbackResult struct {
	RollbackID   string    `json:"rollback_id"`
	ExecutionID  string    `json:"execution_id"`
	Status       string    `json:"status"` // "success", "failed", "partial"
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	Output       string    `json:"output"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

// CircuitBreaker implements circuit breaker pattern for auto-fix safety
type CircuitBreaker struct {
	threshold   int
	window      time.Duration
	recovery    time.Duration
	failures    []time.Time
	state       string // "closed", "open", "half-open"
	lastFailure time.Time
	mutex       sync.RWMutex
}

// RateLimiter implements rate limiting for auto-fix actions
type RateLimiter struct {
	maxActions int
	window     time.Duration
	actions    []time.Time
	mutex      sync.RWMutex
}

// ActionExecutor interface for executing actions
type ActionExecutor interface {
	Execute(ctx context.Context, action *AutoFixAction, request *AutoFixRequest) (*AutoFixResult, error)
	ValidateAction(action *AutoFixAction, request *AutoFixRequest) error
	SupportsActionType(actionType string) bool
}

// SafetyValidator interface for validating action safety
type SafetyValidator interface {
	ValidateSafety(action *AutoFixAction, request *AutoFixRequest) (*SafetyAssessment, error)
	CalculateSafetyScore(action *AutoFixAction, request *AutoFixRequest) float64
	CheckPreConditions(action *AutoFixAction, request *AutoFixRequest) (map[string]bool, error)
}

// SafetyAssessment represents a safety assessment for an action
type SafetyAssessment struct {
	SafetyScore     float64         `json:"safety_score"`
	RiskLevel       string          `json:"risk_level"`
	SafetyChecks    map[string]bool `json:"safety_checks"`
	Warnings        []string        `json:"warnings"`
	BlockingIssues  []string        `json:"blocking_issues"`
	Recommendations []string        `json:"recommendations"`
}

// NewAutoFixEngine creates a new auto-fix engine
func NewAutoFixEngine(config *AutoFixConfig, executor ActionExecutor, validator SafetyValidator) *AutoFixEngine {
	if config == nil {
		config = DefaultAutoFixConfig()
	}

	engine := &AutoFixEngine{
		config:           config,
		actionExecutor:   executor,
		safetyValidator:  validator,
		actionRegistry:   make(map[string]AutoFixAction),
		executionHistory: []*AutoFixExecution{},
		stopChan:         make(chan struct{}),
		executionChan:    make(chan *AutoFixRequest, 100),
		rollbackManager:  NewRollbackManager(),
		circuitBreaker:   NewCircuitBreaker(config.CircuitBreakerThreshold, config.CircuitBreakerWindow, config.CircuitBreakerRecovery),
		rateLimiter:      NewRateLimiter(config.MaxActionsPerHour, time.Hour),
	}

	// Register default auto-fix actions
	engine.registerDefaultActions()

	return engine
}

// DefaultAutoFixConfig returns default auto-fix configuration
func DefaultAutoFixConfig() *AutoFixConfig {
	return &AutoFixConfig{
		EnableAutoFix:           true,
		RequireHumanApproval:    false,
		MaxActionsPerHour:       10,
		MaxActionsPerPattern:    3,
		MinPatternConfidence:    0.85,
		MinActionConfidence:     0.8,
		SafetyScoreThreshold:    0.7,
		ActionTimeout:           5 * time.Minute,
		DryRunEnabled:           true,
		RollbackEnabled:         true,
		RollbackTimeout:         2 * time.Minute,
		EnableMemoryLeakFixes:   true,
		EnableNetworkFixes:      true,
		EnableStorageFixes:      true,
		EnableRuntimeFixes:      true,
		EnableDependencyFixes:   true,
		AuditLogging:            true,
		ComplianceMode:          "moderate",
		CircuitBreakerThreshold: 5,
		CircuitBreakerWindow:    10 * time.Minute,
		CircuitBreakerRecovery:  30 * time.Minute,
	}
}

// registerDefaultActions registers built-in auto-fix actions
func (afe *AutoFixEngine) registerDefaultActions() {
	// Memory leak fixes
	afe.registerAction(AutoFixAction{
		ID:                "restart_high_memory_pod",
		Name:              "Restart High Memory Pod",
		Description:       "Restart pod with memory leak to free memory",
		PatternTypes:      []string{"memory_leak_oom_cascade"},
		SafetyLevel:       "moderate",
		RequiresApproval:  false,
		RequiresDryRun:    true,
		ActionType:        "kubectl",
		Command:           "kubectl delete pod {{.pod_name}} -n {{.namespace}}",
		RollbackSupported: false,
		SuccessIndicators: []string{"pod_restarted", "memory_usage_decreased"},
		FailureIndicators: []string{"pod_failed", "service_unavailable"},
		ValidationTimeout: 2 * time.Minute,
		ExpectedImpact:    "Temporary service interruption during pod restart",
		RiskLevel:         "medium",
		Execute:           afe.executeKubectlAction,
	})

	afe.registerAction(AutoFixAction{
		ID:                "increase_memory_limit",
		Name:              "Increase Memory Limit",
		Description:       "Increase memory limit for deployment experiencing memory pressure",
		PatternTypes:      []string{"memory_leak_oom_cascade"},
		SafetyLevel:       "safe",
		RequiresApproval:  false,
		RequiresDryRun:    true,
		ActionType:        "kubectl",
		Command:           "kubectl patch deployment {{.deployment_name}} -n {{.namespace}} -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"{{.container_name}}\",\"resources\":{\"limits\":{\"memory\":\"{{.new_memory_limit}}\"}}}]}}}}'",
		RollbackSupported: true,
		RollbackCommand:   "kubectl patch deployment {{.deployment_name}} -n {{.namespace}} -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"{{.container_name}}\",\"resources\":{\"limits\":{\"memory\":\"{{.original_memory_limit}}\"}}}]}}}}'",
		SuccessIndicators: []string{"deployment_updated", "no_oom_events"},
		FailureIndicators: []string{"deployment_failed", "resource_quota_exceeded"},
		ValidationTimeout: 3 * time.Minute,
		ExpectedImpact:    "Increased resource allocation for deployment",
		RiskLevel:         "low",
		Execute:           afe.executeKubectlAction,
	})

	// Network failure fixes
	afe.registerAction(AutoFixAction{
		ID:                "restart_network_pod",
		Name:              "Restart Network-Failed Pod",
		Description:       "Restart pod experiencing network connectivity issues",
		PatternTypes:      []string{"network_failure_cascade"},
		SafetyLevel:       "moderate",
		RequiresApproval:  false,
		RequiresDryRun:    true,
		ActionType:        "kubectl",
		Command:           "kubectl delete pod {{.pod_name}} -n {{.namespace}}",
		RollbackSupported: false,
		SuccessIndicators: []string{"pod_restarted", "network_connectivity_restored"},
		FailureIndicators: []string{"pod_failed", "network_still_failing"},
		ValidationTimeout: 2 * time.Minute,
		ExpectedImpact:    "Temporary service interruption during pod restart",
		RiskLevel:         "medium",
		Execute:           afe.executeKubectlAction,
	})

	afe.registerAction(AutoFixAction{
		ID:                "scale_up_deployment",
		Name:              "Scale Up Deployment",
		Description:       "Scale up deployment to handle increased load during network issues",
		PatternTypes:      []string{"network_failure_cascade", "service_dependency_failure"},
		SafetyLevel:       "safe",
		RequiresApproval:  false,
		RequiresDryRun:    true,
		ActionType:        "kubectl",
		Command:           "kubectl scale deployment {{.deployment_name}} -n {{.namespace}} --replicas={{.new_replica_count}}",
		RollbackSupported: true,
		RollbackCommand:   "kubectl scale deployment {{.deployment_name}} -n {{.namespace}} --replicas={{.original_replica_count}}",
		SuccessIndicators: []string{"deployment_scaled", "load_distributed"},
		FailureIndicators: []string{"scaling_failed", "resource_limits_exceeded"},
		ValidationTimeout: 3 * time.Minute,
		ExpectedImpact:    "Increased resource usage and improved service availability",
		RiskLevel:         "low",
		Execute:           afe.executeKubectlAction,
	})

	// Storage fixes
	afe.registerAction(AutoFixAction{
		ID:                "cleanup_disk_space",
		Name:              "Cleanup Disk Space",
		Description:       "Clean up temporary files and logs to free disk space",
		PatternTypes:      []string{"storage_io_bottleneck"},
		SafetyLevel:       "moderate",
		RequiresApproval:  true,
		RequiresDryRun:    true,
		ActionType:        "script",
		Command:           "kubectl exec {{.pod_name}} -n {{.namespace}} -- sh -c 'find /tmp -type f -mtime +7 -delete; find /var/log -name \"*.log\" -mtime +30 -delete'",
		RollbackSupported: false,
		SuccessIndicators: []string{"disk_space_freed", "io_performance_improved"},
		FailureIndicators: []string{"cleanup_failed", "critical_files_deleted"},
		ValidationTimeout: 2 * time.Minute,
		ExpectedImpact:    "Free disk space by removing old temporary files and logs",
		RiskLevel:         "medium",
		Execute:           afe.executeScriptAction,
	})

	// Container runtime fixes
	afe.registerAction(AutoFixAction{
		ID:                "restart_kubelet",
		Name:              "Restart Kubelet",
		Description:       "Restart kubelet service on node experiencing runtime issues",
		PatternTypes:      []string{"container_runtime_failure"},
		SafetyLevel:       "risky",
		RequiresApproval:  true,
		RequiresDryRun:    false,
		ActionType:        "script",
		Command:           "systemctl restart kubelet",
		RollbackSupported: false,
		SuccessIndicators: []string{"kubelet_restarted", "pod_creation_successful"},
		FailureIndicators: []string{"kubelet_failed", "node_not_ready"},
		ValidationTimeout: 5 * time.Minute,
		ExpectedImpact:    "Node temporary unavailability during kubelet restart",
		RiskLevel:         "high",
		Execute:           afe.executeScriptAction,
	})
}

// ProcessPatternResult processes a pattern result and determines if auto-fix should be triggered
func (afe *AutoFixEngine) ProcessPatternResult(ctx context.Context, result *types.PatternResult) (*AutoFixExecution, error) {
	if !afe.config.EnableAutoFix || !result.Detected {
		return nil, nil
	}

	// Check pattern confidence
	if result.Confidence < afe.config.MinPatternConfidence {
		return nil, fmt.Errorf("pattern confidence %.2f below threshold %.2f", result.Confidence, afe.config.MinPatternConfidence)
	}

	// Check circuit breaker
	if !afe.circuitBreaker.AllowRequest() {
		return nil, fmt.Errorf("circuit breaker is open, rejecting auto-fix request")
	}

	// Check rate limiting
	if !afe.rateLimiter.AllowRequest() {
		return nil, fmt.Errorf("rate limit exceeded, rejecting auto-fix request")
	}

	// Find suitable auto-fix actions
	actions := afe.findActionsForPattern(result)
	if len(actions) == 0 {
		return nil, fmt.Errorf("no auto-fix actions available for pattern %s", result.PatternID)
	}

	// Select best action based on safety and confidence
	selectedAction, err := afe.selectBestAction(actions, result)
	if err != nil {
		return nil, fmt.Errorf("failed to select action: %w", err)
	}

	// Create auto-fix request
	request := &AutoFixRequest{
		ID:               fmt.Sprintf("autofix-%d", time.Now().UnixNano()),
		PatternResult:    result,
		ActionID:         selectedAction.ID,
		Parameters:       afe.extractParametersFromPattern(selectedAction, result),
		RequestedBy:      "system",
		Priority:         afe.determinePriority(result),
		DryRun:           afe.config.DryRunEnabled,
		RequiresApproval: selectedAction.RequiresApproval || afe.config.RequireHumanApproval,
		AffectedEntities: result.AffectedEntities,
		Timestamp:        time.Now(),
		Deadline:         time.Now().Add(10 * time.Minute),
		ApprovalStatus:   "pending",
	}

	// Execute or queue for approval
	if request.RequiresApproval {
		return afe.queueForApproval(request)
	}

	return afe.executeAutoFix(ctx, request)
}

// executeAutoFix executes an auto-fix request
func (afe *AutoFixEngine) executeAutoFix(ctx context.Context, request *AutoFixRequest) (*AutoFixExecution, error) {
	execution := &AutoFixExecution{
		ID:                fmt.Sprintf("exec-%d", time.Now().UnixNano()),
		Request:           request,
		CreatedAt:         time.Now(),
		PatternID:         request.PatternResult.PatternID,
		PatternName:       request.PatternResult.PatternName,
		PatternConfidence: request.PatternResult.Confidence,
		AuditEvents:       []*AuditEvent{},
	}

	// Add audit event
	execution.AuditEvents = append(execution.AuditEvents, &AuditEvent{
		EventType:   "execution_started",
		Timestamp:   time.Now(),
		Description: fmt.Sprintf("Started auto-fix execution for pattern %s", request.PatternResult.PatternID),
		Actor:       "autofix-engine",
		Details: map[string]string{
			"pattern_id": request.PatternResult.PatternID,
			"action_id":  request.ActionID,
			"confidence": fmt.Sprintf("%.2f", request.PatternResult.Confidence),
		},
	})

	// Get action
	action, exists := afe.getAction(request.ActionID)
	if !exists {
		return nil, fmt.Errorf("action %s not found", request.ActionID)
	}

	// Validate safety
	safetyAssessment, err := afe.safetyValidator.ValidateSafety(&action, request)
	if err != nil {
		return nil, fmt.Errorf("safety validation failed: %w", err)
	}

	if safetyAssessment.SafetyScore < afe.config.SafetyScoreThreshold {
		return nil, fmt.Errorf("safety score %.2f below threshold %.2f", safetyAssessment.SafetyScore, afe.config.SafetyScoreThreshold)
	}

	// Execute action with timeout
	execution.StartedAt = time.Now()

	actionCtx, cancel := context.WithTimeout(ctx, afe.config.ActionTimeout)
	defer cancel()

	result, err := afe.actionExecutor.Execute(actionCtx, &action, request)
	if err != nil {
		afe.circuitBreaker.RecordFailure()
		execution.Result = &AutoFixResult{
			RequestID:    request.ID,
			ActionID:     request.ActionID,
			Status:       "failed",
			StartTime:    execution.StartedAt,
			EndTime:      time.Now(),
			Duration:     time.Since(execution.StartedAt),
			ErrorMessage: err.Error(),
			SafetyScore:  safetyAssessment.SafetyScore,
		}
	} else {
		afe.circuitBreaker.RecordSuccess()
		execution.Result = result
		execution.Result.SafetyScore = safetyAssessment.SafetyScore
	}

	execution.CompletedAt = time.Now()

	// Handle rollback if needed
	if execution.Result.Status == "failed" && action.RollbackSupported && afe.config.RollbackEnabled {
		afe.handleRollback(ctx, execution, &action)
	}

	// Store execution in history
	afe.historyMutex.Lock()
	afe.executionHistory = append(afe.executionHistory, execution)
	afe.historyMutex.Unlock()

	// Add final audit event
	execution.AuditEvents = append(execution.AuditEvents, &AuditEvent{
		EventType:   "execution_completed",
		Timestamp:   time.Now(),
		Description: fmt.Sprintf("Completed auto-fix execution with status: %s", execution.Result.Status),
		Actor:       "autofix-engine",
		Details: map[string]string{
			"status":       execution.Result.Status,
			"duration":     execution.Result.Duration.String(),
			"safety_score": fmt.Sprintf("%.2f", execution.Result.SafetyScore),
		},
	})

	return execution, nil
}

// findActionsForPattern finds suitable actions for a pattern
func (afe *AutoFixEngine) findActionsForPattern(result *types.PatternResult) []AutoFixAction {
	afe.actionMutex.RLock()
	defer afe.actionMutex.RUnlock()

	var actions []AutoFixAction

	for _, action := range afe.actionRegistry {
		for _, patternType := range action.PatternTypes {
			if patternType == result.PatternID {
				// Check if action is enabled for this pattern type
				if afe.isActionEnabledForPattern(action, result.PatternID) {
					actions = append(actions, action)
				}
				break
			}
		}
	}

	return actions
}

// selectBestAction selects the best action based on safety and confidence
func (afe *AutoFixEngine) selectBestAction(actions []AutoFixAction, result *types.PatternResult) (AutoFixAction, error) {
	if len(actions) == 0 {
		return AutoFixAction{}, fmt.Errorf("no actions available")
	}

	// Score actions based on safety, confidence, and effectiveness
	type actionScore struct {
		action AutoFixAction
		score  float64
	}

	var scoredActions []actionScore

	for _, action := range actions {
		score := afe.calculateActionScore(action, result)
		scoredActions = append(scoredActions, actionScore{action, score})
	}

	// Sort by score (highest first)
	for i := 0; i < len(scoredActions)-1; i++ {
		for j := i + 1; j < len(scoredActions); j++ {
			if scoredActions[j].score > scoredActions[i].score {
				scoredActions[i], scoredActions[j] = scoredActions[j], scoredActions[i]
			}
		}
	}

	return scoredActions[0].action, nil
}

// calculateActionScore calculates a score for an action
func (afe *AutoFixEngine) calculateActionScore(action AutoFixAction, result *types.PatternResult) float64 {
	score := 0.0

	// Safety component (higher is better)
	switch action.SafetyLevel {
	case "safe":
		score += 0.4
	case "moderate":
		score += 0.3
	case "risky":
		score += 0.1
	}

	// Risk component (lower risk is better)
	switch action.RiskLevel {
	case "low":
		score += 0.3
	case "medium":
		score += 0.2
	case "high":
		score += 0.1
	}

	// Pattern confidence component
	score += result.Confidence * 0.2

	// Rollback support component
	if action.RollbackSupported {
		score += 0.1
	}

	return score
}

// extractParametersFromPattern extracts action parameters from pattern result
func (afe *AutoFixEngine) extractParametersFromPattern(action AutoFixAction, result *types.PatternResult) map[string]string {
	params := make(map[string]string)

	// Extract entity information
	if len(result.AffectedEntities) > 0 {
		entity := result.AffectedEntities[0]
		params["pod_name"] = entity.Name
		params["namespace"] = entity.Namespace
		params["deployment_name"] = entity.Name
		params["container_name"] = entity.Container
	}

	// Extract pattern-specific parameters
	switch result.PatternID {
	case "memory_leak_oom_cascade":
		// Calculate new memory limit based on current usage and growth pattern
		if result.Metrics.MemoryPressure > 0 {
			currentLimit := result.Metrics.MemoryPressure * 1000 // Simplified calculation
			newLimit := currentLimit * 1.5                       // Increase by 50%
			params["new_memory_limit"] = fmt.Sprintf("%.0fMi", newLimit)
			params["original_memory_limit"] = fmt.Sprintf("%.0fMi", currentLimit)
		}

	case "network_failure_cascade", "service_dependency_failure":
		// Calculate new replica count for scaling
		if result.Impact.AffectedServices > 0 {
			currentReplicas := 3 // Default assumption
			newReplicas := currentReplicas + result.Impact.AffectedServices
			params["new_replica_count"] = fmt.Sprintf("%d", newReplicas)
			params["original_replica_count"] = fmt.Sprintf("%d", currentReplicas)
		}
	}

	return params
}

// determinePriority determines the priority of an auto-fix request
func (afe *AutoFixEngine) determinePriority(result *types.PatternResult) string {
	switch result.Severity {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium":
		return "medium"
	default:
		return "low"
	}
}

// isActionEnabledForPattern checks if an action is enabled for a specific pattern
func (afe *AutoFixEngine) isActionEnabledForPattern(action AutoFixAction, patternID string) bool {
	switch patternID {
	case "memory_leak_oom_cascade":
		return afe.config.EnableMemoryLeakFixes
	case "network_failure_cascade":
		return afe.config.EnableNetworkFixes
	case "storage_io_bottleneck":
		return afe.config.EnableStorageFixes
	case "container_runtime_failure":
		return afe.config.EnableRuntimeFixes
	case "service_dependency_failure":
		return afe.config.EnableDependencyFixes
	default:
		return true
	}
}

// Action execution methods
func (afe *AutoFixEngine) executeKubectlAction(ctx context.Context, request *AutoFixRequest) (*AutoFixResult, error) {
	// This would execute kubectl commands
	// Placeholder implementation
	return &AutoFixResult{
		RequestID:         request.ID,
		ActionID:          request.ActionID,
		Status:            "success",
		StartTime:         time.Now(),
		EndTime:           time.Now().Add(1 * time.Second),
		Duration:          1 * time.Second,
		CommandOutput:     "Action executed successfully",
		ExitCode:          0,
		SuccessValidation: true,
		ExecutedBy:        "autofix-engine",
		ConfidenceScore:   0.9,
	}, nil
}

func (afe *AutoFixEngine) executeScriptAction(ctx context.Context, request *AutoFixRequest) (*AutoFixResult, error) {
	// This would execute shell scripts
	// Placeholder implementation
	return &AutoFixResult{
		RequestID:         request.ID,
		ActionID:          request.ActionID,
		Status:            "success",
		StartTime:         time.Now(),
		EndTime:           time.Now().Add(2 * time.Second),
		Duration:          2 * time.Second,
		CommandOutput:     "Script executed successfully",
		ExitCode:          0,
		SuccessValidation: true,
		ExecutedBy:        "autofix-engine",
		ConfidenceScore:   0.85,
	}, nil
}

// Helper methods and infrastructure

func (afe *AutoFixEngine) registerAction(action AutoFixAction) {
	afe.actionMutex.Lock()
	defer afe.actionMutex.Unlock()
	afe.actionRegistry[action.ID] = action
}

func (afe *AutoFixEngine) getAction(actionID string) (AutoFixAction, bool) {
	afe.actionMutex.RLock()
	defer afe.actionMutex.RUnlock()
	action, exists := afe.actionRegistry[actionID]
	return action, exists
}

func (afe *AutoFixEngine) queueForApproval(request *AutoFixRequest) (*AutoFixExecution, error) {
	// Implementation for human approval workflow
	return nil, fmt.Errorf("approval workflow not implemented")
}

func (afe *AutoFixEngine) handleRollback(ctx context.Context, execution *AutoFixExecution, action *AutoFixAction) {
	// Implementation for rollback handling
}

// Circuit breaker implementation
func NewCircuitBreaker(threshold int, window, recovery time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		window:    window,
		recovery:  recovery,
		failures:  []time.Time{},
		state:     "closed",
	}
}

func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()

	// Clean old failures
	cutoff := now.Add(-cb.window)
	var validFailures []time.Time
	for _, failure := range cb.failures {
		if failure.After(cutoff) {
			validFailures = append(validFailures, failure)
		}
	}
	cb.failures = validFailures

	switch cb.state {
	case "closed":
		return len(cb.failures) < cb.threshold
	case "open":
		if now.Sub(cb.lastFailure) > cb.recovery {
			cb.state = "half-open"
			return true
		}
		return false
	case "half-open":
		return true
	default:
		return false
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if cb.state == "half-open" {
		cb.state = "closed"
		cb.failures = []time.Time{}
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	cb.failures = append(cb.failures, now)
	cb.lastFailure = now

	if len(cb.failures) >= cb.threshold {
		cb.state = "open"
	}
}

// Rate limiter implementation
func NewRateLimiter(maxActions int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		maxActions: maxActions,
		window:     window,
		actions:    []time.Time{},
	}
}

func (rl *RateLimiter) AllowRequest() bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Clean old actions
	var validActions []time.Time
	for _, action := range rl.actions {
		if action.After(cutoff) {
			validActions = append(validActions, action)
		}
	}
	rl.actions = validActions

	if len(rl.actions) < rl.maxActions {
		rl.actions = append(rl.actions, now)
		return true
	}

	return false
}

// Rollback manager implementation
func NewRollbackManager() *RollbackManager {
	return &RollbackManager{
		rollbackActions: make(map[string]*RollbackAction),
		rollbackHistory: []*RollbackResult{},
	}
}

// GetExecutionHistory returns the execution history
func (afe *AutoFixEngine) GetExecutionHistory() []*AutoFixExecution {
	afe.historyMutex.RLock()
	defer afe.historyMutex.RUnlock()

	// Return a copy
	history := make([]*AutoFixExecution, len(afe.executionHistory))
	copy(history, afe.executionHistory)
	return history
}

// GetActionRegistry returns all registered actions
func (afe *AutoFixEngine) GetActionRegistry() map[string]AutoFixAction {
	afe.actionMutex.RLock()
	defer afe.actionMutex.RUnlock()

	// Return a copy
	registry := make(map[string]AutoFixAction)
	for k, v := range afe.actionRegistry {
		registry[k] = v
	}
	return registry
}
