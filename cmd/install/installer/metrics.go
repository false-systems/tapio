package installer

import (
	"sync"
	"time"
)

// metricsCollector collects metrics for the installation process
type metricsCollector struct {
	mu         sync.RWMutex
	durations  map[string]time.Duration
	errors     map[string]error
	successes  map[string]bool
	startTimes map[string]time.Time
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() MetricsCollector {
	return &metricsCollector{
		durations:  make(map[string]time.Duration),
		errors:     make(map[string]error),
		successes:  make(map[string]bool),
		startTimes: make(map[string]time.Time),
	}
}

// RecordDuration records step duration
func (m *metricsCollector) RecordDuration(step string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.durations[step] = duration
}

// RecordError records an error
func (m *metricsCollector) RecordError(step string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[step] = err
}

// RecordSuccess records a successful step
func (m *metricsCollector) RecordSuccess(step string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.successes[step] = true
}

// GetReport returns metrics report
func (m *metricsCollector) GetReport() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	report := make(map[string]interface{})

	// Copy durations
	durations := make(map[string]time.Duration)
	for k, v := range m.durations {
		durations[k] = v
	}
	report["durations"] = durations

	// Copy errors
	errors := make(map[string]string)
	for k, v := range m.errors {
		if v != nil {
			errors[k] = v.Error()
		}
	}
	report["errors"] = errors

	// Copy successes
	successes := make(map[string]bool)
	for k, v := range m.successes {
		successes[k] = v
	}
	report["successes"] = successes

	// Calculate totals
	var totalDuration time.Duration
	for _, d := range m.durations {
		totalDuration += d
	}
	report["total_duration"] = totalDuration
	report["total_errors"] = len(m.errors)
	report["total_successes"] = len(m.successes)

	return report
}
