package resilience

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// EmergencyLevel represents the severity of an emergency situation
type EmergencyLevel int32

const (
	EmergencyNone EmergencyLevel = iota
	EmergencyLow
	EmergencyMedium
	EmergencyHigh
	EmergencyCritical
)

func (e EmergencyLevel) String() string {
	switch e {
	case EmergencyNone:
		return "none"
	case EmergencyLow:
		return "low"
	case EmergencyMedium:
		return "medium"
	case EmergencyHigh:
		return "high"
	case EmergencyCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// EmergencyCondition represents a condition that can trigger emergency protocols
type EmergencyCondition int

const (
	ConditionCPUOverload EmergencyCondition = iota
	ConditionMemoryPressure
	ConditionDiskFull
	ConditionNetworkCongestion
	ConditionHighErrorRate
	ConditionResponseTimeSpike
	ConditionThroughputDrop
	ConditionCircuitBreakerOpen
	ConditionHealthCheckFailure
	ConditionExternalDependencyFailure
)

func (c EmergencyCondition) String() string {
	switch c {
	case ConditionCPUOverload:
		return "cpu_overload"
	case ConditionMemoryPressure:
		return "memory_pressure"
	case ConditionDiskFull:
		return "disk_full"
	case ConditionNetworkCongestion:
		return "network_congestion"
	case ConditionHighErrorRate:
		return "high_error_rate"
	case ConditionResponseTimeSpike:
		return "response_time_spike"
	case ConditionThroughputDrop:
		return "throughput_drop"
	case ConditionCircuitBreakerOpen:
		return "circuit_breaker_open"
	case ConditionHealthCheckFailure:
		return "health_check_failure"
	case ConditionExternalDependencyFailure:
		return "external_dependency_failure"
	default:
		return "unknown"
	}
}

// EmergencyProtocolManager handles emergency situations and automatic responses
type EmergencyProtocolManager struct {
	config                *EmergencyConfig
	
	// Current emergency state
	currentLevel          int32          // atomic: EmergencyLevel
	activeEmergencies     map[EmergencyCondition]*ActiveEmergency
	emergencyMutex        sync.RWMutex
	
	// Protocol registry
	protocols             map[EmergencyCondition][]*EmergencyProtocol
	protocolMutex         sync.RWMutex
	
	// Monitoring and detection
	monitors              map[EmergencyCondition]*EmergencyMonitor
	monitorsMutex         sync.RWMutex
	
	// Emergency response execution
	executionQueue        chan *EmergencyExecution
	executorWorkers       []*EmergencyExecutor
	
	// Integration points
	healthChecker         *HealthChecker
	degradationManager    *DegradationManager
	circuitBreakers       map[string]*CircuitBreaker
	
	// Metrics and logging
	metrics               *EmergencyMetrics
	
	// State management
	running               bool
	stopChan              chan struct{}
}

// EmergencyConfig configures emergency protocol behavior
type EmergencyConfig struct {
	// Detection settings
	MonitoringInterval       time.Duration               `json:"monitoring_interval"`
	DetectionSensitivity     float64                     `json:"detection_sensitivity"`     // 0.0-1.0
	
	// Thresholds for emergency conditions
	CPUThreshold            float64                     `json:"cpu_threshold"`             // 0.0-1.0
	MemoryThreshold         float64                     `json:"memory_threshold"`          // 0.0-1.0
	DiskThreshold           float64                     `json:"disk_threshold"`            // 0.0-1.0
	ErrorRateThreshold      float64                     `json:"error_rate_threshold"`      // 0.0-1.0
	ResponseTimeThreshold   time.Duration               `json:"response_time_threshold"`
	ThroughputDropThreshold float64                     `json:"throughput_drop_threshold"` // 0.0-1.0
	
	// Emergency response settings
	EnableAutomaticResponse bool                        `json:"enable_automatic_response"`
	MaxConcurrentResponses  int                         `json:"max_concurrent_responses"`
	ResponseTimeout         time.Duration               `json:"response_timeout"`
	CooldownPeriod          time.Duration               `json:"cooldown_period"`
	
	// Load shedding configuration
	LoadSheddingEnabled     bool                        `json:"load_shedding_enabled"`
	LoadSheddingThresholds  map[string]float64          `json:"load_shedding_thresholds"`
	
	// Emergency contacts and notifications
	NotificationChannels    []string                    `json:"notification_channels"`
	EscalationThresholds    map[EmergencyLevel]time.Duration `json:"escalation_thresholds"`
	
	// Safety settings
	RequireApproval         []EmergencyCondition        `json:"require_approval"`
	DryRunMode              bool                        `json:"dry_run_mode"`
	SafetyLimits            map[string]interface{}      `json:"safety_limits"`
}

// EmergencyProtocol defines a response protocol for emergency conditions
type EmergencyProtocol struct {
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	Condition          EmergencyCondition `json:"condition"`
	TriggerLevel       EmergencyLevel     `json:"trigger_level"`
	
	// Execution settings
	Priority           int                `json:"priority"`           // 1-10, 1 = highest
	RequiresApproval   bool               `json:"requires_approval"`
	MaxRetries         int                `json:"max_retries"`
	RetryDelay         time.Duration      `json:"retry_delay"`
	Timeout            time.Duration      `json:"timeout"`
	
	// Actions to perform
	Actions            []EmergencyAction  `json:"actions"`
	RollbackActions    []EmergencyAction  `json:"rollback_actions"`
	
	// Validation and safety
	PreConditions      []string           `json:"pre_conditions"`
	PostValidations    []string           `json:"post_validations"`
	SafetyChecks       []string           `json:"safety_checks"`
	
	// Impact assessment
	ExpectedImpact     string             `json:"expected_impact"`
	RiskLevel          string             `json:"risk_level"`        // "low", "medium", "high"
	
	// Metadata
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	Version            int                `json:"version"`
}

// EmergencyAction defines a specific action to take during an emergency
type EmergencyAction struct {
	Type         string                 `json:"type"`          // "load_shedding", "scale_down", "circuit_breaker", etc.
	Parameters   map[string]interface{} `json:"parameters"`
	ExecuteFunc  func(context.Context, map[string]interface{}) error `json:"-"`
	ValidateFunc func(map[string]interface{}) error                   `json:"-"`
}

// ActiveEmergency represents an ongoing emergency situation
type ActiveEmergency struct {
	Condition      EmergencyCondition  `json:"condition"`
	Level          EmergencyLevel      `json:"level"`
	StartTime      time.Time           `json:"start_time"`
	LastUpdate     time.Time           `json:"last_update"`
	TriggerValue   float64             `json:"trigger_value"`
	CurrentValue   float64             `json:"current_value"`
	
	// Response tracking
	ResponsesTriggered []string         `json:"responses_triggered"`
	LastResponse       time.Time        `json:"last_response"`
	ResponseCount      int              `json:"response_count"`
	
	// Status
	Status         string              `json:"status"`        // "detected", "responding", "recovering", "resolved"
	Escalated      bool                `json:"escalated"`
	
	// Context
	Context        map[string]interface{} `json:"context"`
	Metadata       map[string]string      `json:"metadata"`
}

// EmergencyMonitor monitors specific conditions and triggers emergency protocols
type EmergencyMonitor struct {
	Condition         EmergencyCondition `json:"condition"`
	MonitorFunc       func() float64     `json:"-"`
	Threshold         float64            `json:"threshold"`
	CurrentValue      float64            `json:"current_value"`
	LastCheck         time.Time          `json:"last_check"`
	
	// Trend analysis
	ValueHistory      []float64          `json:"-"`
	TrendDirection    string             `json:"trend_direction"`  // "rising", "falling", "stable"
	
	// Alert settings
	Enabled           bool               `json:"enabled"`
	CooldownPeriod    time.Duration      `json:"cooldown_period"`
	LastAlert         time.Time          `json:"last_alert"`
}

// EmergencyExecution represents an emergency protocol execution
type EmergencyExecution struct {
	ID               string             `json:"id"`
	ProtocolID       string             `json:"protocol_id"`
	Condition        EmergencyCondition `json:"condition"`
	Level            EmergencyLevel     `json:"level"`
	
	// Execution details
	StartTime        time.Time          `json:"start_time"`
	EndTime          time.Time          `json:"end_time"`
	Duration         time.Duration      `json:"duration"`
	Status           string             `json:"status"`        // "pending", "executing", "success", "failed", "timeout"
	
	// Results
	ActionsExecuted  []string           `json:"actions_executed"`
	ActionsSuccess   []string           `json:"actions_success"`
	ActionsFailed    []string           `json:"actions_failed"`
	ErrorMessage     string             `json:"error_message,omitempty"`
	
	// Impact
	ImpactAssessment string             `json:"impact_assessment"`
	SideEffects      []string           `json:"side_effects"`
	
	// Context
	TriggerValue     float64            `json:"trigger_value"`
	Context          map[string]interface{} `json:"context"`
}

// EmergencyExecutor executes emergency protocols
type EmergencyExecutor struct {
	ID            string
	Manager       *EmergencyProtocolManager
	Running       bool
	CurrentTask   *EmergencyExecution
	TaskQueue     chan *EmergencyExecution
	StopChan      chan struct{}
}

// EmergencyMetrics tracks emergency protocol performance
type EmergencyMetrics struct {
	// Detection metrics
	EmergenciesDetected      uint64                              `json:"emergencies_detected"`
	FalsePositives          uint64                              `json:"false_positives"`
	DetectionTime           map[EmergencyCondition]time.Duration `json:"detection_time"`
	
	// Response metrics
	ProtocolsExecuted       uint64                              `json:"protocols_executed"`
	SuccessfulResponses     uint64                              `json:"successful_responses"`
	FailedResponses         uint64                              `json:"failed_responses"`
	AverageResponseTime     time.Duration                       `json:"average_response_time"`
	
	// Impact metrics
	SystemStabilized        uint64                              `json:"system_stabilized"`
	EmergenciesEscalated    uint64                              `json:"emergencies_escalated"`
	AverageRecoveryTime     time.Duration                       `json:"average_recovery_time"`
	
	// Condition-specific metrics
	ConditionFrequency      map[EmergencyCondition]uint64       `json:"condition_frequency"`
	ConditionSuccessRate    map[EmergencyCondition]float64      `json:"condition_success_rate"`
	
	LastUpdated             time.Time                           `json:"last_updated"`
}

// NewEmergencyProtocolManager creates a new emergency protocol manager
func NewEmergencyProtocolManager(config *EmergencyConfig) *EmergencyProtocolManager {
	if config == nil {
		config = DefaultEmergencyConfig()
	}
	
	epm := &EmergencyProtocolManager{
		config:              config,
		activeEmergencies:   make(map[EmergencyCondition]*ActiveEmergency),
		protocols:           make(map[EmergencyCondition][]*EmergencyProtocol),
		monitors:            make(map[EmergencyCondition]*EmergencyMonitor),
		executionQueue:      make(chan *EmergencyExecution, 100),
		executorWorkers:     make([]*EmergencyExecutor, config.MaxConcurrentResponses),
		circuitBreakers:     make(map[string]*CircuitBreaker),
		metrics:             &EmergencyMetrics{
			DetectionTime:       make(map[EmergencyCondition]time.Duration),
			ConditionFrequency:  make(map[EmergencyCondition]uint64),
			ConditionSuccessRate: make(map[EmergencyCondition]float64),
		},
		stopChan:            make(chan struct{}),
	}
	
	// Initialize executor workers
	for i := 0; i < config.MaxConcurrentResponses; i++ {
		epm.executorWorkers[i] = &EmergencyExecutor{
			ID:        fmt.Sprintf("executor-%d", i),
			Manager:   epm,
			TaskQueue: make(chan *EmergencyExecution, 10),
			StopChan:  make(chan struct{}),
		}
	}
	
	// Register default protocols and monitors
	epm.registerDefaultProtocols()
	epm.registerDefaultMonitors()
	
	return epm
}

// DefaultEmergencyConfig returns default emergency configuration
func DefaultEmergencyConfig() *EmergencyConfig {
	return &EmergencyConfig{
		MonitoringInterval:       1 * time.Second,
		DetectionSensitivity:     0.8,
		CPUThreshold:            0.9,
		MemoryThreshold:         0.85,
		DiskThreshold:           0.95,
		ErrorRateThreshold:      0.1,
		ResponseTimeThreshold:   5 * time.Second,
		ThroughputDropThreshold: 0.5,
		EnableAutomaticResponse: true,
		MaxConcurrentResponses:  3,
		ResponseTimeout:         30 * time.Second,
		CooldownPeriod:          5 * time.Minute,
		LoadSheddingEnabled:     true,
		LoadSheddingThresholds: map[string]float64{
			"low_priority":    0.7,
			"medium_priority": 0.8,
			"high_priority":   0.9,
		},
		NotificationChannels: []string{"log", "webhook"},
		EscalationThresholds: map[EmergencyLevel]time.Duration{
			EmergencyLow:      10 * time.Minute,
			EmergencyMedium:   5 * time.Minute,
			EmergencyHigh:     2 * time.Minute,
			EmergencyCritical: 30 * time.Second,
		},
		RequireApproval: []EmergencyCondition{},
		DryRunMode:     false,
		SafetyLimits: map[string]interface{}{
			"max_restarts_per_hour": 5,
			"max_scale_down_ratio":  0.5,
		},
	}
}

// Start starts the emergency protocol manager
func (epm *EmergencyProtocolManager) Start(ctx context.Context) error {
	epm.running = true
	
	// Start monitoring loop
	go epm.monitoringLoop(ctx)
	
	// Start executor workers
	for _, worker := range epm.executorWorkers {
		go worker.Start(ctx)
	}
	
	// Start execution dispatcher
	go epm.executionDispatcher(ctx)
	
	return nil
}

// Stop stops the emergency protocol manager
func (epm *EmergencyProtocolManager) Stop() error {
	epm.running = false
	close(epm.stopChan)
	
	// Stop executor workers
	for _, worker := range epm.executorWorkers {
		worker.Stop()
	}
	
	return nil
}

// registerDefaultProtocols registers built-in emergency protocols
func (epm *EmergencyProtocolManager) registerDefaultProtocols() {
	// CPU Overload Protocol
	epm.RegisterProtocol(&EmergencyProtocol{
		ID:               "cpu_overload_load_shedding",
		Name:             "CPU Overload Load Shedding",
		Description:      "Shed low priority load when CPU usage is high",
		Condition:        ConditionCPUOverload,
		TriggerLevel:     EmergencyMedium,
		Priority:         5,
		RequiresApproval: false,
		MaxRetries:       2,
		RetryDelay:       10 * time.Second,
		Timeout:          30 * time.Second,
		Actions: []EmergencyAction{
			{
				Type: "load_shedding",
				Parameters: map[string]interface{}{
					"priority_threshold": "low",
					"shed_percentage":    30,
				},
				ExecuteFunc: epm.executeLoadShedding,
			},
		},
		ExpectedImpact: "Reduced processing of low-priority requests",
		RiskLevel:     "low",
	})
	
	// Memory Pressure Protocol
	epm.RegisterProtocol(&EmergencyProtocol{
		ID:               "memory_pressure_gc_force",
		Name:             "Memory Pressure Garbage Collection",
		Description:      "Force garbage collection and reduce memory allocation",
		Condition:        ConditionMemoryPressure,
		TriggerLevel:     EmergencyMedium,
		Priority:         3,
		RequiresApproval: false,
		MaxRetries:       1,
		RetryDelay:       5 * time.Second,
		Timeout:          10 * time.Second,
		Actions: []EmergencyAction{
			{
				Type: "force_gc",
				Parameters: map[string]interface{}{
					"aggressive": true,
				},
				ExecuteFunc: epm.executeForceGC,
			},
			{
				Type: "reduce_cache",
				Parameters: map[string]interface{}{
					"reduction_percentage": 50,
				},
				ExecuteFunc: epm.executeReduceCache,
			},
		},
		ExpectedImpact: "Temporary performance impact during GC",
		RiskLevel:     "low",
	})
	
	// High Error Rate Protocol
	epm.RegisterProtocol(&EmergencyProtocol{
		ID:               "high_error_rate_circuit_break",
		Name:             "High Error Rate Circuit Breaking",
		Description:      "Open circuit breakers for failing components",
		Condition:        ConditionHighErrorRate,
		TriggerLevel:     EmergencyHigh,
		Priority:         2,
		RequiresApproval: false,
		MaxRetries:       1,
		RetryDelay:       0,
		Timeout:          5 * time.Second,
		Actions: []EmergencyAction{
			{
				Type: "open_circuit_breakers",
				Parameters: map[string]interface{}{
					"error_threshold": 0.5,
				},
				ExecuteFunc: epm.executeOpenCircuitBreakers,
			},
		},
		ExpectedImpact: "Prevent cascade failures by isolating failing components",
		RiskLevel:     "medium",
	})
	
	// Response Time Spike Protocol
	epm.RegisterProtocol(&EmergencyProtocol{
		ID:               "response_time_spike_degradation",
		Name:             "Response Time Spike Graceful Degradation",
		Description:      "Enable graceful degradation to improve response times",
		Condition:        ConditionResponseTimeSpike,
		TriggerLevel:     EmergencyMedium,
		Priority:         4,
		RequiresApproval: false,
		MaxRetries:       2,
		RetryDelay:       15 * time.Second,
		Timeout:          30 * time.Second,
		Actions: []EmergencyAction{
			{
				Type: "enable_degradation",
				Parameters: map[string]interface{}{
					"degradation_level": "minor",
				},
				ExecuteFunc: epm.executeEnableDegradation,
			},
		},
		ExpectedImpact: "Reduced feature availability to improve performance",
		RiskLevel:     "low",
	})
}

// registerDefaultMonitors registers built-in condition monitors
func (epm *EmergencyProtocolManager) registerDefaultMonitors() {
	// CPU Monitor
	epm.RegisterMonitor(&EmergencyMonitor{
		Condition:      ConditionCPUOverload,
		MonitorFunc:    epm.monitorCPUUsage,
		Threshold:      epm.config.CPUThreshold,
		Enabled:        true,
		CooldownPeriod: 30 * time.Second,
		ValueHistory:   make([]float64, 0, 60), // 1 minute of history
	})
	
	// Memory Monitor
	epm.RegisterMonitor(&EmergencyMonitor{
		Condition:      ConditionMemoryPressure,
		MonitorFunc:    epm.monitorMemoryUsage,
		Threshold:      epm.config.MemoryThreshold,
		Enabled:        true,
		CooldownPeriod: 30 * time.Second,
		ValueHistory:   make([]float64, 0, 60),
	})
	
	// Error Rate Monitor
	epm.RegisterMonitor(&EmergencyMonitor{
		Condition:      ConditionHighErrorRate,
		MonitorFunc:    epm.monitorErrorRate,
		Threshold:      epm.config.ErrorRateThreshold,
		Enabled:        true,
		CooldownPeriod: 1 * time.Minute,
		ValueHistory:   make([]float64, 0, 60),
	})
	
	// Response Time Monitor
	epm.RegisterMonitor(&EmergencyMonitor{
		Condition:      ConditionResponseTimeSpike,
		MonitorFunc:    epm.monitorResponseTime,
		Threshold:      float64(epm.config.ResponseTimeThreshold.Milliseconds()),
		Enabled:        true,
		CooldownPeriod: 1 * time.Minute,
		ValueHistory:   make([]float64, 0, 60),
	})
}

// RegisterProtocol registers an emergency protocol
func (epm *EmergencyProtocolManager) RegisterProtocol(protocol *EmergencyProtocol) {
	epm.protocolMutex.Lock()
	defer epm.protocolMutex.Unlock()
	
	protocol.CreatedAt = time.Now()
	protocol.UpdatedAt = time.Now()
	
	if epm.protocols[protocol.Condition] == nil {
		epm.protocols[protocol.Condition] = make([]*EmergencyProtocol, 0)
	}
	
	epm.protocols[protocol.Condition] = append(epm.protocols[protocol.Condition], protocol)
	
	// Sort by priority (lower number = higher priority)
	protocols := epm.protocols[protocol.Condition]
	for i := 0; i < len(protocols)-1; i++ {
		for j := i + 1; j < len(protocols); j++ {
			if protocols[j].Priority < protocols[i].Priority {
				protocols[i], protocols[j] = protocols[j], protocols[i]
			}
		}
	}
}

// RegisterMonitor registers a condition monitor
func (epm *EmergencyProtocolManager) RegisterMonitor(monitor *EmergencyMonitor) {
	epm.monitorsMutex.Lock()
	defer epm.monitorsMutex.Unlock()
	epm.monitors[monitor.Condition] = monitor
}

// monitoringLoop runs the main monitoring loop
func (epm *EmergencyProtocolManager) monitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(epm.config.MonitoringInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-epm.stopChan:
			return
		case <-ticker.C:
			epm.checkAllConditions()
		}
	}
}

