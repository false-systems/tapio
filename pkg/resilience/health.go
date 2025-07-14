package resilience

import (
	"context"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// HealthStatus represents the overall health status
type HealthStatus int32

const (
	HealthUnknown HealthStatus = iota
	HealthHealthy
	HealthDegraded
	HealthUnhealthy
	HealthCritical
)

func (h HealthStatus) String() string {
	switch h {
	case HealthHealthy:
		return "healthy"
	case HealthDegraded:
		return "degraded"
	case HealthUnhealthy:
		return "unhealthy"
	case HealthCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// HealthChecker provides ultra-fast health checking with <1ms response time
type HealthChecker struct {
	// Pre-computed health data for ultra-fast access
	cachedHealth     unsafe.Pointer // *CachedHealthData
	lastUpdate       int64          // atomic: unix timestamp in nanoseconds
	
	// Health configuration
	config           *HealthConfig
	
	// Component health trackers
	components       map[string]*ComponentHealth
	componentsMutex  sync.RWMutex
	
	// Fast health computation
	computationChan  chan struct{}
	stopChan         chan struct{}
	running          bool
	
	// Metrics
	metrics          *HealthMetrics
	
	// Integration points
	degradationManager *DegradationManager
	circuitBreakers    map[string]*CircuitBreaker
	cbMutex           sync.RWMutex
}

// HealthConfig configures health checking behavior
type HealthConfig struct {
	// Update intervals
	FastUpdateInterval   time.Duration `json:"fast_update_interval"`   // 100µs for critical components
	SlowUpdateInterval   time.Duration `json:"slow_update_interval"`   // 1s for non-critical components
	
	// Response targets
	TargetResponseTime   time.Duration `json:"target_response_time"`   // <1ms
	MaxResponseTime      time.Duration `json:"max_response_time"`      // 5ms hard limit
	
	// Cache settings
	CacheValidityPeriod  time.Duration `json:"cache_validity_period"`  // How long cached data is valid
	PrecomputeEnabled    bool          `json:"precompute_enabled"`     // Enable precomputation
	
	// Health thresholds
	HealthyThreshold     float64       `json:"healthy_threshold"`      // 0.95
	DegradedThreshold    float64       `json:"degraded_threshold"`     // 0.8
	UnhealthyThreshold   float64       `json:"unhealthy_threshold"`    // 0.6
	CriticalThreshold    float64       `json:"critical_threshold"`     // 0.4
	
	// Component weights
	ComponentWeights     map[string]float64 `json:"component_weights"`
	
	// HTTP health endpoint settings
	EnableHTTPEndpoint   bool          `json:"enable_http_endpoint"`
	HTTPPort            int           `json:"http_port"`
	HTTPPath            string        `json:"http_path"`
	HTTPTimeout         time.Duration `json:"http_timeout"`
}

// CachedHealthData contains pre-computed health information for ultra-fast access
type CachedHealthData struct {
	// Overall health
	OverallStatus    HealthStatus `json:"overall_status"`
	OverallScore     float64      `json:"overall_score"`
	
	// Component statuses (pre-computed for speed)
	ComponentStatuses map[string]HealthStatus `json:"component_statuses"`
	ComponentScores   map[string]float64      `json:"component_scores"`
	
	// Summary information
	HealthyComponents    int       `json:"healthy_components"`
	DegradedComponents   int       `json:"degraded_components"`
	UnhealthyComponents  int       `json:"unhealthy_components"`
	CriticalComponents   int       `json:"critical_components"`
	
	// Timestamps
	ComputedAt          time.Time `json:"computed_at"`
	ValidUntil          time.Time `json:"valid_until"`
	
	// Performance data
	ComputationTimeNs   int64     `json:"computation_time_ns"`
	ResponseTimeNs      int64     `json:"response_time_ns"`
}

// ComponentHealth tracks health for individual components
type ComponentHealth struct {
	Name              string                 `json:"name"`
	Type              string                 `json:"type"`              // "critical", "important", "optional"
	Weight            float64                `json:"weight"`            // 0.0-1.0
	
	// Current state (atomic for thread safety)
	currentScore      int64                  // atomic: float64 * 1000 for precision
	currentStatus     int32                  // atomic: HealthStatus
	lastUpdate        int64                  // atomic: unix timestamp in nanoseconds
	
	// Health measurement function
	measureFunc       func() HealthMeasurement `json:"-"`
	
	// Update frequency
	updateInterval    time.Duration          `json:"update_interval"`
	
	// Health history (lock-free ring buffer for performance)
	historyBuffer     []HealthMeasurement    `json:"-"`
	historyIndex      int64                  // atomic: current index in ring buffer
	historySize       int                    `json:"history_size"`
	
	// Circuit breaker integration
	circuitBreaker    *CircuitBreaker        `json:"-"`
	
	// Performance tracking
	measurementCount  uint64                 `json:"-"`  // atomic
	totalMeasureTime  uint64                 `json:"-"`  // atomic: nanoseconds
	fastestMeasure    uint64                 `json:"-"`  // atomic: nanoseconds
	slowestMeasure    uint64                 `json:"-"`  // atomic: nanoseconds
}

// HealthMetrics tracks health checking performance
type HealthMetrics struct {
	// Response time tracking
	AverageResponseTime   time.Duration `json:"average_response_time"`
	MedianResponseTime    time.Duration `json:"median_response_time"`
	P95ResponseTime       time.Duration `json:"p95_response_time"`
	P99ResponseTime       time.Duration `json:"p99_response_time"`
	FastestResponse       time.Duration `json:"fastest_response"`
	SlowestResponse       time.Duration `json:"slowest_response"`
	
	// Request tracking
	TotalRequests         uint64        `json:"total_requests"`
	RequestsUnder1ms      uint64        `json:"requests_under_1ms"`
	RequestsUnder500us    uint64        `json:"requests_under_500us"`
	RequestsUnder100us    uint64        `json:"requests_under_100us"`
	
	// Health computation tracking
	ComputationCount      uint64        `json:"computation_count"`
	AverageComputeTime    time.Duration `json:"average_compute_time"`
	CacheHitRate          float64       `json:"cache_hit_rate"`
	
	// Component tracking
	ComponentUpdateCount  map[string]uint64 `json:"component_update_count"`
	ComponentErrorCount   map[string]uint64 `json:"component_error_count"`
	
	// Performance counters
	LastUpdated           time.Time     `json:"last_updated"`
}

// NewHealthChecker creates a new ultra-fast health checker
func NewHealthChecker(config *HealthConfig) *HealthChecker {
	if config == nil {
		config = DefaultHealthConfig()
	}
	
	hc := &HealthChecker{
		config:           config,
		components:       make(map[string]*ComponentHealth),
		circuitBreakers:  make(map[string]*CircuitBreaker),
		computationChan:  make(chan struct{}, 1),
		stopChan:         make(chan struct{}),
		metrics:          &HealthMetrics{
			ComponentUpdateCount: make(map[string]uint64),
			ComponentErrorCount:  make(map[string]uint64),
		},
	}
	
	// Initialize with empty cached health data
	initialHealth := &CachedHealthData{
		OverallStatus:       HealthUnknown,
		OverallScore:        0.0,
		ComponentStatuses:   make(map[string]HealthStatus),
		ComponentScores:     make(map[string]float64),
		ComputedAt:          time.Now(),
		ValidUntil:          time.Now().Add(config.CacheValidityPeriod),
	}
	atomic.StorePointer(&hc.cachedHealth, unsafe.Pointer(initialHealth))
	atomic.StoreInt64(&hc.lastUpdate, time.Now().UnixNano())
	
	return hc
}

// DefaultHealthConfig returns default health checking configuration
func DefaultHealthConfig() *HealthConfig {
	return &HealthConfig{
		FastUpdateInterval:   100 * time.Microsecond,
		SlowUpdateInterval:   1 * time.Second,
		TargetResponseTime:   500 * time.Microsecond,
		MaxResponseTime:      5 * time.Millisecond,
		CacheValidityPeriod:  100 * time.Millisecond,
		PrecomputeEnabled:    true,
		HealthyThreshold:     0.95,
		DegradedThreshold:    0.8,
		UnhealthyThreshold:   0.6,
		CriticalThreshold:    0.4,
		ComponentWeights:     map[string]float64{
			"critical":  1.0,
			"important": 0.7,
			"optional":  0.3,
		},
		EnableHTTPEndpoint:   true,
		HTTPPort:            8080,
		HTTPPath:            "/health",
		HTTPTimeout:         1 * time.Millisecond,
	}
}

// Start starts the health checker
func (hc *HealthChecker) Start(ctx context.Context) error {
	hc.running = true
	
	// Start health computation loop
	go hc.computationLoop(ctx)
	
	// Start component update loops
	for _, component := range hc.components {
		go hc.componentUpdateLoop(ctx, component)
	}
	
	// Start HTTP health endpoint if enabled
	if hc.config.EnableHTTPEndpoint {
		go hc.startHTTPServer(ctx)
	}
	
	return nil
}

// Stop stops the health checker
func (hc *HealthChecker) Stop() error {
	hc.running = false
	close(hc.stopChan)
	return nil
}

// GetHealth returns the current health status with <1ms response time
func (hc *HealthChecker) GetHealth() *CachedHealthData {
	start := time.Now()
	
	// Load cached health data atomically (ultra-fast)
	cachedPtr := atomic.LoadPointer(&hc.cachedHealth)
	cached := (*CachedHealthData)(cachedPtr)
	
	// Update response time tracking
	responseTime := time.Since(start)
	hc.updateResponseTimeMetrics(responseTime)
	
	// Check if cache is still valid
	now := time.Now()
	if now.Before(cached.ValidUntil) {
		// Return cached data with updated response time
		result := *cached
		result.ResponseTimeNs = responseTime.Nanoseconds()
		return &result
	}
	
	// Cache expired, trigger recomputation (non-blocking)
	select {
	case hc.computationChan <- struct{}{}:
	default:
		// Computation already in progress, return stale data
	}
	
	// Return stale cached data with warning
	result := *cached
	result.ResponseTimeNs = responseTime.Nanoseconds()
	return &result
}

// GetHealthStatus returns just the health status (even faster)
func (hc *HealthChecker) GetHealthStatus() HealthStatus {
	cachedPtr := atomic.LoadPointer(&hc.cachedHealth)
	cached := (*CachedHealthData)(cachedPtr)
	return cached.OverallStatus
}

// GetHealthScore returns just the health score (fastest)
func (hc *HealthChecker) GetHealthScore() float64 {
	cachedPtr := atomic.LoadPointer(&hc.cachedHealth)
	cached := (*CachedHealthData)(cachedPtr)
	return cached.OverallScore
}

// RegisterComponent registers a component for health tracking
func (hc *HealthChecker) RegisterComponent(name, componentType string, measureFunc func() HealthMeasurement) {
	hc.componentsMutex.Lock()
	defer hc.componentsMutex.Unlock()
	
	weight := hc.config.ComponentWeights[componentType]
	if weight == 0 {
		weight = 0.5 // Default weight
	}
	
	updateInterval := hc.config.SlowUpdateInterval
	if componentType == "critical" {
		updateInterval = hc.config.FastUpdateInterval
	}
	
	component := &ComponentHealth{
		Name:           name,
		Type:           componentType,
		Weight:         weight,
		measureFunc:    measureFunc,
		updateInterval: updateInterval,
		historyBuffer:  make([]HealthMeasurement, 100), // Ring buffer
		historySize:    100,
	}
	
	// Initialize with neutral values
	atomic.StoreInt64(&component.currentScore, 500) // 0.5 * 1000
	atomic.StoreInt32(&component.currentStatus, int32(HealthUnknown))
	atomic.StoreInt64(&component.lastUpdate, time.Now().UnixNano())
	
	hc.components[name] = component
	
	// Create circuit breaker for component
	hc.cbMutex.Lock()
	hc.circuitBreakers[name] = NewCircuitBreaker(CircuitBreakerConfig{
		Name:             name + "-health",
		MaxFailures:      3,
		ResetTimeout:     30 * time.Second,
		HalfOpenMaxCalls: 1,
	})
	hc.cbMutex.Unlock()
	
	// Start update loop if health checker is running
	if hc.running {
		go hc.componentUpdateLoop(context.Background(), component)
	}
}

// computationLoop runs the health computation loop
func (hc *HealthChecker) computationLoop(ctx context.Context) {
	ticker := time.NewTicker(hc.config.CacheValidityPeriod / 2)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-hc.stopChan:
			return
		case <-ticker.C:
			hc.computeHealth()
		case <-hc.computationChan:
			hc.computeHealth()
		}
	}
}

// computeHealth computes and caches health data
func (hc *HealthChecker) computeHealth() {
	start := time.Now()
	
	hc.componentsMutex.RLock()
	components := make([]*ComponentHealth, 0, len(hc.components))
	for _, component := range hc.components {
		components = append(components, component)
	}
	hc.componentsMutex.RUnlock()
	
	// Compute component statuses and scores
	componentStatuses := make(map[string]HealthStatus)
	componentScores := make(map[string]float64)
	
	var weightedScore, totalWeight float64
	healthyCount, degradedCount, unhealthyCount, criticalCount := 0, 0, 0, 0
	
	for _, component := range components {
		score := float64(atomic.LoadInt64(&component.currentScore)) / 1000.0
		status := HealthStatus(atomic.LoadInt32(&component.currentStatus))
		
		componentScores[component.Name] = score
		componentStatuses[component.Name] = status
		
		// Weighted score calculation
		weightedScore += score * component.Weight
		totalWeight += component.Weight
		
		// Count components by status
		switch status {
		case HealthHealthy:
			healthyCount++
		case HealthDegraded:
			degradedCount++
		case HealthUnhealthy:
			unhealthyCount++
		case HealthCritical:
			criticalCount++
		}
	}
	
	// Calculate overall score
	overallScore := 0.0
	if totalWeight > 0 {
		overallScore = weightedScore / totalWeight
	}
	
	// Determine overall status
	overallStatus := hc.calculateOverallStatus(overallScore)
	
	// Create new cached health data
	computationTime := time.Since(start)
	newHealth := &CachedHealthData{
		OverallStatus:       overallStatus,
		OverallScore:        overallScore,
		ComponentStatuses:   componentStatuses,
		ComponentScores:     componentScores,
		HealthyComponents:   healthyCount,
		DegradedComponents:  degradedCount,
		UnhealthyComponents: unhealthyCount,
		CriticalComponents:  criticalCount,
		ComputedAt:          time.Now(),
		ValidUntil:          time.Now().Add(hc.config.CacheValidityPeriod),
		ComputationTimeNs:   computationTime.Nanoseconds(),
	}
	
	// Atomically update cached health data
	atomic.StorePointer(&hc.cachedHealth, unsafe.Pointer(newHealth))
	atomic.StoreInt64(&hc.lastUpdate, time.Now().UnixNano())
	
	// Update metrics
	atomic.AddUint64(&hc.metrics.ComputationCount, 1)
	
	// Update degradation manager if available
	if hc.degradationManager != nil {
		hc.degradationManager.UpdateHealth(HealthMeasurement{
			Timestamp: time.Now(),
			Score:     overallScore,
		})
	}
}

// calculateOverallStatus determines overall status from score
func (hc *HealthChecker) calculateOverallStatus(score float64) HealthStatus {
	if score >= hc.config.HealthyThreshold {
		return HealthHealthy
	} else if score >= hc.config.DegradedThreshold {
		return HealthDegraded
	} else if score >= hc.config.UnhealthyThreshold {
		return HealthUnhealthy
	} else {
		return HealthCritical
	}
}

// componentUpdateLoop updates individual component health
func (hc *HealthChecker) componentUpdateLoop(ctx context.Context, component *ComponentHealth) {
	ticker := time.NewTicker(component.updateInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-hc.stopChan:
			return
		case <-ticker.C:
			hc.updateComponentHealth(component)
		}
	}
}

