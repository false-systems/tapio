package resilience

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// LoadShedder implements intelligent load shedding
type LoadShedder struct {
	name          string
	config        *LoadSheddingConfig
	
	// Metrics
	totalRequests atomic.Uint64
	shedRequests  atomic.Uint64
	acceptedRequests atomic.Uint64
	
	// State
	currentLoad   atomic.Uint64
	cpuUsage      atomic.Uint64
	memoryUsage   atomic.Uint64
	latencyP99    atomic.Uint64
	errorRate     atomic.Uint64
	
	// Adaptive thresholds
	adaptiveThreshold float64
	lastAdaptTime     time.Time
	
	// Priority handling
	priorityQueues map[Priority]*PriorityQueue
	
	mutex sync.RWMutex
}

// LoadSheddingConfig configures load shedding
type LoadSheddingConfig struct {
	// Basic thresholds
	MaxLoad           uint64
	CPUThreshold      float64  // 0-100
	MemoryThreshold   float64  // 0-100
	LatencyThreshold  time.Duration
	ErrorRateThreshold float64 // 0-1
	
	// Adaptive settings
	EnableAdaptive    bool
	AdaptiveWindow    time.Duration
	AdaptiveAlpha     float64 // Learning rate
	
	// Priority settings
	EnablePriority    bool
	PriorityLevels    []Priority
	
	// Shedding strategies
	Strategy          SheddingStrategy
	GradualSteps      int
	RandomSeedTime    bool
}

// Priority represents request priority
type Priority int

const (
	PriorityCritical Priority = iota
	PriorityHigh
	PriorityNormal
	PriorityLow
	PriorityBulk
)

// SheddingStrategy defines how to shed load
type SheddingStrategy string

const (
	StrategyRandom     SheddingStrategy = "random"
	StrategyPriority   SheddingStrategy = "priority"
	StrategyAdaptive   SheddingStrategy = "adaptive"
	StrategyGradual    SheddingStrategy = "gradual"
	StrategyCircuitBreaker SheddingStrategy = "circuit_breaker"
)

// PriorityQueue manages requests by priority
type PriorityQueue struct {
	priority    Priority
	capacity    int
	current     int
	shedRate    float64
	mutex       sync.Mutex
}

// Request represents a request to be processed
type Request struct {
	ID       string
	Priority Priority
	Size     int
	Metadata map[string]interface{}
}

// DefaultLoadSheddingConfig returns default configuration
func DefaultLoadSheddingConfig() *LoadSheddingConfig {
	return &LoadSheddingConfig{
		MaxLoad:            10000,
		CPUThreshold:       80.0,
		MemoryThreshold:    85.0,
		LatencyThreshold:   1 * time.Second,
		ErrorRateThreshold: 0.05,
		EnableAdaptive:     true,
		AdaptiveWindow:     1 * time.Minute,
		AdaptiveAlpha:      0.1,
		EnablePriority:     true,
		PriorityLevels:     []Priority{PriorityCritical, PriorityHigh, PriorityNormal, PriorityLow, PriorityBulk},
		Strategy:           StrategyAdaptive,
		GradualSteps:       10,
		RandomSeedTime:     true,
	}
}

// NewLoadShedder creates a new load shedder
func NewLoadShedder(name string, config *LoadSheddingConfig) *LoadShedder {
	if config == nil {
		config = DefaultLoadSheddingConfig()
	}
	
	ls := &LoadShedder{
		name:              name,
		config:            config,
		adaptiveThreshold: 1.0,
		lastAdaptTime:     time.Now(),
		priorityQueues:    make(map[Priority]*PriorityQueue),
	}
	
	// Initialize priority queues
	if config.EnablePriority {
		for _, priority := range config.PriorityLevels {
			ls.priorityQueues[priority] = &PriorityQueue{
				priority: priority,
				capacity: int(config.MaxLoad) / len(config.PriorityLevels),
			}
		}
	}
	
	// Seed random if needed
	if config.RandomSeedTime {
		rand.Seed(time.Now().UnixNano())
	}
	
	return ls
}

// ShouldAccept determines if a request should be accepted
func (ls *LoadShedder) ShouldAccept(ctx context.Context, request *Request) bool {
	ls.totalRequests.Add(1)
	
	// Check if we should shed based on strategy
	shouldShed := ls.shouldShed(request)
	
	if shouldShed {
		ls.shedRequests.Add(1)
		return false
	}
	
	ls.acceptedRequests.Add(1)
	return true
}

// shouldShed determines if load should be shed
func (ls *LoadShedder) shouldShed(request *Request) bool {
	switch ls.config.Strategy {
	case StrategyRandom:
		return ls.randomShedding()
	case StrategyPriority:
		return ls.priorityShedding(request)
	case StrategyAdaptive:
		return ls.adaptiveShedding(request)
	case StrategyGradual:
		return ls.gradualShedding()
	case StrategyCircuitBreaker:
		return ls.circuitBreakerShedding()
	default:
		return ls.adaptiveShedding(request)
	}
}