// checkAllConditions checks all monitored conditions
func (epm *EmergencyProtocolManager) checkAllConditions() {
	epm.monitorsMutex.RLock()
	monitors := make([]*EmergencyMonitor, 0, len(epm.monitors))
	for _, monitor := range epm.monitors {
		if monitor.Enabled {
			monitors = append(monitors, monitor)
		}
	}
	epm.monitorsMutex.RUnlock()
	
	for _, monitor := range monitors {
		epm.checkCondition(monitor)
	}
}

// checkCondition checks a specific condition
func (epm *EmergencyProtocolManager) checkCondition(monitor *EmergencyMonitor) {
	// Check cooldown period
	if time.Since(monitor.LastAlert) < monitor.CooldownPeriod {
		return
	}
	
	// Get current value
	currentValue := monitor.MonitorFunc()
	monitor.CurrentValue = currentValue
	monitor.LastCheck = time.Now()
	
	// Update value history
	monitor.ValueHistory = append(monitor.ValueHistory, currentValue)
	if len(monitor.ValueHistory) > 60 {
		monitor.ValueHistory = monitor.ValueHistory[1:]
	}
	
	// Analyze trend
	monitor.TrendDirection = epm.analyzeTrend(monitor.ValueHistory)
	
	// Check if threshold is exceeded
	if currentValue > monitor.Threshold {
		epm.triggerEmergency(monitor.Condition, currentValue, monitor.Threshold)
		monitor.LastAlert = time.Now()
	} else {
		epm.resolveEmergency(monitor.Condition)
	}
}

