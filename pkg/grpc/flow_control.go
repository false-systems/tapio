package grpc

import (
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
)

// FlowController manages backpressure and flow control for the server
type FlowController struct {
	config ServerConfig
	
	// Global state
	globalLoad          float64
	globalEventsPerSec  uint64
	globalMemoryUsage   uint64
	backpressureActive  bool
	
	// Per-connection state
	connectionStates map[string]*ConnectionFlowState
	mu              sync.RWMutex
	
	// Rate limiting
	globalRateLimiter *RateLimiter
	
	// Statistics
	throttledRequests uint64
	totalRequests     uint64
	
	// Control parameters
	lastUpdate time.Time
}

// ConnectionFlowState tracks flow control state for a connection
type ConnectionFlowState struct {
	connectionID      string
	currentRate       uint32
	allowedRate       uint32
	bufferUtilization float32
	memoryPressure    MemoryPressure
	lastUpdate        time.Time
	violations        uint32
	
	// Rate limiter for this connection
	rateLimiter *RateLimiter
}

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	rate       float64 // tokens per second
	burst      int64   // maximum burst size
	tokens     float64 // current tokens
	lastUpdate time.Time
	mu         sync.Mutex
}

// FlowControlStats provides flow control statistics
type FlowControlStats struct {
	GlobalLoad         float64
	GlobalEventsPerSec uint64
	BackpressureActive bool
	ThrottledRequests  uint64
	TotalRequests      uint64
	ConnectionCount    int
}

// NewFlowController creates a new flow controller
func NewFlowController(config ServerConfig) *FlowController {
	return &FlowController{
		config:           config,
		connectionStates: make(map[string]*ConnectionFlowState),
		globalRateLimiter: NewRateLimiter(float64(config.DefaultEventsPerSec*100), int64(config.DefaultEventsPerSec*2)),
		lastUpdate:       time.Now(),
	}
}

// NewRateLimiter creates a new token bucket rate limiter
func NewRateLimiter(rate float64, burst int64) *RateLimiter {
	return &RateLimiter{
		rate:       rate,
		burst:      burst,
		tokens:     float64(burst),
		lastUpdate: time.Now(),
	}
}

// Allow checks if a request is allowed under rate limiting
func (rl *RateLimiter) Allow() bool {
	return rl.AllowN(1)
}

// AllowN checks if N requests are allowed under rate limiting
func (rl *RateLimiter) AllowN(n int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	now := time.Now()
	elapsed := now.Sub(rl.lastUpdate).Seconds()
	
	// Add tokens based on elapsed time
	rl.tokens += elapsed * rl.rate
	if rl.tokens > float64(rl.burst) {
		rl.tokens = float64(rl.burst)
	}
	
	rl.lastUpdate = now
	
	// Check if we have enough tokens
	if rl.tokens >= float64(n) {
		rl.tokens -= float64(n)
		return true
	}
	
	return false
}

// SetRate updates the rate limit
func (rl *RateLimiter) SetRate(rate float64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.rate = rate
}

// ShouldThrottle checks if a connection should be throttled
func (fc *FlowController) ShouldThrottle(conn *Connection) bool {
	atomic.AddUint64(&fc.totalRequests, 1)
	
	// Check global rate limit
	if !fc.globalRateLimiter.Allow() {
		atomic.AddUint64(&fc.throttledRequests, 1)
		return true
	}
	
	fc.mu.RLock()
	state, exists := fc.connectionStates[conn.ID]
	fc.mu.RUnlock()
	
	if !exists {
		// Create new state for connection
		state = &ConnectionFlowState{
			connectionID: conn.ID,
			allowedRate:  fc.config.DefaultEventsPerSec,
			rateLimiter:  NewRateLimiter(float64(fc.config.DefaultEventsPerSec), int64(fc.config.DefaultEventsPerSec*2)),
			lastUpdate:   time.Now(),
		}
		
		fc.mu.Lock()
		fc.connectionStates[conn.ID] = state
		fc.mu.Unlock()
	}
	
	// Check connection-specific rate limit
	if !state.rateLimiter.Allow() {
		atomic.AddUint64(&fc.throttledRequests, 1)
		atomic.AddUint32(&state.violations, 1)
		return true
	}
	
	// Check backpressure conditions
	if fc.shouldApplyBackpressure(conn, state) {
		atomic.AddUint64(&fc.throttledRequests, 1)
		return true
	}
	
	return false
}

