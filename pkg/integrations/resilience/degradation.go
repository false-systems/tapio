package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// DegradationManager manages system degradation detection and response
type DegradationManager struct {
	// Configuration
	config *DegradationConfig

	// State tracking
	currentHealth    int64 // atomic: float64 * 1000 for precision
	degradationLevel int32 // atomic: DegradationLevel
	lastHealthUpdate int64 // atomic: unix timestamp in nanoseconds

	// Degradation detection
	healthHistory    []HealthMeasurement
	historyMutex     sync.RWMutex
	trendAnalyzer    *TrendAnalyzer
	thresholdMonitor *ThresholdMonitor

	// Response management
	responseManager *ResponseManager
	recoveryManager *RecoveryManager
	alertManager    *AlertManager

	// Performance tracking
	detectionCount    uint64 // atomic
	responseCount     uint64 // atomic
	recoveryCount     uint64 // atomic
	falsePositiveRate float64

	// State management
	mu       sync.RWMutex
	running  bool
	stopChan chan struct{}
}

// DegradationConfig configures degradation management
type DegradationConfig struct {
	// Detection thresholds
	HealthyThreshold  float64 `json:"healthy_threshold"`  // 0.95
	DegradedThreshold float64 `json:"degraded_threshold"` // 0.8
	CriticalThreshold float64 `json:"critical_threshold"` // 0.6
	FailureThreshold  float64 `json:"failure_threshold"`  // 0.4

	// Detection sensitivity
	TrendWindowSize    int           `json:"trend_window_size"`   // 10 measurements
	DetectionWindow    time.Duration `json:"detection_window"`    // 5 minutes
	ConfirmationWindow time.Duration `json:"confirmation_window"` // 30 seconds
	MinSampleSize      int           `json:"min_sample_size"`     // 3 measurements

	// Response configuration
	AutoRecoveryEnabled bool          `json:"auto_recovery_enabled"` // true
	ResponseTimeout     time.Duration `json:"response_timeout"`      // 30 seconds
	RecoveryTimeout     time.Duration `json:"recovery_timeout"`      // 5 minutes
	MaxRetryAttempts    int           `json:"max_retry_attempts"`    // 3

	// Alert configuration
	AlertsEnabled     bool          `json:"alerts_enabled"`     // true
	AlertCooldown     time.Duration `json:"alert_cooldown"`     // 10 minutes
	EscalationTimeout time.Duration `json:"escalation_timeout"` // 30 minutes

	// Performance settings
	HistorySize     int           `json:"history_size"`     // 1000 measurements
	CleanupInterval time.Duration `json:"cleanup_interval"` // 1 hour
	MetricsEnabled  bool          `json:"metrics_enabled"`  // true
}

// DegradationLevel represents the level of system degradation
type DegradationLevel int32

const (
	DegradationNone DegradationLevel = iota
	DegradationMinor
	DegradationModerate
	DegradationSevere
	DegradationCritical
	DegradationFailure
)

func (d DegradationLevel) String() string {
	switch d {
	case DegradationNone:
		return "none"
	case DegradationMinor:
		return "minor"
	case DegradationModerate:
		return "moderate"
	case DegradationSevere:
		return "severe"
	case DegradationCritical:
		return "critical"
	case DegradationFailure:
		return "failure"
	default:
		return "unknown"
	}
}

// HealthMeasurement represents a health measurement point
type HealthMeasurement struct {
	Timestamp time.Time `json:"timestamp"`
	Score     float64   `json:"score"`
	Metric    string    `json:"metric"`
	Value     float64   `json:"value"`
	Status    string    `json:"status"`
	Source    string    `json:"source"`
}

// TrendAnalyzer analyzes health trends for degradation detection
type TrendAnalyzer struct {
	config       *TrendConfig
	trendMetrics *TrendMetrics
}

// TrendConfig configures trend analysis
type TrendConfig struct {
	WindowSize      int     `json:"window_size"`      // 10
	SmoothingFactor float64 `json:"smoothing_factor"` // 0.3
	TrendThreshold  float64 `json:"trend_threshold"`  // 0.1
	VolatilityLimit float64 `json:"volatility_limit"` // 0.2
}

// TrendMetrics contains trend analysis results
type TrendMetrics struct {
	Slope       float64   `json:"slope"`
	R2          float64   `json:"r2"`
	Volatility  float64   `json:"volatility"`
	Trend       string    `json:"trend"` // "improving", "stable", "degrading"
	Confidence  float64   `json:"confidence"`
	LastUpdated time.Time `json:"last_updated"`
}

// ThresholdMonitor monitors health thresholds
type ThresholdMonitor struct {
	config            *ThresholdConfig
	thresholdBreaches map[string]*ThresholdBreach
	mu                sync.RWMutex
}