// triggerEmergency triggers emergency protocols for a condition
func (epm *EmergencyProtocolManager) triggerEmergency(condition EmergencyCondition, currentValue, threshold float64) {
	epm.emergencyMutex.Lock()
	defer epm.emergencyMutex.Unlock()
	
	// Check if emergency already active
	if existing, exists := epm.activeEmergencies[condition]; exists {
		// Update existing emergency
		existing.LastUpdate = time.Now()
		existing.CurrentValue = currentValue
		existing.Level = epm.calculateEmergencyLevel(currentValue, threshold)
		return
	}
	
	// Create new emergency
	level := epm.calculateEmergencyLevel(currentValue, threshold)
	emergency := &ActiveEmergency{
		Condition:      condition,
		Level:          level,
		StartTime:      time.Now(),
		LastUpdate:     time.Now(),
		TriggerValue:   threshold,
		CurrentValue:   currentValue,
		Status:         "detected",
		Context:        make(map[string]interface{}),
		Metadata:       make(map[string]string),
	}
	
	epm.activeEmergencies[condition] = emergency
	atomic.AddUint64(&epm.metrics.EmergenciesDetected, 1)
	atomic.AddUint64(&epm.metrics.ConditionFrequency[condition], 1)
	
	// Update current emergency level
	epm.updateOverallEmergencyLevel()
	
	// Trigger appropriate protocols
	if epm.config.EnableAutomaticResponse {
		epm.executeProtocolsForCondition(condition, level)
	}
}

