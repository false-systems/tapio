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
func (m *metricsCollector) GetReport() MetricsReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	report := MetricsReport{
		StepDurations: make(map[string]time.Duration),
		Errors:        make(map[string][]error),
	}

	// Copy durations
	for k, v := range m.durations {
		report.StepDurations[k] = v
	}

	// Copy errors
	for k, v := range m.errors {
		if v != nil {
			report.Errors[k] = []error{v}
		}
	}

	// Collect successful and failed steps
	for k, success := range m.successes {
		if success {
			report.SuccessfulSteps = append(report.SuccessfulSteps, k)
		} else {
			report.FailedSteps = append(report.FailedSteps, k)
		}
	}

	// Add failed steps from errors
	for k := range m.errors {
		if _, ok := m.successes[k]; !ok {
			report.FailedSteps = append(report.FailedSteps, k)
		}
	}

	// Calculate total duration
	var totalDuration time.Duration
	for _, d := range m.durations {
		totalDuration += d
	}
	report.TotalDuration = totalDuration

	return report
}