// randomShedding implements random load shedding
func (ls *LoadShedder) randomShedding() bool {
	load := ls.getCurrentLoadFactor()
	if load < 0.8 {
		return false
	}
	
	// Shed probability increases with load
	shedProbability := (load - 0.8) / 0.2
	return rand.Float64() < shedProbability
}

// priorityShedding implements priority-based shedding
func (ls *LoadShedder) priorityShedding(request *Request) bool {
	if !ls.config.EnablePriority {
		return ls.randomShedding()
	}
	
	load := ls.getCurrentLoadFactor()
	
	// Never shed critical requests unless extreme load
	if request.Priority == PriorityCritical && load < 0.95 {
		return false
	}
	
	// Shed based on priority thresholds
	thresholds := map[Priority]float64{
		PriorityCritical: 0.95,
		PriorityHigh:     0.85,
		PriorityNormal:   0.75,
		PriorityLow:      0.65,
		PriorityBulk:     0.55,
	}
	
	threshold, exists := thresholds[request.Priority]
	if !exists {
		threshold = 0.75
	}
	
	return load > threshold
}

// adaptiveShedding implements adaptive load shedding
func (ls *LoadShedder) adaptiveShedding(request *Request) bool {
	// Update adaptive threshold if needed
	if ls.config.EnableAdaptive && time.Since(ls.lastAdaptTime) > ls.config.AdaptiveWindow {
		ls.updateAdaptiveThreshold()
	}
	
	load := ls.getCurrentLoadFactor()
	adaptedLoad := load * ls.adaptiveThreshold
	
	// Combine with priority if enabled
	if ls.config.EnablePriority {
		priorityFactor := ls.getPriorityFactor(request.Priority)
		adaptedLoad *= priorityFactor
	}
	
	// Use sigmoid function for smooth shedding
	shedProbability := 1 / (1 + math.Exp(-10*(adaptedLoad-0.8)))
	
	return rand.Float64() < shedProbability
}

// gradualShedding implements gradual load shedding
func (ls *LoadShedder) gradualShedding() bool {
	load := ls.getCurrentLoadFactor()
	
	// Determine shedding step
	step := int(load * float64(ls.config.GradualSteps))
	if step >= ls.config.GradualSteps {
		step = ls.config.GradualSteps - 1
	}
	
	// Calculate shed rate for this step
	shedRate := float64(step) / float64(ls.config.GradualSteps)
	
	return rand.Float64() < shedRate
}

// circuitBreakerShedding implements circuit breaker style shedding
func (ls *LoadShedder) circuitBreakerShedding() bool {
	// Check multiple signals
	cpuOverloaded := float64(ls.cpuUsage.Load()) > ls.config.CPUThreshold
	memoryOverloaded := float64(ls.memoryUsage.Load()) > ls.config.MemoryThreshold
	latencyHigh := time.Duration(ls.latencyP99.Load()) > ls.config.LatencyThreshold
	errorRateHigh := float64(ls.errorRate.Load())/100 > ls.config.ErrorRateThreshold
	
	// If any signal is critical, shed all non-critical traffic
	if cpuOverloaded || memoryOverloaded || latencyHigh || errorRateHigh {
		return true
	}
	
	return false
}

// getCurrentLoadFactor calculates current load factor (0-1)
func (ls *LoadShedder) getCurrentLoadFactor() float64 {
	// Combine multiple signals
	currentRequests := float64(ls.currentLoad.Load())
	maxLoad := float64(ls.config.MaxLoad)
	loadFactor := currentRequests / maxLoad
	
	cpuFactor := float64(ls.cpuUsage.Load()) / 100.0
	memoryFactor := float64(ls.memoryUsage.Load()) / 100.0
	
	// Weighted average
	return 0.4*loadFactor + 0.3*cpuFactor + 0.3*memoryFactor
}

// getPriorityFactor returns priority adjustment factor
func (ls *LoadShedder) getPriorityFactor(priority Priority) float64 {
	factors := map[Priority]float64{
		PriorityCritical: 0.5,  // Reduce load factor by 50%
		PriorityHigh:     0.7,
		PriorityNormal:   1.0,
		PriorityLow:      1.3,
		PriorityBulk:     1.5,
	}
	
	factor, exists := factors[priority]
	if !exists {
		return 1.0
	}
	return factor
}