// resolveEmergency resolves an emergency condition
func (epm *EmergencyProtocolManager) resolveEmergency(condition EmergencyCondition) {
	epm.emergencyMutex.Lock()
	defer epm.emergencyMutex.Unlock()
	
	if emergency, exists := epm.activeEmergencies[condition]; exists {
		emergency.Status = "resolved"
		delete(epm.activeEmergencies, condition)
		atomic.AddUint64(&epm.metrics.SystemStabilized, 1)
		
		// Update overall emergency level
		epm.updateOverallEmergencyLevel()
	}
}

// calculateEmergencyLevel calculates emergency level based on severity
func (epm *EmergencyProtocolManager) calculateEmergencyLevel(currentValue, threshold float64) EmergencyLevel {
	ratio := currentValue / threshold
	
	if ratio >= 2.0 {
		return EmergencyCritical
	} else if ratio >= 1.5 {
		return EmergencyHigh
	} else if ratio >= 1.2 {
		return EmergencyMedium
	} else {
		return EmergencyLow
	}
}

// updateOverallEmergencyLevel updates the overall emergency level
func (epm *EmergencyProtocolManager) updateOverallEmergencyLevel() {
	maxLevel := EmergencyNone
	
	for _, emergency := range epm.activeEmergencies {
		if emergency.Level > maxLevel {
			maxLevel = emergency.Level
		}
	}
	
	atomic.StoreInt32(&epm.currentLevel, int32(maxLevel))
}