// ThresholdConfig configures threshold monitoring
type ThresholdConfig struct {
	StaticThresholds  map[string]float64 `json:"static_thresholds"`
	DynamicThresholds bool               `json:"dynamic_thresholds"`
	AdaptationRate    float64            `json:"adaptation_rate"`
	BreachCooldown    time.Duration      `json:"breach_cooldown"`
}

// ThresholdBreach represents a threshold breach event
type ThresholdBreach struct {
	Threshold    string        `json:"threshold"`
	Value        float64       `json:"value"`
	Limit        float64       `json:"limit"`
	BreachTime   time.Time     `json:"breach_time"`
	Duration     time.Duration `json:"duration"`
	Severity     string        `json:"severity"`
	Acknowledged bool          `json:"acknowledged"`
}

// ResponseManager manages degradation responses
type ResponseManager struct {
	config    *ResponseConfig
	responses map[DegradationLevel][]Response
	executor  *ResponseExecutor
}

// ResponseConfig configures degradation responses
type ResponseConfig struct {
	ResponseMapping   map[string][]string `json:"response_mapping"`
	ParallelExecution bool                `json:"parallel_execution"`
	FailureHandling   string              `json:"failure_handling"` // "continue", "abort", "retry"
	ExecutionTimeout  time.Duration       `json:"execution_timeout"`
}

// Response represents a degradation response action
type Response interface {
	Execute(ctx context.Context) error
	GetName() string
	GetTimeout() time.Duration
	IsReversible() bool
}

// ResponseExecutor executes degradation responses
type ResponseExecutor struct {
	executionHistory []ResponseExecution
	activeResponses  map[string]context.CancelFunc
	mu               sync.RWMutex
}

// ResponseExecution tracks response execution
type ResponseExecution struct {
	ResponseName string        `json:"response_name"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Duration     time.Duration `json:"duration"`
	Success      bool          `json:"success"`
	Error        string        `json:"error"`
}

// RecoveryManager manages system recovery
type RecoveryManager struct {
	config        *RecoveryConfig
	recoveryPlan  *RecoveryPlan
	recoveryState *RecoveryState
}

// RecoveryConfig configures recovery management
type RecoveryConfig struct {
	AutoRecovery      bool          `json:"auto_recovery"`
	RecoverySteps     []string      `json:"recovery_steps"`
	StepTimeout       time.Duration `json:"step_timeout"`
	VerificationDelay time.Duration `json:"verification_delay"`
	MaxRecoveryTime   time.Duration `json:"max_recovery_time"`
}

// RecoveryPlan defines recovery steps
type RecoveryPlan struct {
	Steps         []RecoveryStep `json:"steps"`
	Verification  []string       `json:"verification"`
	Rollback      []string       `json:"rollback"`
	EstimatedTime time.Duration  `json:"estimated_time"`
}

// RecoveryStep represents a single recovery step
type RecoveryStep struct {
	Name         string        `json:"name"`
	Action       string        `json:"action"`
	Timeout      time.Duration `json:"timeout"`
	Dependencies []string      `json:"dependencies"`
	Verification string        `json:"verification"`
}

// RecoveryState tracks recovery progress
type RecoveryState struct {
	InProgress    bool                 `json:"in_progress"`
	CurrentStep   int                  `json:"current_step"`
	StartTime     time.Time            `json:"start_time"`
	StepResults   []RecoveryStepResult `json:"step_results"`
	OverallResult string               `json:"overall_result"`
}

// RecoveryStepResult tracks individual step results
type RecoveryStepResult struct {
	StepName  string        `json:"step_name"`
	Success   bool          `json:"success"`
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error"`
	Timestamp time.Time     `json:"timestamp"`
}

// AlertManager manages degradation alerts
type AlertManager struct {
	config        *AlertConfig
	alertChannels []AlertChannel
	alertHistory  []Alert
	mu            sync.RWMutex
}

// AlertConfig configures alert management
type AlertConfig struct {
	Channels    []string      `json:"channels"`    // "email", "slack", "webhook"
	Severity    []string      `json:"severity"`    // "info", "warning", "critical"
	Cooldown    time.Duration `json:"cooldown"`    // 10 minutes
	Escalation  bool          `json:"escalation"`  // true
	Aggregation bool          `json:"aggregation"` // true
}

// AlertChannel represents an alert delivery channel
type AlertChannel interface {
	SendAlert(alert Alert) error
	GetName() string
	IsHealthy() bool
}

// Alert represents a degradation alert
type Alert struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Severity     string                 `json:"severity"`
	Title        string                 `json:"title"`
	Message      string                 `json:"message"`
	Timestamp    time.Time              `json:"timestamp"`
	Source       string                 `json:"source"`
	Metadata     map[string]interface{} `json:"metadata"`
	Acknowledged bool                   `json:"acknowledged"`
	Resolved     bool                   `json:"resolved"`
}