// updateAdaptiveThreshold updates the adaptive threshold
func (ls *LoadShedder) updateAdaptiveThreshold() {
	ls.mutex.Lock()
	defer ls.mutex.Unlock()
	
	// Calculate performance metrics
	totalReqs := ls.totalRequests.Load()
	shedReqs := ls.shedRequests.Load()
	
	if totalReqs == 0 {
		return
	}
	
	shedRate := float64(shedReqs) / float64(totalReqs)
	errorRate := float64(ls.errorRate.Load()) / 100.0
	
	// Adjust threshold based on performance
	var adjustment float64
	if errorRate > ls.config.ErrorRateThreshold {
		// Increase shedding
		adjustment = -ls.config.AdaptiveAlpha
	} else if shedRate > 0.1 && errorRate < ls.config.ErrorRateThreshold*0.5 {
		// Decrease shedding
		adjustment = ls.config.AdaptiveAlpha
	}
	
	ls.adaptiveThreshold += adjustment
	
	// Clamp between 0.5 and 1.5
	if ls.adaptiveThreshold < 0.5 {
		ls.adaptiveThreshold = 0.5
	} else if ls.adaptiveThreshold > 1.5 {
		ls.adaptiveThreshold = 1.5
	}
	
	ls.lastAdaptTime = time.Now()
	
	// Reset counters
	ls.totalRequests.Store(0)
	ls.shedRequests.Store(0)
}

// UpdateMetrics updates system metrics
func (ls *LoadShedder) UpdateMetrics(metrics SystemMetrics) {
	ls.currentLoad.Store(metrics.CurrentLoad)
	ls.cpuUsage.Store(uint64(metrics.CPUUsage))
	ls.memoryUsage.Store(uint64(metrics.MemoryUsage))
	ls.latencyP99.Store(uint64(metrics.LatencyP99))
	ls.errorRate.Store(uint64(metrics.ErrorRate * 100))
}

// GetMetrics returns load shedder metrics
func (ls *LoadShedder) GetMetrics() LoadShedderMetrics {
	total := ls.totalRequests.Load()
	shed := ls.shedRequests.Load()
	accepted := ls.acceptedRequests.Load()
	
	var shedRate float64
	if total > 0 {
		shedRate = float64(shed) / float64(total)
	}
	
	return LoadShedderMetrics{
		Name:              ls.name,
		TotalRequests:     total,
		ShedRequests:      shed,
		AcceptedRequests:  accepted,
		ShedRate:          shedRate,
		CurrentLoad:       ls.currentLoad.Load(),
		CPUUsage:          float64(ls.cpuUsage.Load()),
		MemoryUsage:       float64(ls.memoryUsage.Load()),
		AdaptiveThreshold: ls.adaptiveThreshold,
	}
}

// Reset resets the load shedder metrics
func (ls *LoadShedder) Reset() {
	ls.totalRequests.Store(0)
	ls.shedRequests.Store(0)
	ls.acceptedRequests.Store(0)
	ls.adaptiveThreshold = 1.0
	ls.lastAdaptTime = time.Now()
}

// LoadShedderMetrics contains load shedder metrics
type LoadShedderMetrics struct {
	Name              string
	TotalRequests     uint64
	ShedRequests      uint64
	AcceptedRequests  uint64
	ShedRate          float64
	CurrentLoad       uint64
	CPUUsage          float64
	MemoryUsage       float64
	AdaptiveThreshold float64
}

// SystemMetrics represents system metrics for load shedding decisions
type SystemMetrics struct {
	CurrentLoad  uint64
	CPUUsage     float64
	MemoryUsage  float64
	LatencyP99   time.Duration
	ErrorRate    float64
}

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	name         string
	rate         int
	burst        int
	tokens       float64
	maxTokens    float64
	lastRefill   time.Time
	
	// Metrics
	totalRequests atomic.Uint64
	allowedRequests atomic.Uint64
	deniedRequests atomic.Uint64
	
	mutex sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(name string, rate int, burst int) *RateLimiter {
	return &RateLimiter{
		name:      name,
		rate:      rate,
		burst:     burst,
		tokens:    float64(burst),
		maxTokens: float64(burst),
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed
func (rl *RateLimiter) Allow() bool {
	return rl.AllowN(1)
}

// AllowN checks if n requests are allowed
func (rl *RateLimiter) AllowN(n int) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	rl.totalRequests.Add(1)
	
	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.tokens += elapsed * float64(rl.rate)
	if rl.tokens > rl.maxTokens {
		rl.tokens = rl.maxTokens
	}
	rl.lastRefill = now
	
	// Check if we have enough tokens
	if rl.tokens >= float64(n) {
		rl.tokens -= float64(n)
		rl.allowedRequests.Add(1)
		return true
	}
	
	rl.deniedRequests.Add(1)
	return false
}

// GetMetrics returns rate limiter metrics
func (rl *RateLimiter) GetMetrics() RateLimiterMetrics {
	return RateLimiterMetrics{
		Name:            rl.name,
		Rate:            rl.rate,
		Burst:           rl.burst,
		TotalRequests:   rl.totalRequests.Load(),
		AllowedRequests: rl.allowedRequests.Load(),
		DeniedRequests:  rl.deniedRequests.Load(),
	}
}

// RateLimiterMetrics contains rate limiter metrics
type RateLimiterMetrics struct {
	Name            string
	Rate            int
	Burst           int
	TotalRequests   uint64
	AllowedRequests uint64
	DeniedRequests  uint64
}