// executeProtocolsForCondition executes protocols for a specific condition
func (epm *EmergencyProtocolManager) executeProtocolsForCondition(condition EmergencyCondition, level EmergencyLevel) {
	epm.protocolMutex.RLock()
	protocols := epm.protocols[condition]
	epm.protocolMutex.RUnlock()
	
	for _, protocol := range protocols {
		if protocol.TriggerLevel <= level {
			execution := &EmergencyExecution{
				ID:           fmt.Sprintf("exec-%d", time.Now().UnixNano()),
				ProtocolID:   protocol.ID,
				Condition:    condition,
				Level:        level,
				StartTime:    time.Now(),
				Status:       "pending",
				Context:      make(map[string]interface{}),
			}
			
			// Queue for execution
			select {
			case epm.executionQueue <- execution:
			default:
				// Queue full, skip this execution
			}
			
			break // Execute only the highest priority protocol
		}
	}
}

// executionDispatcher dispatches executions to workers
func (epm *EmergencyProtocolManager) executionDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-epm.stopChan:
			return
		case execution := <-epm.executionQueue:
			epm.dispatchToWorker(execution)
		}
	}
}

// dispatchToWorker dispatches execution to available worker
func (epm *EmergencyProtocolManager) dispatchToWorker(execution *EmergencyExecution) {
	// Find available worker
	for _, worker := range epm.executorWorkers {
		if worker.CurrentTask == nil {
			select {
			case worker.TaskQueue <- execution:
				return
			default:
			}
		}
	}
	
	// All workers busy, queue will handle backpressure
}