// NewDegradationManager creates a new degradation manager
func NewDegradationManager(config *DegradationConfig) *DegradationManager {
	if config == nil {
		config = DefaultDegradationConfig()
	}

	dm := &DegradationManager{
		config:        config,
		healthHistory: make([]HealthMeasurement, 0, config.HistorySize),
		stopChan:      make(chan struct{}),
	}

	// Initialize trend analyzer
	dm.trendAnalyzer = &TrendAnalyzer{
		config: &TrendConfig{
			WindowSize:      config.TrendWindowSize,
			SmoothingFactor: 0.3,
			TrendThreshold:  0.1,
			VolatilityLimit: 0.2,
		},
		trendMetrics: &TrendMetrics{},
	}

	// Initialize threshold monitor
	dm.thresholdMonitor = &ThresholdMonitor{
		config: &ThresholdConfig{
			StaticThresholds: map[string]float64{
				"healthy":  config.HealthyThreshold,
				"degraded": config.DegradedThreshold,
				"critical": config.CriticalThreshold,
				"failure":  config.FailureThreshold,
			},
			DynamicThresholds: true,
			AdaptationRate:    0.1,
			BreachCooldown:    time.Minute,
		},
		thresholdBreaches: make(map[string]*ThresholdBreach),
	}

	// Initialize response manager
	dm.responseManager = &ResponseManager{
		config: &ResponseConfig{
			ResponseMapping: map[string][]string{
				"minor":    {"log_warning", "increase_monitoring"},
				"moderate": {"reduce_load", "scale_up"},
				"severe":   {"circuit_breaker", "failover"},
				"critical": {"emergency_response", "alert_oncall"},
			},
			ParallelExecution: false,
			FailureHandling:   "continue",
			ExecutionTimeout:  config.ResponseTimeout,
		},
		responses: make(map[DegradationLevel][]Response),
		executor: &ResponseExecutor{
			executionHistory: make([]ResponseExecution, 0),
			activeResponses:  make(map[string]context.CancelFunc),
		},
	}

	// Initialize recovery manager
	dm.recoveryManager = &RecoveryManager{
		config: &RecoveryConfig{
			AutoRecovery:      config.AutoRecoveryEnabled,
			RecoverySteps:     []string{"diagnose", "isolate", "repair", "verify"},
			StepTimeout:       30 * time.Second,
			VerificationDelay: 10 * time.Second,
			MaxRecoveryTime:   config.RecoveryTimeout,
		},
		recoveryState: &RecoveryState{
			StepResults: make([]RecoveryStepResult, 0),
		},
	}

	// Initialize alert manager
	dm.alertManager = &AlertManager{
		config: &AlertConfig{
			Channels:    []string{"log"},
			Severity:    []string{"warning", "critical"},
			Cooldown:    config.AlertCooldown,
			Escalation:  true,
			Aggregation: true,
		},
		alertChannels: make([]AlertChannel, 0),
		alertHistory:  make([]Alert, 0),
	}

	// Initialize atomic values
	atomic.StoreInt64(&dm.currentHealth, int64(1000)) // 1.0 * 1000
	atomic.StoreInt32(&dm.degradationLevel, int32(DegradationNone))
	atomic.StoreInt64(&dm.lastHealthUpdate, time.Now().UnixNano())

	return dm
}

// DefaultDegradationConfig returns default degradation configuration
func DefaultDegradationConfig() *DegradationConfig {
	return &DegradationConfig{
		HealthyThreshold:    0.95,
		DegradedThreshold:   0.8,
		CriticalThreshold:   0.6,
		FailureThreshold:    0.4,
		TrendWindowSize:     10,
		DetectionWindow:     5 * time.Minute,
		ConfirmationWindow:  30 * time.Second,
		MinSampleSize:       3,
		AutoRecoveryEnabled: true,
		ResponseTimeout:     30 * time.Second,
		RecoveryTimeout:     5 * time.Minute,
		MaxRetryAttempts:    3,
		AlertsEnabled:       true,
		AlertCooldown:       10 * time.Minute,
		EscalationTimeout:   30 * time.Minute,
		HistorySize:         1000,
		CleanupInterval:     time.Hour,
		MetricsEnabled:      true,
	}
}

// Start starts the degradation manager
func (dm *DegradationManager) Start(ctx context.Context) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.running {
		return fmt.Errorf("degradation manager already running")
	}

	dm.running = true

	// Start monitoring goroutine
	go dm.monitoringLoop(ctx)

	// Start cleanup goroutine
	go dm.cleanupLoop(ctx)

	return nil
}