// shouldApplyBackpressure determines if backpressure should be applied
func (fc *FlowController) shouldApplyBackpressure(conn *Connection, state *ConnectionFlowState) bool {
	// Check global memory usage
	if atomic.LoadUint64(&fc.globalMemoryUsage) > fc.config.MaxMemoryUsage {
		return true
	}
	
	// Check global load
	if fc.globalLoad > fc.config.BackpressureThreshold {
		return true
	}
	
	// Check connection-specific memory pressure
	if conn.GetMemoryPressure() >= MemoryPressure_MEMORY_PRESSURE_HIGH {
		return true
	}
	
	// Check buffer utilization
	if conn.GetBufferUtilization() > float32(fc.config.BackpressureThreshold) {
		return true
	}
	
	return false
}

// UpdateCollectorState updates flow control state for a collector
func (fc *FlowController) UpdateCollectorState(conn *Connection) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	
	state, exists := fc.connectionStates[conn.ID]
	if !exists {
		state = &ConnectionFlowState{
			connectionID: conn.ID,
			allowedRate:  fc.config.DefaultEventsPerSec,
			rateLimiter:  NewRateLimiter(float64(fc.config.DefaultEventsPerSec), int64(fc.config.DefaultEventsPerSec*2)),
		}
		fc.connectionStates[conn.ID] = state
	}
	
	// Update state from connection
	state.currentRate = conn.GetRequestedRate()
	state.bufferUtilization = conn.GetBufferUtilization()
	state.memoryPressure = conn.GetMemoryPressure()
	state.lastUpdate = time.Now()
	
	// Adjust allowed rate based on conditions
	newAllowedRate := fc.calculateAllowedRate(conn, state)
	if newAllowedRate != state.allowedRate {
		state.allowedRate = newAllowedRate
		state.rateLimiter.SetRate(float64(newAllowedRate))
	}
}

// calculateAllowedRate calculates the allowed rate for a connection
func (fc *FlowController) calculateAllowedRate(conn *Connection, state *ConnectionFlowState) uint32 {
	baseRate := fc.config.DefaultEventsPerSec
	
	// Reduce rate based on global load
	loadFactor := 1.0 - fc.globalLoad
	if loadFactor < 0.1 {
		loadFactor = 0.1 // Minimum 10% of base rate
	}
	
	// Reduce rate based on memory pressure
	memoryFactor := 1.0
	switch conn.GetMemoryPressure() {
	case MemoryPressure_MEMORY_PRESSURE_LOW:
		memoryFactor = 0.9
	case MemoryPressure_MEMORY_PRESSURE_MEDIUM:
		memoryFactor = 0.7
	case MemoryPressure_MEMORY_PRESSURE_HIGH:
		memoryFactor = 0.5
	case MemoryPressure_MEMORY_PRESSURE_CRITICAL:
		memoryFactor = 0.1
	}
	
	// Reduce rate based on buffer utilization
	bufferFactor := 1.0
	if conn.GetBufferUtilization() > 0.8 {
		bufferFactor = 0.5
	} else if conn.GetBufferUtilization() > 0.6 {
		bufferFactor = 0.8
	}
	
	// Calculate final rate
	finalRate := float64(baseRate) * loadFactor * memoryFactor * bufferFactor
	
	// Ensure minimum rate
	if finalRate < float64(baseRate)*0.1 {
		finalRate = float64(baseRate) * 0.1
	}
	
	return uint32(finalRate)
}

// GetDirective generates a flow control directive for a connection
func (fc *FlowController) GetDirective(conn *Connection) *FlowControlDirective {
	fc.mu.RLock()
	state, exists := fc.connectionStates[conn.ID]
	fc.mu.RUnlock()
	
	if !exists {
		return nil
	}
	
	// Check if directive is needed
	if time.Since(state.lastUpdate) < 5*time.Second && state.violations == 0 {
		return nil // No updates needed
	}
	
	return &FlowControlDirective{
		MaxEventsPerSecond: state.allowedRate,
		MaxBatchSize:      fc.config.MaxBatchSize,
		BatchInterval:     durationpb.New(time.Second),
		EnableCompression: true,
		CompressionType:   CompressionType_COMPRESSION_LZ4,
		ValidDuration:     durationpb.New(time.Minute),
	}
}