// Built-in monitoring functions

// monitorCPUUsage monitors CPU usage
func (epm *EmergencyProtocolManager) monitorCPUUsage() float64 {
	// Simplified CPU monitoring using goroutine count as proxy
	goroutines := runtime.NumGoroutine()
	// Normalize to 0.0-1.0 (simplified)
	return float64(goroutines) / 10000.0
}

// monitorMemoryUsage monitors memory usage
func (epm *EmergencyProtocolManager) monitorMemoryUsage() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	// Calculate memory pressure
	if m.Sys == 0 {
		return 0.0
	}
	return float64(m.Alloc) / float64(m.Sys)
}

// monitorErrorRate monitors error rate
func (epm *EmergencyProtocolManager) monitorErrorRate() float64 {
	// Would integrate with actual error tracking
	// For now, return a simulated value
	return 0.05 // 5% error rate
}

// monitorResponseTime monitors response time
func (epm *EmergencyProtocolManager) monitorResponseTime() float64 {
	// Would integrate with actual response time tracking
	// For now, return a simulated value in milliseconds
	return 100.0 // 100ms
}

// analyzeTrend analyzes value trend
func (epm *EmergencyProtocolManager) analyzeTrend(values []float64) string {
	if len(values) < 3 {
		return "stable"
	}
	
	recent := values[len(values)-3:]
	
	if recent[2] > recent[1] && recent[1] > recent[0] {
		return "rising"
	} else if recent[2] < recent[1] && recent[1] < recent[0] {
		return "falling"
	} else {
		return "stable"
	}
}