// Stop stops the degradation manager
func (dm *DegradationManager) Stop() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if !dm.running {
		return nil
	}

	dm.running = false
	close(dm.stopChan)

	return nil
}

// UpdateHealth updates the current health measurement
func (dm *DegradationManager) UpdateHealth(measurement HealthMeasurement) {
	// Update atomic health value
	atomic.StoreInt64(&dm.currentHealth, int64(measurement.Score*1000))
	atomic.StoreInt64(&dm.lastHealthUpdate, time.Now().UnixNano())

	// Add to history
	dm.historyMutex.Lock()
	dm.healthHistory = append(dm.healthHistory, measurement)

	// Trim history if it exceeds max size
	if len(dm.healthHistory) > dm.config.HistorySize {
		dm.healthHistory = dm.healthHistory[1:]
	}
	dm.historyMutex.Unlock()

	// Analyze for degradation
	dm.analyzeDegradation(measurement)
}

// GetCurrentHealth returns the current health score
func (dm *DegradationManager) GetCurrentHealth() float64 {
	return float64(atomic.LoadInt64(&dm.currentHealth)) / 1000.0
}

// GetDegradationLevel returns the current degradation level
func (dm *DegradationManager) GetDegradationLevel() DegradationLevel {
	return DegradationLevel(atomic.LoadInt32(&dm.degradationLevel))
}

// GetCurrentLevel returns the current degradation level (alias for backward compatibility)
func (dm *DegradationManager) GetCurrentLevel() DegradationLevel {
	return dm.GetDegradationLevel()
}

// GetHealthHistory returns recent health history
func (dm *DegradationManager) GetHealthHistory(limit int) []HealthMeasurement {
	dm.historyMutex.RLock()
	defer dm.historyMutex.RUnlock()

	if limit <= 0 || limit > len(dm.healthHistory) {
		limit = len(dm.healthHistory)
	}

	start := len(dm.healthHistory) - limit
	result := make([]HealthMeasurement, limit)
	copy(result, dm.healthHistory[start:])

	return result
}

// analyzeDegradation analyzes health measurements for degradation
func (dm *DegradationManager) analyzeDegradation(measurement HealthMeasurement) {
	// Determine degradation level from health score
	newLevel := dm.calculateDegradationLevel(measurement.Score)
	currentLevel := dm.GetDegradationLevel()

	// Update degradation level if changed
	if newLevel != currentLevel {
		atomic.StoreInt32(&dm.degradationLevel, int32(newLevel))
		atomic.AddUint64(&dm.detectionCount, 1)

		// Handle degradation level change
		dm.handleDegradationChange(currentLevel, newLevel, measurement)
	}

	// Update trend analysis
	dm.updateTrendAnalysis(measurement)

	// Check threshold breaches
	dm.checkThresholdBreaches(measurement)
}

// calculateDegradationLevel calculates degradation level from health score
func (dm *DegradationManager) calculateDegradationLevel(healthScore float64) DegradationLevel {
	if healthScore >= dm.config.HealthyThreshold {
		return DegradationNone
	} else if healthScore >= dm.config.DegradedThreshold {
		return DegradationMinor
	} else if healthScore >= dm.config.CriticalThreshold {
		return DegradationModerate
	} else if healthScore >= dm.config.FailureThreshold {
		return DegradationSevere
	} else {
		return DegradationCritical
	}
}

// handleDegradationChange handles changes in degradation level
func (dm *DegradationManager) handleDegradationChange(oldLevel, newLevel DegradationLevel, measurement HealthMeasurement) {
	// Send alert if degradation worsened
	if newLevel > oldLevel {
		alert := Alert{
			ID:        fmt.Sprintf("degradation_%d", time.Now().Unix()),
			Type:      "degradation",
			Severity:  dm.mapDegradationToSeverity(newLevel),
			Title:     fmt.Sprintf("System degradation detected: %s", newLevel.String()),
			Message:   fmt.Sprintf("Health score dropped to %.3f (was %.3f)", measurement.Score, dm.GetCurrentHealth()),
			Timestamp: time.Now(),
			Source:    "degradation_manager",
			Metadata: map[string]interface{}{
				"old_level":    oldLevel.String(),
				"new_level":    newLevel.String(),
				"health_score": measurement.Score,
			},
		}
		dm.sendAlert(alert)
	}

	// Execute degradation response
	if dm.config.AutoRecoveryEnabled {
		go dm.executeResponse(newLevel, measurement)
	}
}

// executeResponse executes appropriate response for degradation level
func (dm *DegradationManager) executeResponse(level DegradationLevel, measurement HealthMeasurement) {
	ctx, cancel := context.WithTimeout(context.Background(), dm.config.ResponseTimeout)
	defer cancel()

	// Get responses for this degradation level
	responses := dm.responseManager.responses[level]

	for _, response := range responses {
		err := response.Execute(ctx)
		if err != nil {
			// Log response execution error
			continue
		}
		atomic.AddUint64(&dm.responseCount, 1)
	}
}

