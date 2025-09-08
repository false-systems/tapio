//go:build linux
// +build linux

package resourcestarvation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// PatternDetector identifies recurring starvation patterns
type PatternDetector struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Pattern tracking
	patterns map[string]*Pattern // Key: "pid:eventType"
	windows  map[string]*TimeWindow

	// Metrics
	patternsDetected metric.Int64Counter
	patternDuration  metric.Float64Histogram
}

// Pattern represents a detected starvation pattern
type Pattern struct {
	Type          string
	PID           uint32
	Count         int
	FirstSeen     time.Time
	LastSeen      time.Time
	AverageWaitMS float64
	MaxWaitMS     float64
	Periodicity   time.Duration // If periodic
	Confidence    float64
}

// TimeWindow tracks events in a sliding window
type TimeWindow struct {
	Events     []time.Time
	WaitTimes  []float64
	WindowSize time.Duration
}

// NewPatternDetector creates a pattern detector
func NewPatternDetector(logger *zap.Logger, meter metric.Meter) (*PatternDetector, error) {
	patternsDetected, err := meter.Int64Counter(
		"starvation_patterns_detected_total",
		metric.WithDescription("Total patterns detected"),
	)
	if err != nil {
		return nil, err
	}

	patternDuration, err := meter.Float64Histogram(
		"starvation_pattern_duration_seconds",
		metric.WithDescription("Duration of detected patterns"),
	)
	if err != nil {
		return nil, err
	}

	return &PatternDetector{
		logger:           logger,
		patterns:         make(map[string]*Pattern),
		windows:          make(map[string]*TimeWindow),
		patternsDetected: patternsDetected,
		patternDuration:  patternDuration,
	}, nil
}

// AddEvent adds an event for pattern detection
func (d *PatternDetector) AddEvent(ctx context.Context, event *StarvationEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := d.getPatternKey(event)
	waitMS := float64(event.WaitTimeNS) / 1_000_000

	// Update or create window
	window, exists := d.windows[key]
	if !exists {
		window = &TimeWindow{
			Events:     make([]time.Time, 0, 100),
			WaitTimes:  make([]float64, 0, 100),
			WindowSize: 60 * time.Second,
		}
		d.windows[key] = window
	}

	// Add event to window
	now := time.Now()
	window.Events = append(window.Events, now)
	window.WaitTimes = append(window.WaitTimes, waitMS)

	// Clean old events
	d.cleanWindow(window, now)

	// Detect patterns
	if pattern := d.detectPattern(window, event); pattern != nil {
		d.patterns[key] = pattern

		// Record metrics
		if d.patternsDetected != nil {
			d.patternsDetected.Add(ctx, 1, metric.WithAttributes(
				attribute.String("pattern_type", pattern.Type),
				attribute.Float64("confidence", pattern.Confidence),
			))
		}

		// Log significant patterns
		if pattern.Confidence > 0.8 {
			d.logger.Info("Detected starvation pattern",
				zap.String("type", pattern.Type),
				zap.Uint32("pid", pattern.PID),
				zap.Float64("confidence", pattern.Confidence),
				zap.Duration("periodicity", pattern.Periodicity),
				zap.Float64("avg_wait_ms", pattern.AverageWaitMS),
			)
		}
	}
}

// detectPattern analyzes the window for patterns
func (d *PatternDetector) detectPattern(window *TimeWindow, event *StarvationEvent) *Pattern {
	if len(window.Events) < 3 {
		return nil // Need at least 3 events
	}

	// Calculate statistics
	var totalWait float64
	var maxWait float64
	for _, wait := range window.WaitTimes {
		totalWait += wait
		if wait > maxWait {
			maxWait = wait
		}
	}
	avgWait := totalWait / float64(len(window.WaitTimes))

	// Check for periodicity
	periodicity, confidence := d.detectPeriodicity(window.Events)

	// Determine pattern type
	patternType := d.classifyPattern(event, avgWait, periodicity)

	if confidence > 0.5 { // Threshold for pattern detection
		return &Pattern{
			Type:          patternType,
			PID:           event.VictimPID,
			Count:         len(window.Events),
			FirstSeen:     window.Events[0],
			LastSeen:      window.Events[len(window.Events)-1],
			AverageWaitMS: avgWait,
			MaxWaitMS:     maxWait,
			Periodicity:   periodicity,
			Confidence:    confidence,
		}
	}

	return nil
}

// detectPeriodicity checks if events occur at regular intervals
func (d *PatternDetector) detectPeriodicity(events []time.Time) (time.Duration, float64) {
	if len(events) < 3 {
		return 0, 0
	}

	// Calculate intervals
	intervals := make([]time.Duration, len(events)-1)
	for i := 1; i < len(events); i++ {
		intervals[i-1] = events[i].Sub(events[i-1])
	}

	// Find median interval
	var totalInterval time.Duration
	for _, interval := range intervals {
		totalInterval += interval
	}
	avgInterval := totalInterval / time.Duration(len(intervals))

	// Calculate variance
	var variance float64
	for _, interval := range intervals {
		diff := float64(interval - avgInterval)
		variance += diff * diff
	}
	variance /= float64(len(intervals))

	// Low variance = high periodicity confidence
	stdDev := variance // Simplified
	confidence := 1.0 / (1.0 + stdDev/float64(avgInterval))

	return avgInterval, confidence
}

// classifyPattern determines the pattern type
func (d *PatternDetector) classifyPattern(event *StarvationEvent, avgWait float64, periodicity time.Duration) string {
	eventType := EventType(event.EventType)

	switch {
	case eventType == EventCFSThrottle && periodicity > 0:
		return "periodic_throttling"
	case eventType == EventCFSThrottle:
		return "sustained_throttling"
	case eventType == EventSchedWait && avgWait > 500:
		return "severe_starvation"
	case eventType == EventCoreMigrate:
		return "migration_storm"
	case eventType == EventNoisyNeighbor:
		return "noisy_neighbor_pattern"
	case periodicity > 0 && periodicity < 100*time.Millisecond:
		return "high_frequency_starvation"
	default:
		return "irregular_starvation"
	}
}

// cleanWindow removes old events outside the window
func (d *PatternDetector) cleanWindow(window *TimeWindow, now time.Time) {
	cutoff := now.Add(-window.WindowSize)

	// Find first event within window
	keepFrom := 0
	for i, eventTime := range window.Events {
		if eventTime.After(cutoff) {
			keepFrom = i
			break
		}
	}

	// Keep only recent events
	if keepFrom > 0 {
		window.Events = window.Events[keepFrom:]
		window.WaitTimes = window.WaitTimes[keepFrom:]
	}
}

// getPatternKey creates a unique key for pattern tracking
func (d *PatternDetector) getPatternKey(event *StarvationEvent) string {
	return fmt.Sprintf("%d:%d", event.VictimPID, event.EventType)
}

// GetPatterns returns current detected patterns
func (d *PatternDetector) GetPatterns() map[string]*Pattern {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Return copy to avoid race conditions
	patterns := make(map[string]*Pattern)
	for k, v := range d.patterns {
		patterns[k] = v
	}
	return patterns
}

// Cleanup removes old patterns
func (d *PatternDetector) Cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for key, pattern := range d.patterns {
		if now.Sub(pattern.LastSeen) > 5*time.Minute {
			delete(d.patterns, key)
			delete(d.windows, key)
		}
	}
}