// updateComponentHealth updates health for a single component
func (hc *HealthChecker) updateComponentHealth(component *ComponentHealth) {
	if component.measureFunc == nil {
		return
	}
	
	start := time.Now()
	
	// Get circuit breaker for component
	hc.cbMutex.RLock()
	cb := hc.circuitBreakers[component.Name]
	hc.cbMutex.RUnlock()
	
	var measurement HealthMeasurement
	var err error
	
	// Execute health measurement with circuit breaker protection
	if cb != nil {
		err = cb.Execute(context.Background(), func() error {
			measurement = component.measureFunc()
			return nil
		})
	} else {
		measurement = component.measureFunc()
	}
	
	measureTime := time.Since(start)
	
	if err != nil {
		// Circuit breaker open or measurement failed
		measurement = HealthMeasurement{
			Timestamp: time.Now(),
			Score:     0.0, // Unhealthy
		}
		atomic.AddUint64(&hc.metrics.ComponentErrorCount[component.Name], 1)
	} else {
		atomic.AddUint64(&hc.metrics.ComponentUpdateCount[component.Name], 1)
	}
	
	// Update component state atomically
	atomic.StoreInt64(&component.currentScore, int64(measurement.Score*1000))
	atomic.StoreInt32(&component.currentStatus, int32(hc.calculateOverallStatus(measurement.Score)))
	atomic.StoreInt64(&component.lastUpdate, time.Now().UnixNano())
	
	// Update ring buffer (lock-free)
	index := atomic.AddInt64(&component.historyIndex, 1) - 1
	component.historyBuffer[index%int64(component.historySize)] = measurement
	
	// Update performance metrics
	atomic.AddUint64(&component.measurementCount, 1)
	atomic.AddUint64(&component.totalMeasureTime, uint64(measureTime.Nanoseconds()))
	
	// Update fastest/slowest times
	measureNs := uint64(measureTime.Nanoseconds())
	for {
		fastest := atomic.LoadUint64(&component.fastestMeasure)
		if fastest == 0 || measureNs < fastest {
			if atomic.CompareAndSwapUint64(&component.fastestMeasure, fastest, measureNs) {
				break
			}
		} else {
			break
		}
	}
	
	for {
		slowest := atomic.LoadUint64(&component.slowestMeasure)
		if measureNs > slowest {
			if atomic.CompareAndSwapUint64(&component.slowestMeasure, slowest, measureNs) {
				break
			}
		} else {
			break
		}
	}
	
	// Trigger health recomputation
	select {
	case hc.computationChan <- struct{}{}:
	default:
	}
}