// updateTrendAnalysis updates trend analysis with new measurement
func (dm *DegradationManager) updateTrendAnalysis(measurement HealthMeasurement) {
	// Get recent measurements for trend analysis
	recentMeasurements := dm.GetHealthHistory(dm.config.TrendWindowSize)

	if len(recentMeasurements) < dm.config.MinSampleSize {
		return
	}

	// Calculate trend metrics (simplified implementation)
	dm.trendAnalyzer.trendMetrics.LastUpdated = time.Now()

	// Calculate slope (simplified linear regression)
	n := len(recentMeasurements)
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0

	for i, m := range recentMeasurements {
		x := float64(i)
		y := m.Score
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	slope := (float64(n)*sumXY - sumX*sumY) / (float64(n)*sumX2 - sumX*sumX)
	dm.trendAnalyzer.trendMetrics.Slope = slope

	// Determine trend direction
	if slope > dm.trendAnalyzer.config.TrendThreshold {
		dm.trendAnalyzer.trendMetrics.Trend = "improving"
	} else if slope < -dm.trendAnalyzer.config.TrendThreshold {
		dm.trendAnalyzer.trendMetrics.Trend = "degrading"
	} else {
		dm.trendAnalyzer.trendMetrics.Trend = "stable"
	}

	dm.trendAnalyzer.trendMetrics.Confidence = 0.8 // Simplified confidence
}

// checkThresholdBreaches checks for threshold breaches
func (dm *DegradationManager) checkThresholdBreaches(measurement HealthMeasurement) {
	dm.thresholdMonitor.mu.Lock()
	defer dm.thresholdMonitor.mu.Unlock()

	for name, threshold := range dm.thresholdMonitor.config.StaticThresholds {
		if measurement.Score < threshold {
			// Threshold breached
			breach := &ThresholdBreach{
				Threshold:  name,
				Value:      measurement.Score,
				Limit:      threshold,
				BreachTime: measurement.Timestamp,
				Severity:   dm.mapThresholdToSeverity(name),
			}
			dm.thresholdMonitor.thresholdBreaches[name] = breach
		} else {
			// Threshold restored
			delete(dm.thresholdMonitor.thresholdBreaches, name)
		}
	}
}

// sendAlert sends an alert through configured channels
func (dm *DegradationManager) sendAlert(alert Alert) {
	dm.alertManager.mu.Lock()
	defer dm.alertManager.mu.Unlock()

	// Add to alert history
	dm.alertManager.alertHistory = append(dm.alertManager.alertHistory, alert)

	// Send through alert channels
	for _, channel := range dm.alertManager.alertChannels {
		go func(ch AlertChannel, a Alert) {
			ch.SendAlert(a)
		}(channel, alert)
	}
}

// monitoringLoop runs the main monitoring loop
func (dm *DegradationManager) monitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(dm.config.DetectionWindow)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-dm.stopChan:
			return
		case <-ticker.C:
			dm.performPeriodicAnalysis()
		}
	}
}

// cleanupLoop performs periodic cleanup
func (dm *DegradationManager) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(dm.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-dm.stopChan:
			return
		case <-ticker.C:
			dm.performCleanup()
		}
	}
}

// performPeriodicAnalysis performs periodic degradation analysis
func (dm *DegradationManager) performPeriodicAnalysis() {
	// Get recent health measurements
	recentMeasurements := dm.GetHealthHistory(dm.config.TrendWindowSize)

	if len(recentMeasurements) < dm.config.MinSampleSize {
		return
	}

	// Analyze trends and patterns
	dm.analyzeTrends(recentMeasurements)

	// Check for anomalies
	dm.detectAnomalies(recentMeasurements)
}

// performCleanup performs periodic cleanup of old data
func (dm *DegradationManager) performCleanup() {
	cutoff := time.Now().Add(-24 * time.Hour)

	// Clean alert history
	dm.alertManager.mu.Lock()
	filtered := make([]Alert, 0)
	for _, alert := range dm.alertManager.alertHistory {
		if alert.Timestamp.After(cutoff) {
			filtered = append(filtered, alert)
		}
	}
	dm.alertManager.alertHistory = filtered
	dm.alertManager.mu.Unlock()

	// Clean threshold breaches
	dm.thresholdMonitor.mu.Lock()
	for name, breach := range dm.thresholdMonitor.thresholdBreaches {
		if breach.BreachTime.Before(cutoff) {
			delete(dm.thresholdMonitor.thresholdBreaches, name)
		}
	}
	dm.thresholdMonitor.mu.Unlock()
}