// Built-in emergency action implementations

// executeLoadShedding executes load shedding
func (epm *EmergencyProtocolManager) executeLoadShedding(ctx context.Context, params map[string]interface{}) error {
	// Implementation would integrate with load balancer or request router
	// For now, simulate the action
	time.Sleep(100 * time.Millisecond)
	return nil
}

// executeForceGC forces garbage collection
func (epm *EmergencyProtocolManager) executeForceGC(ctx context.Context, params map[string]interface{}) error {
	runtime.GC()
	runtime.GC() // Force twice for aggressive collection
	return nil
}

// executeReduceCache reduces cache size
func (epm *EmergencyProtocolManager) executeReduceCache(ctx context.Context, params map[string]interface{}) error {
	// Implementation would integrate with cache manager
	// For now, simulate the action
	time.Sleep(50 * time.Millisecond)
	return nil
}

// executeOpenCircuitBreakers opens circuit breakers
func (epm *EmergencyProtocolManager) executeOpenCircuitBreakers(ctx context.Context, params map[string]interface{}) error {
	// Implementation would integrate with circuit breaker manager
	for _, cb := range epm.circuitBreakers {
		// Force circuit breaker to open state
		cb.recordFailure()
	}
	return nil
}

// executeEnableDegradation enables graceful degradation
func (epm *EmergencyProtocolManager) executeEnableDegradation(ctx context.Context, params map[string]interface{}) error {
	if epm.degradationManager != nil {
		// Trigger degradation by simulating low health
		epm.degradationManager.UpdateHealth(HealthMeasurement{
			Timestamp: time.Now(),
			Score:     0.6, // Trigger degradation
		})
	}
	return nil
}

// Emergency Executor implementation

// Start starts an emergency executor worker
func (ee *EmergencyExecutor) Start(ctx context.Context) {
	ee.Running = true
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ee.StopChan:
			return
		case execution := <-ee.TaskQueue:
			ee.executeEmergencyProtocol(ctx, execution)
		}
	}
}

// Stop stops an emergency executor worker
func (ee *EmergencyExecutor) Stop() {
	ee.Running = false
	close(ee.StopChan)
}

