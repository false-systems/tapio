package internal

import (
	"sync"
	"sync/atomic"
	"time"
)

// BackpressureController manages load shedding and backpressure
type BackpressureController struct {
	// Configuration
	highWatermark    float64       // Buffer utilization to start shedding
	lowWatermark     float64       // Buffer utilization to stop shedding
	shedIncrement    float64       // How much to increase shedding rate
	cooldownDuration time.Duration // Time before reducing shed rate

	// State
	shedRate     atomic.Value // float64: current shed rate (0.0-1.0)
	lastShedTime atomic.Value // time.Time
	loadLevel    atomic.Int32 // LoadLevel

	// Metrics
	mu      sync.RWMutex
	metrics BackpressureMetrics
}

// LoadLevel represents system load level
type LoadLevel int32

const (
	LoadNormal   LoadLevel = iota // Normal operation
	LoadElevated                  // Starting to see pressure
	LoadHigh                      // High pressure, shedding load
	LoadCritical                  // Critical pressure, aggressive shedding
)

// BackpressureMetrics tracks backpressure statistics
type BackpressureMetrics struct {
	EventsAccepted   uint64
	EventsShed       uint64
	LoadTransitions  uint64
	CurrentShedRate  float64
	CurrentLoadLevel string
	TimeInHighLoad   time.Duration
	LastHighLoadTime time.Time
}

// NewBackpressureController creates a new backpressure controller
func NewBackpressureController() *BackpressureController {
	bc := &BackpressureController{
		highWatermark:    70.0,        // Start shedding at 70% buffer
		lowWatermark:     50.0,        // Stop shedding at 50% buffer
		shedIncrement:    0.1,         // Increase shed rate by 10%
		cooldownDuration: time.Minute, // Wait 1 minute before reducing
	}

	bc.shedRate.Store(0.0)
	bc.lastShedTime.Store(time.Time{})

	return bc
}

// ShouldAccept determines if an event should be accepted based on load
func (bc *BackpressureController) ShouldAccept(priority EventPriority) bool {
	shedRate := bc.shedRate.Load().(float64)

	// Never shed critical events
	if priority == PriorityCritical {
		bc.recordAccepted()
		return true
	}

	// No shedding active
	if shedRate == 0.0 {
		bc.recordAccepted()
		return true
	}

	// Apply different thresholds based on priority
	threshold := 1.0 - shedRate
	if priority == PriorityHigh {
		threshold *= 1.5 // Give high priority events better chance
	}

	// Random shedding based on rate
	if randomFloat() < threshold {
		bc.recordAccepted()
		return true
	}

	bc.recordShed()
	return false
}

// UpdateLoad updates the load level based on buffer utilization
func (bc *BackpressureController) UpdateLoad(bufferUtilization float64) {
	currentLevel := bc.calculateLoadLevel(bufferUtilization)
	oldLevel := LoadLevel(bc.loadLevel.Swap(int32(currentLevel)))

	if oldLevel != currentLevel {
		bc.recordLoadTransition(oldLevel, currentLevel)
	}

	// Update shed rate based on load
	bc.updateShedRate(bufferUtilization, currentLevel)
}

// calculateLoadLevel determines load level from buffer utilization
func (bc *BackpressureController) calculateLoadLevel(utilization float64) LoadLevel {
	switch {
	case utilization >= 90.0:
		return LoadCritical
	case utilization >= bc.highWatermark:
		return LoadHigh
	case utilization >= 60.0:
		return LoadElevated
	default:
		return LoadNormal
	}
}

// updateShedRate adjusts the shed rate based on conditions
func (bc *BackpressureController) updateShedRate(utilization float64, level LoadLevel) {
	currentRate := bc.shedRate.Load().(float64)
	newRate := currentRate

	switch level {
	case LoadCritical:
		// Aggressive shedding for critical load
		newRate = 0.8 // Shed 80% of non-critical events

	case LoadHigh:
		// Increase shedding if above high watermark
		if utilization > bc.highWatermark {
			newRate = currentRate + bc.shedIncrement
			if newRate > 0.7 { // Cap at 70% shed rate for high load
				newRate = 0.7
			}
		}

	case LoadElevated:
		// Maintain current rate but prepare for potential increase
		// No change

	case LoadNormal:
		// Reduce shedding if we've been stable
		if utilization < bc.lowWatermark {
			lastShed := bc.lastShedTime.Load().(time.Time)
			if !lastShed.IsZero() && time.Since(lastShed) > bc.cooldownDuration {
				newRate = currentRate - bc.shedIncrement
				if newRate < 0 {
					newRate = 0
				}
			}
		}
	}

	if newRate != currentRate {
		bc.shedRate.Store(newRate)
		if newRate > 0 {
			bc.lastShedTime.Store(time.Now())
		}
	}
}

// GetAdaptiveTimeout returns timeout based on load
func (bc *BackpressureController) GetAdaptiveTimeout(baseTimeout time.Duration) time.Duration {
	level := LoadLevel(bc.loadLevel.Load())

	switch level {
	case LoadCritical:
		return baseTimeout / 4 // Very short timeout
	case LoadHigh:
		return baseTimeout / 2 // Reduced timeout
	case LoadElevated:
		return baseTimeout * 3 / 4 // Slightly reduced
	default:
		return baseTimeout // Normal timeout
	}
}

// recordAccepted records an accepted event
func (bc *BackpressureController) recordAccepted() {
	bc.mu.Lock()
	bc.metrics.EventsAccepted++
	bc.mu.Unlock()
}

// recordShed records a shed event
func (bc *BackpressureController) recordShed() {
	bc.mu.Lock()
	bc.metrics.EventsShed++
	bc.mu.Unlock()
}

// recordLoadTransition records load level changes
func (bc *BackpressureController) recordLoadTransition(old, new LoadLevel) {
	bc.mu.Lock()
	bc.metrics.LoadTransitions++

	// Track time in high load
	if old >= LoadHigh && new < LoadHigh && !bc.metrics.LastHighLoadTime.IsZero() {
		bc.metrics.TimeInHighLoad += time.Since(bc.metrics.LastHighLoadTime)
		bc.metrics.LastHighLoadTime = time.Time{}
	} else if old < LoadHigh && new >= LoadHigh {
		bc.metrics.LastHighLoadTime = time.Now()
	}

	bc.mu.Unlock()
}

// GetMetrics returns backpressure metrics
func (bc *BackpressureController) GetMetrics() BackpressureMetrics {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	metrics := bc.metrics
	metrics.CurrentShedRate = bc.shedRate.Load().(float64)
	metrics.CurrentLoadLevel = bc.getLoadLevelName(LoadLevel(bc.loadLevel.Load()))

	// Add current high load duration if applicable
	if !metrics.LastHighLoadTime.IsZero() {
		metrics.TimeInHighLoad += time.Since(metrics.LastHighLoadTime)
	}

	return metrics
}

// getLoadLevelName returns human-readable load level
func (bc *BackpressureController) getLoadLevelName(level LoadLevel) string {
	switch level {
	case LoadNormal:
		return "normal"
	case LoadElevated:
		return "elevated"
	case LoadHigh:
		return "high"
	case LoadCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Reset resets the backpressure controller
func (bc *BackpressureController) Reset() {
	bc.shedRate.Store(0.0)
	bc.lastShedTime.Store(time.Time{})
	bc.loadLevel.Store(int32(LoadNormal))

	bc.mu.Lock()
	bc.metrics = BackpressureMetrics{}
	bc.mu.Unlock()
}