// Helper methods

func (dm *DegradationManager) mapDegradationToSeverity(level DegradationLevel) string {
	switch level {
	case DegradationMinor:
		return "warning"
	case DegradationModerate:
		return "warning"
	case DegradationSevere:
		return "critical"
	case DegradationCritical:
		return "critical"
	case DegradationFailure:
		return "critical"
	default:
		return "info"
	}
}

func (dm *DegradationManager) mapThresholdToSeverity(threshold string) string {
	switch threshold {
	case "healthy":
		return "info"
	case "degraded":
		return "warning"
	case "critical":
		return "critical"
	case "failure":
		return "critical"
	default:
		return "warning"
	}
}

func (dm *DegradationManager) analyzeTrends(measurements []HealthMeasurement) {
	if len(measurements) < 2 {
		return
	}

	// Calculate moving averages for short and long term trends
	shortTermWindow := min(5, len(measurements))
	longTermWindow := min(10, len(measurements))

	shortTermAvg := dm.calculateMovingAverage(measurements, shortTermWindow)
	longTermAvg := dm.calculateMovingAverage(measurements, longTermWindow)

	// Detect trend direction
	if shortTermAvg > longTermAvg+0.05 {
		dm.trendAnalyzer.trendMetrics.Trend = "improving"
		dm.trendAnalyzer.trendMetrics.Confidence = 0.8
	} else if shortTermAvg < longTermAvg-0.05 {
		dm.trendAnalyzer.trendMetrics.Trend = "degrading"
		dm.trendAnalyzer.trendMetrics.Confidence = 0.8
		
		// Trigger early warning for degrading trends
		if dm.trendAnalyzer.trendMetrics.Trend == "degrading" && shortTermAvg < dm.config.DegradedThreshold {
			dm.sendTrendAlert("degrading_trend_detected", shortTermAvg, longTermAvg)
		}
	} else {
		dm.trendAnalyzer.trendMetrics.Trend = "stable"
		dm.trendAnalyzer.trendMetrics.Confidence = 0.6
	}

	// Calculate trend velocity (rate of change)
	if len(measurements) >= 3 {
		velocity := dm.calculateTrendVelocity(measurements)
		dm.trendAnalyzer.trendMetrics.Slope = velocity
		
		// High velocity changes are more concerning
		if abs(velocity) > 0.1 {
			dm.trendAnalyzer.trendMetrics.Confidence = min(1.0, dm.trendAnalyzer.trendMetrics.Confidence+0.2)
		}
	}

	// Update volatility measurement
	volatility := dm.calculateVolatility(measurements)
	dm.trendAnalyzer.trendMetrics.Volatility = volatility
	
	// High volatility reduces confidence in trend detection
	if volatility > dm.trendAnalyzer.config.VolatilityLimit {
		dm.trendAnalyzer.trendMetrics.Confidence *= 0.7
	}

	dm.trendAnalyzer.trendMetrics.LastUpdated = time.Now()
}

func (dm *DegradationManager) detectAnomalies(measurements []HealthMeasurement) {
	if len(measurements) < 3 {
		return
	}

	latestMeasurement := measurements[len(measurements)-1]
	
	// Statistical anomaly detection using Z-score
	anomalyScore := dm.calculateAnomalyScore(measurements, latestMeasurement.Score)
	
	// Threshold for anomaly detection (typically 2-3 standard deviations)
	anomalyThreshold := 2.5
	
	if abs(anomalyScore) > anomalyThreshold {
		anomalyType := "statistical_outlier"
		severity := "warning"
		
		if abs(anomalyScore) > 3.0 {
			severity = "critical"
		}
		
		dm.sendAnomalyAlert(anomalyType, severity, latestMeasurement, anomalyScore)
	}

	// Detect sudden drops (cliff detection)
	if len(measurements) >= 2 {
		previousMeasurement := measurements[len(measurements)-2]
		dropThreshold := 0.2 // 20% drop
		
		if latestMeasurement.Score < previousMeasurement.Score-dropThreshold {
			dm.sendAnomalyAlert("sudden_drop", "critical", latestMeasurement, 
				(previousMeasurement.Score - latestMeasurement.Score))
		}
	}

	// Detect sustained degradation patterns
	dm.detectSustainedDegradation(measurements)
	
	// Detect cyclical patterns that might indicate systemic issues
	dm.detectCyclicalAnomalies(measurements)
}

// Helper methods for trend analysis

// calculateMovingAverage calculates moving average over specified window
func (dm *DegradationManager) calculateMovingAverage(measurements []HealthMeasurement, window int) float64 {
	if len(measurements) == 0 || window <= 0 {
		return 0.0
	}

	start := maxInt(0, len(measurements)-window)
	sum := 0.0
	count := 0

	for i := start; i < len(measurements); i++ {
		sum += measurements[i].Score
		count++
	}

	if count == 0 {
		return 0.0
	}
	return sum / float64(count)
}