// executeEmergencyProtocol executes an emergency protocol
func (ee *EmergencyExecutor) executeEmergencyProtocol(ctx context.Context, execution *EmergencyExecution) {
	ee.CurrentTask = execution
	defer func() { ee.CurrentTask = nil }()
	
	execution.Status = "executing"
	execution.StartTime = time.Now()
	
	// Get protocol
	ee.Manager.protocolMutex.RLock()
	protocols := ee.Manager.protocols[execution.Condition]
	var protocol *EmergencyProtocol
	for _, p := range protocols {
		if p.ID == execution.ProtocolID {
			protocol = p
			break
		}
	}
	ee.Manager.protocolMutex.RUnlock()
	
	if protocol == nil {
		execution.Status = "failed"
		execution.ErrorMessage = "Protocol not found"
		execution.EndTime = time.Now()
		execution.Duration = execution.EndTime.Sub(execution.StartTime)
		return
	}
	
	// Execute actions
	executionCtx, cancel := context.WithTimeout(ctx, protocol.Timeout)
	defer cancel()
	
	for _, action := range protocol.Actions {
		if action.ExecuteFunc != nil {
			err := action.ExecuteFunc(executionCtx, action.Parameters)
			if err != nil {
				execution.ActionsFailed = append(execution.ActionsFailed, action.Type)
				execution.ErrorMessage = err.Error()
			} else {
				execution.ActionsSuccess = append(execution.ActionsSuccess, action.Type)
			}
			execution.ActionsExecuted = append(execution.ActionsExecuted, action.Type)
		}
	}
	
	execution.EndTime = time.Now()
	execution.Duration = execution.EndTime.Sub(execution.StartTime)
	
	if len(execution.ActionsFailed) == 0 {
		execution.Status = "success"
		atomic.AddUint64(&ee.Manager.metrics.SuccessfulResponses, 1)
	} else {
		execution.Status = "failed"
		atomic.AddUint64(&ee.Manager.metrics.FailedResponses, 1)
	}
	
	atomic.AddUint64(&ee.Manager.metrics.ProtocolsExecuted, 1)
}

// GetCurrentEmergencyLevel returns the current overall emergency level
func (epm *EmergencyProtocolManager) GetCurrentEmergencyLevel() EmergencyLevel {
	return EmergencyLevel(atomic.LoadInt32(&epm.currentLevel))
}

// GetActiveEmergencies returns current active emergencies
func (epm *EmergencyProtocolManager) GetActiveEmergencies() map[EmergencyCondition]*ActiveEmergency {
	epm.emergencyMutex.RLock()
	defer epm.emergencyMutex.RUnlock()
	
	// Return a copy
	result := make(map[EmergencyCondition]*ActiveEmergency)
	for k, v := range epm.activeEmergencies {
		emergencyCopy := *v
		result[k] = &emergencyCopy
	}
	return result
}

// GetMetrics returns emergency protocol metrics
func (epm *EmergencyProtocolManager) GetMetrics() *EmergencyMetrics {
	metrics := *epm.metrics
	metrics.LastUpdated = time.Now()
	return &metrics
}

// SetHealthChecker sets the health checker for integration
func (epm *EmergencyProtocolManager) SetHealthChecker(hc *HealthChecker) {
	epm.healthChecker = hc
}

// SetDegradationManager sets the degradation manager for integration
func (epm *EmergencyProtocolManager) SetDegradationManager(dm *DegradationManager) {
	epm.degradationManager = dm
}

// SetCircuitBreakers sets circuit breakers for integration
func (epm *EmergencyProtocolManager) SetCircuitBreakers(cbs map[string]*CircuitBreaker) {
	epm.circuitBreakers = cbs
}

// Global emergency protocol manager instance
var globalEmergencyProtocolManager *EmergencyProtocolManager

// InitializeGlobalEmergencyProtocolManager initializes the global emergency protocol manager
func InitializeGlobalEmergencyProtocolManager(config *EmergencyConfig) error {
	globalEmergencyProtocolManager = NewEmergencyProtocolManager(config)
	return globalEmergencyProtocolManager.Start(context.Background())
}

// GetGlobalEmergencyProtocolManager returns the global emergency protocol manager
func GetGlobalEmergencyProtocolManager() *EmergencyProtocolManager {
	return globalEmergencyProtocolManager
}

// GetGlobalEmergencyLevel returns the global emergency level
func GetGlobalEmergencyLevel() EmergencyLevel {
	if globalEmergencyProtocolManager != nil {
		return globalEmergencyProtocolManager.GetCurrentEmergencyLevel()
	}
	return EmergencyNone
}