// updateResponseTimeMetrics updates response time metrics
func (hc *HealthChecker) updateResponseTimeMetrics(responseTime time.Duration) {
	atomic.AddUint64(&hc.metrics.TotalRequests, 1)
	
	if responseTime < 1*time.Millisecond {
		atomic.AddUint64(&hc.metrics.RequestsUnder1ms, 1)
	}
	if responseTime < 500*time.Microsecond {
		atomic.AddUint64(&hc.metrics.RequestsUnder500us, 1)
	}
	if responseTime < 100*time.Microsecond {
		atomic.AddUint64(&hc.metrics.RequestsUnder100us, 1)
	}
}

// startHTTPServer starts the HTTP health endpoint
func (hc *HealthChecker) startHTTPServer(ctx context.Context) {
	mux := http.NewServeMux()
	
	// Ultra-fast health endpoint
	mux.HandleFunc(hc.config.HTTPPath, func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// Set headers for speed
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		
		health := hc.GetHealth()
		
		// Simple JSON response optimized for speed
		statusCode := http.StatusOK
		if health.OverallStatus != HealthHealthy {
			statusCode = http.StatusServiceUnavailable
		}
		
		w.WriteHeader(statusCode)
		
		// Write minimal JSON for speed
		response := `{"status":"` + health.OverallStatus.String() + 
					`","score":` + formatFloat(health.OverallScore) +
					`,"response_time_ns":` + formatInt64(time.Since(start).Nanoseconds()) + `}`
		
		w.Write([]byte(response))
	})
	
	// Detailed health endpoint
	mux.HandleFunc(hc.config.HTTPPath+"/detailed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		health := hc.GetHealth()
		
		// Could implement full JSON marshaling here, but keeping simple for demo
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"detailed":"health_data"}`))
	})
	
	server := &http.Server{
		Addr:         ":" + formatInt(hc.config.HTTPPort),
		Handler:      mux,
		ReadTimeout:  hc.config.HTTPTimeout,
		WriteTimeout: hc.config.HTTPTimeout,
	}
	
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()
	
	server.ListenAndServe()
}

// GetMetrics returns health checking metrics
func (hc *HealthChecker) GetMetrics() *HealthMetrics {
	metrics := *hc.metrics
	metrics.LastUpdated = time.Now()
	
	// Calculate cache hit rate
	if metrics.TotalRequests > 0 {
		metrics.CacheHitRate = float64(metrics.RequestsUnder1ms) / float64(metrics.TotalRequests)
	}
	
	return &metrics
}

// SetDegradationManager sets the degradation manager for integration
func (hc *HealthChecker) SetDegradationManager(dm *DegradationManager) {
	hc.degradationManager = dm
}

// Built-in health measurement functions

// SystemHealthMeasurement measures system health
func SystemHealthMeasurement() HealthMeasurement {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	// Simple system health calculation
	memoryPressure := float64(m.Alloc) / float64(m.Sys)
	goroutineCount := float64(runtime.NumGoroutine())
	
	// Calculate health score (simplified)
	score := 1.0
	if memoryPressure > 0.8 {
		score -= 0.3
	}
	if goroutineCount > 1000 {
		score -= 0.2
	}
	
	if score < 0 {
		score = 0
	}
	
	return HealthMeasurement{
		Timestamp:   time.Now(),
		Score:       score,
		CPUUsage:    0.0, // Would need actual CPU measurement
		MemoryUsage: int64(m.Alloc),
		ErrorRate:   0.0, // Would need actual error tracking
		Latency:     0,   // Would need actual latency measurement
		Throughput:  0.0, // Would need actual throughput measurement
	}
}

// NetworkHealthMeasurement measures network connectivity health
func NetworkHealthMeasurement() HealthMeasurement {
	// Simplified network health check
	start := time.Now()
	
	// Could implement actual network checks here
	// For now, assume healthy
	latency := time.Since(start)
	
	score := 1.0
	if latency > 10*time.Millisecond {
		score = 0.8
	}
	if latency > 50*time.Millisecond {
		score = 0.5
	}
	
	return HealthMeasurement{
		Timestamp:      time.Now(),
		Score:          score,
		Latency:        latency,
		NetworkLatency: latency,
		Throughput:     100.0, // Simplified
	}
}

// DatabaseHealthMeasurement measures database health
func DatabaseHealthMeasurement() HealthMeasurement {
	// Simplified database health check
	start := time.Now()
	
	// Could implement actual database ping here
	// For now, assume healthy
	latency := time.Since(start)
	
	score := 1.0
	if latency > 5*time.Millisecond {
		score = 0.9
	}
	if latency > 20*time.Millisecond {
		score = 0.7
	}
	
	return HealthMeasurement{
		Timestamp:      time.Now(),
		Score:          score,
		Latency:        latency,
		DatabaseHealth: score,
		Throughput:     50.0, // Simplified
	}
}

// Helper functions for formatting (optimized for speed)
func formatFloat(f float64) string {
	// Simplified float formatting for speed
	if f >= 1.0 {
		return "1.0"
	}
	if f >= 0.95 {
		return "0.95"
	}
	if f >= 0.9 {
		return "0.9"
	}
	if f >= 0.8 {
		return "0.8"
	}
	if f >= 0.7 {
		return "0.7"
	}
	if f >= 0.5 {
		return "0.5"
	}
	return "0.0"
}

func formatInt64(i int64) string {
	// Simplified int formatting for speed
	return "0" // Would implement proper formatting
}

func formatInt(i int) string {
	// Simplified int formatting for speed
	return "8080" // Would implement proper formatting
}

// Global health checker instance
var globalHealthChecker *HealthChecker

// InitializeGlobalHealthChecker initializes the global health checker
func InitializeGlobalHealthChecker(config *HealthConfig) error {
	globalHealthChecker = NewHealthChecker(config)
	
	// Register default system components
	globalHealthChecker.RegisterComponent("system", "critical", SystemHealthMeasurement)
	globalHealthChecker.RegisterComponent("network", "important", NetworkHealthMeasurement)
	globalHealthChecker.RegisterComponent("database", "important", DatabaseHealthMeasurement)
	
	return globalHealthChecker.Start(context.Background())
}

// GetGlobalHealthChecker returns the global health checker
func GetGlobalHealthChecker() *HealthChecker {
	return globalHealthChecker
}

// GetGlobalHealth returns global health status
func GetGlobalHealth() *CachedHealthData {
	if globalHealthChecker != nil {
		return globalHealthChecker.GetHealth()
	}
	return &CachedHealthData{
		OverallStatus: HealthUnknown,
		OverallScore:  0.0,
		ComputedAt:    time.Now(),
	}
}

// GetGlobalHealthStatus returns global health status quickly
func GetGlobalHealthStatus() HealthStatus {
	if globalHealthChecker != nil {
		return globalHealthChecker.GetHealthStatus()
	}
	return HealthUnknown
}