// calculateTrendVelocity calculates the rate of change in health score
func (dm *DegradationManager) calculateTrendVelocity(measurements []HealthMeasurement) float64 {
	if len(measurements) < 2 {
		return 0.0
	}

	// Use linear regression to calculate slope
	n := len(measurements)
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0

	for i, measurement := range measurements {
		x := float64(i)
		y := measurement.Score
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// Calculate slope (velocity)
	denominator := float64(n)*sumX2 - sumX*sumX
	if denominator == 0 {
		return 0.0
	}

	slope := (float64(n)*sumXY - sumX*sumY) / denominator
	return slope
}

// calculateVolatility calculates the volatility (standard deviation) of measurements
func (dm *DegradationManager) calculateVolatility(measurements []HealthMeasurement) float64 {
	if len(measurements) < 2 {
		return 0.0
	}

	// Calculate mean
	sum := 0.0
	for _, measurement := range measurements {
		sum += measurement.Score
	}
	mean := sum / float64(len(measurements))

	// Calculate variance
	variance := 0.0
	for _, measurement := range measurements {
		diff := measurement.Score - mean
		variance += diff * diff
	}
	variance /= float64(len(measurements))

	// Return standard deviation (volatility)
	return sqrt(variance)
}

// sendTrendAlert sends an alert for trend-based events
func (dm *DegradationManager) sendTrendAlert(alertType string, shortTerm, longTerm float64) {
	alert := Alert{
		ID:        fmt.Sprintf("trend_%d", time.Now().Unix()),
		Type:      "trend_alert",
		Severity:  "warning",
		Title:     fmt.Sprintf("Trend analysis: %s", alertType),
		Message:   fmt.Sprintf("Short-term average %.3f vs long-term average %.3f", shortTerm, longTerm),
		Timestamp: time.Now(),
		Source:    "trend_analyzer",
		Metadata: map[string]interface{}{
			"alert_type":        alertType,
			"short_term_avg":    shortTerm,
			"long_term_avg":     longTerm,
			"trend_direction":   dm.trendAnalyzer.trendMetrics.Trend,
			"trend_confidence":  dm.trendAnalyzer.trendMetrics.Confidence,
			"trend_volatility":  dm.trendAnalyzer.trendMetrics.Volatility,
		},
	}
	dm.sendAlert(alert)
}

// Helper methods for anomaly detection

// calculateAnomalyScore calculates Z-score for anomaly detection
func (dm *DegradationManager) calculateAnomalyScore(measurements []HealthMeasurement, value float64) float64 {
	if len(measurements) < 2 {
		return 0.0
	}

	// Calculate mean
	sum := 0.0
	for _, measurement := range measurements {
		sum += measurement.Score
	}
	mean := sum / float64(len(measurements))

	// Calculate standard deviation
	variance := 0.0
	for _, measurement := range measurements {
		diff := measurement.Score - mean
		variance += diff * diff
	}
	variance /= float64(len(measurements))
	stdDev := sqrt(variance)

	if stdDev == 0 {
		return 0.0
	}

	// Return Z-score
	return (value - mean) / stdDev
}

// detectSustainedDegradation detects patterns of sustained degradation
func (dm *DegradationManager) detectSustainedDegradation(measurements []HealthMeasurement) {
	if len(measurements) < 5 {
		return
	}

	// Look for sustained degradation over the last 5 measurements
	windowSize := 5
	start := len(measurements) - windowSize
	degradationCount := 0
	threshold := dm.config.DegradedThreshold

	// Count measurements below threshold
	for i := start; i < len(measurements); i++ {
		if measurements[i].Score < threshold {
			degradationCount++
		}
	}

	// If majority of recent measurements are degraded, it's sustained
	if float64(degradationCount)/float64(windowSize) >= 0.6 {
		latestMeasurement := measurements[len(measurements)-1]
		dm.sendAnomalyAlert("sustained_degradation", "warning", latestMeasurement, 
			float64(degradationCount)/float64(windowSize))
	}
}

// detectCyclicalAnomalies detects cyclical patterns that might indicate systemic issues
func (dm *DegradationManager) detectCyclicalAnomalies(measurements []HealthMeasurement) {
	if len(measurements) < 10 {
		return
	}

	// Look for repeating patterns of degradation
	windowSize := min(len(measurements)/2, 10)
	recentWindow := measurements[len(measurements)-windowSize:]
	
	// Calculate pattern signature for recent window
	recentSignature := dm.calculatePatternSignature(recentWindow)
	
	// Compare with earlier windows to detect repetition
	for i := 0; i < len(measurements)-2*windowSize; i += windowSize {
		if i+windowSize >= len(measurements)-windowSize {
			break
		}
		
		compareWindow := measurements[i : i+windowSize]
		compareSignature := dm.calculatePatternSignature(compareWindow)
		
		// If patterns are similar, we might have a cyclical issue
		similarity := dm.calculatePatternSimilarity(recentSignature, compareSignature)
		if similarity > 0.8 {
			latestMeasurement := measurements[len(measurements)-1]
			dm.sendAnomalyAlert("cyclical_pattern", "warning", latestMeasurement, similarity)
			break
		}
	}
}

// calculatePatternSignature creates a signature for a pattern of measurements
func (dm *DegradationManager) calculatePatternSignature(measurements []HealthMeasurement) []float64 {
	if len(measurements) == 0 {
		return nil
	}

	signature := make([]float64, len(measurements))
	
	// Normalize measurements relative to the first measurement
	baseline := measurements[0].Score
	if baseline == 0 {
		baseline = 0.001 // Avoid division by zero
	}
	
	for i, measurement := range measurements {
		signature[i] = measurement.Score / baseline
	}
	
	return signature
}

// calculatePatternSimilarity calculates similarity between two pattern signatures
func (dm *DegradationManager) calculatePatternSimilarity(sig1, sig2 []float64) float64 {
	if len(sig1) != len(sig2) || len(sig1) == 0 {
		return 0.0
	}

	// Calculate correlation coefficient
	mean1 := dm.calculateMean(sig1)
	mean2 := dm.calculateMean(sig2)
	
	numerator := 0.0
	sumSq1 := 0.0
	sumSq2 := 0.0
	
	for i := 0; i < len(sig1); i++ {
		diff1 := sig1[i] - mean1
		diff2 := sig2[i] - mean2
		
		numerator += diff1 * diff2
		sumSq1 += diff1 * diff1
		sumSq2 += diff2 * diff2
	}
	
	denominator := sqrt(sumSq1 * sumSq2)
	if denominator == 0 {
		return 0.0
	}
	
	correlation := numerator / denominator
	return abs(correlation) // Return absolute correlation
}

// calculateMean calculates the mean of a slice of floats
func (dm *DegradationManager) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	
	return sum / float64(len(values))
}

