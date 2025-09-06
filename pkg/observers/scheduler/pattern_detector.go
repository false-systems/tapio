package scheduler

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// Pattern represents a detected scheduling pattern
type Pattern struct {
	Type        string
	Description string
	Confidence  float64
	FirstSeen   time.Time
	LastSeen    time.Time
	Count       int
}

// PatternDetector detects recurring scheduling patterns
type PatternDetector struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Event history for pattern detection
	recentEvents []interface{}
	maxEvents    int

	// Detected patterns
	patterns map[string]*Pattern

	// Statistics for pattern detection
	stats struct {
		totalEvents     int64
		delayEvents     int64
		throttleEvents  int64
		migrationEvents int64
		inversionEvents int64
	}
}

// NewPatternDetector creates a new pattern detector
func NewPatternDetector(logger *zap.Logger) *PatternDetector {
	return &PatternDetector{
		logger:       logger,
		recentEvents: make([]interface{}, 0, 1000),
		maxEvents:    1000,
		patterns:     make(map[string]*Pattern),
	}
}

// AddEvent adds an event for pattern analysis
func (pd *PatternDetector) AddEvent(event interface{}) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	pd.stats.totalEvents++

	// Add to recent events (circular buffer behavior)
	if len(pd.recentEvents) >= pd.maxEvents {
		pd.recentEvents = pd.recentEvents[1:]
	}
	pd.recentEvents = append(pd.recentEvents, event)

	// Update statistics based on event type
	if schedEvent, ok := event.(*SchedEvent); ok {
		switch schedEvent.EventType {
		case 1:
			pd.stats.delayEvents++
		case 2:
			pd.stats.throttleEvents++
		case 3:
			pd.stats.migrationEvents++
		case 4:
			pd.stats.inversionEvents++
		}
	}
}

// DetectPatterns analyzes recent events for patterns
func (pd *PatternDetector) DetectPatterns() []*Pattern {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	detectedPatterns := make([]*Pattern, 0)

	// Detect CPU throttling pattern
	if pd.detectThrottlingPattern() {
		pattern := &Pattern{
			Type:        "cpu_throttling",
			Description: "Consistent CPU quota throttling detected",
			Confidence:  pd.calculateThrottleConfidence(),
			FirstSeen:   time.Now().Add(-5 * time.Minute),
			LastSeen:    time.Now(),
			Count:       int(pd.stats.throttleEvents),
		}
		detectedPatterns = append(detectedPatterns, pattern)
		pd.patterns["cpu_throttling"] = pattern
	}

	// Detect scheduling delay pattern
	if pd.detectDelayPattern() {
		pattern := &Pattern{
			Type:        "scheduling_delays",
			Description: "Persistent scheduling delays indicating CPU contention",
			Confidence:  pd.calculateDelayConfidence(),
			FirstSeen:   time.Now().Add(-5 * time.Minute),
			LastSeen:    time.Now(),
			Count:       int(pd.stats.delayEvents),
		}
		detectedPatterns = append(detectedPatterns, pattern)
		pd.patterns["scheduling_delays"] = pattern
	}

	// Detect excessive migration pattern
	if pd.detectMigrationPattern() {
		pattern := &Pattern{
			Type:        "cpu_migrations",
			Description: "Excessive CPU core migrations affecting cache performance",
			Confidence:  pd.calculateMigrationConfidence(),
			FirstSeen:   time.Now().Add(-5 * time.Minute),
			LastSeen:    time.Now(),
			Count:       int(pd.stats.migrationEvents),
		}
		detectedPatterns = append(detectedPatterns, pattern)
		pd.patterns["cpu_migrations"] = pattern
	}

	// Detect priority inversion pattern
	if pd.detectInversionPattern() {
		pattern := &Pattern{
			Type:        "priority_inversions",
			Description: "Priority inversions causing high-priority task delays",
			Confidence:  0.7,
			FirstSeen:   time.Now().Add(-5 * time.Minute),
			LastSeen:    time.Now(),
			Count:       int(pd.stats.inversionEvents),
		}
		detectedPatterns = append(detectedPatterns, pattern)
		pd.patterns["priority_inversions"] = pattern
	}

	return detectedPatterns
}

// detectThrottlingPattern checks for CPU throttling patterns
func (pd *PatternDetector) detectThrottlingPattern() bool {
	if pd.stats.totalEvents < 100 {
		return false
	}

	throttleRate := float64(pd.stats.throttleEvents) / float64(pd.stats.totalEvents)
	return throttleRate > 0.1 // More than 10% throttle events
}

// detectDelayPattern checks for scheduling delay patterns
func (pd *PatternDetector) detectDelayPattern() bool {
	if pd.stats.totalEvents < 100 {
		return false
	}

	delayRate := float64(pd.stats.delayEvents) / float64(pd.stats.totalEvents)
	return delayRate > 0.2 // More than 20% delay events
}

// detectMigrationPattern checks for excessive migration patterns
func (pd *PatternDetector) detectMigrationPattern() bool {
	if pd.stats.totalEvents < 100 {
		return false
	}

	migrationRate := float64(pd.stats.migrationEvents) / float64(pd.stats.totalEvents)
	return migrationRate > 0.15 // More than 15% migration events
}

// detectInversionPattern checks for priority inversion patterns
func (pd *PatternDetector) detectInversionPattern() bool {
	return pd.stats.inversionEvents > 5 // Any priority inversions are concerning
}

// calculateThrottleConfidence calculates confidence for throttle pattern
func (pd *PatternDetector) calculateThrottleConfidence() float64 {
	if pd.stats.totalEvents < 100 {
		return 0.5
	}

	rate := float64(pd.stats.throttleEvents) / float64(pd.stats.totalEvents)
	if rate > 0.3 {
		return 0.95
	}
	if rate > 0.2 {
		return 0.85
	}
	if rate > 0.1 {
		return 0.75
	}
	return 0.6
}

// calculateDelayConfidence calculates confidence for delay pattern
func (pd *PatternDetector) calculateDelayConfidence() float64 {
	if pd.stats.totalEvents < 100 {
		return 0.5
	}

	rate := float64(pd.stats.delayEvents) / float64(pd.stats.totalEvents)
	if rate > 0.4 {
		return 0.95
	}
	if rate > 0.3 {
		return 0.85
	}
	if rate > 0.2 {
		return 0.75
	}
	return 0.6
}

// calculateMigrationConfidence calculates confidence for migration pattern
func (pd *PatternDetector) calculateMigrationConfidence() float64 {
	if pd.stats.totalEvents < 100 {
		return 0.5
	}

	rate := float64(pd.stats.migrationEvents) / float64(pd.stats.totalEvents)
	if rate > 0.25 {
		return 0.9
	}
	if rate > 0.15 {
		return 0.8
	}
	return 0.7
}

// GetStats returns pattern detection statistics
func (pd *PatternDetector) GetStats() map[string]int64 {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	return map[string]int64{
		"total_events":     pd.stats.totalEvents,
		"delay_events":     pd.stats.delayEvents,
		"throttle_events":  pd.stats.throttleEvents,
		"migration_events": pd.stats.migrationEvents,
		"inversion_events": pd.stats.inversionEvents,
	}
}