// GetGlobalDirective generates a global flow control directive
func (fc *FlowController) GetGlobalDirective() *FlowControlDirective {
	if !fc.backpressureActive {
		return nil
	}
	
	return &FlowControlDirective{
		MaxEventsPerSecond: fc.config.DefaultEventsPerSec / 2, // Reduce by 50% under backpressure
		MaxBatchSize:      fc.config.MaxBatchSize / 2,
		BatchInterval:     durationpb.New(2 * time.Second),
		EnableCompression: true,
		CompressionType:   CompressionType_COMPRESSION_ZSTD, // Use better compression
		ValidDuration:     durationpb.New(30 * time.Second),
	}
}

// UpdateGlobalState updates the global flow control state
func (fc *FlowController) UpdateGlobalState(metrics *ServerMetrics) {
	stats := metrics.GetStats()
	
	// Update global metrics
	atomic.StoreUint64(&fc.globalEventsPerSec, uint64(stats.EventsPerSecond))
	fc.globalLoad = stats.Load
	
	// Determine if backpressure should be active
	shouldActivate := fc.globalLoad > fc.config.BackpressureThreshold ||
		atomic.LoadUint64(&fc.globalMemoryUsage) > fc.config.MaxMemoryUsage
	
	if shouldActivate != fc.backpressureActive {
		fc.backpressureActive = shouldActivate
		if shouldActivate {
			fc.activateGlobalBackpressure()
		} else {
			fc.deactivateGlobalBackpressure()
		}
	}
	
	fc.lastUpdate = time.Now()
}

// TriggerBackpressure manually triggers backpressure
func (fc *FlowController) TriggerBackpressure(reason string) {
	fc.backpressureActive = true
	fc.activateGlobalBackpressure()
}

// activateGlobalBackpressure reduces global rate limits
func (fc *FlowController) activateGlobalBackpressure() {
	// Reduce global rate limit by 50%
	newRate := float64(fc.config.DefaultEventsPerSec) * 0.5
	fc.globalRateLimiter.SetRate(newRate)
	
	// Reduce all connection rate limits
	fc.mu.Lock()
	for _, state := range fc.connectionStates {
		newAllowedRate := state.allowedRate / 2
		if newAllowedRate < fc.config.DefaultEventsPerSec/10 {
			newAllowedRate = fc.config.DefaultEventsPerSec / 10 // Minimum 10%
		}
		state.allowedRate = newAllowedRate
		state.rateLimiter.SetRate(float64(newAllowedRate))
	}
	fc.mu.Unlock()
}

// deactivateGlobalBackpressure restores normal rate limits
func (fc *FlowController) deactivateGlobalBackpressure() {
	// Restore global rate limit
	fc.globalRateLimiter.SetRate(float64(fc.config.DefaultEventsPerSec))
	
	// Restore connection rate limits
	fc.mu.Lock()
	for _, state := range fc.connectionStates {
		state.allowedRate = fc.config.DefaultEventsPerSec
		state.rateLimiter.SetRate(float64(fc.config.DefaultEventsPerSec))
	}
	fc.mu.Unlock()
}

// CleanupConnection removes flow control state for a connection
func (fc *FlowController) CleanupConnection(connectionID string) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	delete(fc.connectionStates, connectionID)
}

// GetStats returns flow control statistics
func (fc *FlowController) GetStats() FlowControlStats {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	
	return FlowControlStats{
		GlobalLoad:         fc.globalLoad,
		GlobalEventsPerSec: atomic.LoadUint64(&fc.globalEventsPerSec),
		BackpressureActive: fc.backpressureActive,
		ThrottledRequests:  atomic.LoadUint64(&fc.throttledRequests),
		TotalRequests:      atomic.LoadUint64(&fc.totalRequests),
		ConnectionCount:    len(fc.connectionStates),
	}
}

// SetGlobalMemoryUsage updates the global memory usage
func (fc *FlowController) SetGlobalMemoryUsage(usage uint64) {
	atomic.StoreUint64(&fc.globalMemoryUsage, usage)
}

// GetThrottleRate returns the current throttle rate (0.0 to 1.0)
func (fc *FlowController) GetThrottleRate() float64 {
	total := atomic.LoadUint64(&fc.totalRequests)
	throttled := atomic.LoadUint64(&fc.throttledRequests)
	
	if total == 0 {
		return 0.0
	}
	
	return float64(throttled) / float64(total)
}