// sendAnomalyAlert sends an alert for detected anomalies
func (dm *DegradationManager) sendAnomalyAlert(anomalyType, severity string, measurement HealthMeasurement, score float64) {
	alert := Alert{
		ID:        fmt.Sprintf("anomaly_%d", time.Now().Unix()),
		Type:      "anomaly_alert",
		Severity:  severity,
		Title:     fmt.Sprintf("Anomaly detected: %s", anomalyType),
		Message:   fmt.Sprintf("Health score %.3f shows %s (score: %.2f)", measurement.Score, anomalyType, score),
		Timestamp: time.Now(),
		Source:    "anomaly_detector",
		Metadata: map[string]interface{}{
			"anomaly_type":   anomalyType,
			"health_score":   measurement.Score,
			"anomaly_score":  score,
			"measurement_time": measurement.Timestamp,
			"metric":         measurement.Metric,
			"source":         measurement.Source,
		},
	}
	dm.sendAlert(alert)
}

// Utility functions
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Simple Newton-Raphson method for square root
	guess := x / 2
	for i := 0; i < 10; i++ {
		guess = (guess + x/guess) / 2
	}
	return guess
}

// GetStats returns degradation manager statistics
func (dm *DegradationManager) GetStats() *DegradationStats {
	return &DegradationStats{
		CurrentHealth:     dm.GetCurrentHealth(),
		DegradationLevel:  dm.GetDegradationLevel().String(),
		DetectionCount:    atomic.LoadUint64(&dm.detectionCount),
		ResponseCount:     atomic.LoadUint64(&dm.responseCount),
		RecoveryCount:     atomic.LoadUint64(&dm.recoveryCount),
		FalsePositiveRate: dm.falsePositiveRate,
		LastUpdate:        time.Unix(0, atomic.LoadInt64(&dm.lastHealthUpdate)),
		TrendMetrics:      dm.trendAnalyzer.trendMetrics,
	}
}

// DegradationStats contains degradation manager statistics
type DegradationStats struct {
	CurrentHealth     float64       `json:"current_health"`
	DegradationLevel  string        `json:"degradation_level"`
	DetectionCount    uint64        `json:"detection_count"`
	ResponseCount     uint64        `json:"response_count"`
	RecoveryCount     uint64        `json:"recovery_count"`
	FalsePositiveRate float64       `json:"false_positive_rate"`
	LastUpdate        time.Time     `json:"last_update"`
	TrendMetrics      *TrendMetrics `json:"trend_metrics"`
}
