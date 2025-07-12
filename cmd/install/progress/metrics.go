package progress

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"tapio/cmd/install/installer"
)

// metricsCollector implements installer.MetricsCollector
type metricsCollector struct {
	mu              sync.RWMutex
	startTime       time.Time
	stepDurations   map[string][]time.Duration
	errors          map[string][]error
	successfulSteps []string
	failedSteps     []string
	metadata        map[string]interface{}
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() installer.MetricsCollector {
	return &metricsCollector{
		startTime:     time.Now(),
		stepDurations: make(map[string][]time.Duration),
		errors:        make(map[string][]error),
		metadata:      make(map[string]interface{}),
	}
}

// RecordDuration records step duration
func (m *metricsCollector) RecordDuration(step string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stepDurations[step] == nil {
		m.stepDurations[step] = make([]time.Duration, 0)
	}
	m.stepDurations[step] = append(m.stepDurations[step], duration)
}

// RecordError records an error
func (m *metricsCollector) RecordError(step string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.errors[step] == nil {
		m.errors[step] = make([]error, 0)
	}
	m.errors[step] = append(m.errors[step], err)

	// Add to failed steps if not already there
	found := false
	for _, s := range m.failedSteps {
		if s == step {
			found = true
			break
		}
	}
	if !found {
		m.failedSteps = append(m.failedSteps, step)
	}
}

// RecordSuccess records a successful step
func (m *metricsCollector) RecordSuccess(step string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Add to successful steps if not already there
	found := false
	for _, s := range m.successfulSteps {
		if s == step {
			found = true
			break
		}
	}
	if !found {
		m.successfulSteps = append(m.successfulSteps, step)
	}
}

// GetReport returns metrics report
func (m *metricsCollector) GetReport() installer.MetricsReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	report := installer.MetricsReport{
		TotalDuration:   time.Since(m.startTime),
		StepDurations:   make(map[string]time.Duration),
		Errors:          make(map[string][]error),
		SuccessfulSteps: make([]string, len(m.successfulSteps)),
		FailedSteps:     make([]string, len(m.failedSteps)),
	}

	// Calculate average durations
	for step, durations := range m.stepDurations {
		if len(durations) > 0 {
			var total time.Duration
			for _, d := range durations {
				total += d
			}
			report.StepDurations[step] = total / time.Duration(len(durations))
		}
	}

	// Copy errors
	for step, errs := range m.errors {
		report.Errors[step] = make([]error, len(errs))
		copy(report.Errors[step], errs)
	}

	// Copy step lists
	copy(report.SuccessfulSteps, m.successfulSteps)
	copy(report.FailedSteps, m.failedSteps)

	return report
}

// SetMetadata sets metadata for the metrics
func (m *metricsCollector) SetMetadata(key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metadata[key] = value
}

// GetMetadata gets metadata value
func (m *metricsCollector) GetMetadata(key string) (interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.metadata[key]
	return val, ok
}

// ExportJSON exports metrics as JSON
func (m *metricsCollector) ExportJSON() ([]byte, error) {
	report := m.GetReport()

	export := map[string]interface{}{
		"timestamp":        m.startTime,
		"total_duration":   report.TotalDuration.String(),
		"successful_steps": report.SuccessfulSteps,
		"failed_steps":     report.FailedSteps,
		"step_durations":   map[string]string{},
		"errors":           map[string][]string{},
		"metadata":         m.metadata,
		"summary":          m.generateSummary(report),
	}

	// Convert durations to strings
	for step, duration := range report.StepDurations {
		export["step_durations"].(map[string]string)[step] = duration.String()
	}

	// Convert errors to strings
	for step, errs := range report.Errors {
		errStrs := make([]string, len(errs))
		for i, err := range errs {
			errStrs[i] = err.Error()
		}
		export["errors"].(map[string][]string)[step] = errStrs
	}

	return json.MarshalIndent(export, "", "  ")
}

// generateSummary creates a summary of the metrics
func (m *metricsCollector) generateSummary(report installer.MetricsReport) map[string]interface{} {
	successRate := float64(len(report.SuccessfulSteps)) /
		float64(len(report.SuccessfulSteps)+len(report.FailedSteps)) * 100

	// Find slowest steps
	type stepDuration struct {
		name     string
		duration time.Duration
	}

	var steps []stepDuration
	for name, duration := range report.StepDurations {
		steps = append(steps, stepDuration{name, duration})
	}

	sort.Slice(steps, func(i, j int) bool {
		return steps[i].duration > steps[j].duration
	})

	slowestSteps := make([]map[string]string, 0)
	for i := 0; i < len(steps) && i < 3; i++ {
		slowestSteps = append(slowestSteps, map[string]string{
			"name":     steps[i].name,
			"duration": steps[i].duration.String(),
		})
	}

	return map[string]interface{}{
		"success_rate":   fmt.Sprintf("%.1f%%", successRate),
		"total_steps":    len(report.SuccessfulSteps) + len(report.FailedSteps),
		"total_errors":   len(report.Errors),
		"slowest_steps":  slowestSteps,
		"execution_time": report.TotalDuration.String(),
	}
}

// TelemetryCollector collects and sends telemetry data
type TelemetryCollector struct {
	endpoint      string
	apiKey        string
	collector     installer.MetricsCollector
	enabled       bool
	mu            sync.Mutex
	buffer        []TelemetryEvent
	bufferSize    int
	flushInterval time.Duration
	done          chan struct{}
	wg            sync.WaitGroup
}

// TelemetryEvent represents a telemetry event
type TelemetryEvent struct {
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"`
	Step      string                 `json:"step,omitempty"`
	Duration  *time.Duration         `json:"duration,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	SessionID string                 `json:"session_id"`
	InstallID string                 `json:"install_id"`
	Platform  string                 `json:"platform"`
	Version   string                 `json:"version"`
}

// NewTelemetryCollector creates a new telemetry collector
func NewTelemetryCollector(endpoint, apiKey string) *TelemetryCollector {
	tc := &TelemetryCollector{
		endpoint:      endpoint,
		apiKey:        apiKey,
		collector:     NewMetricsCollector(),
		enabled:       endpoint != "" && apiKey != "",
		buffer:        make([]TelemetryEvent, 0, 100),
		bufferSize:    100,
		flushInterval: 30 * time.Second,
		done:          make(chan struct{}),
	}

	if tc.enabled {
		tc.wg.Add(1)
		go tc.flushLoop()
	}

	return tc
}

// RecordEvent records a telemetry event
func (tc *TelemetryCollector) RecordEvent(event TelemetryEvent) {
	if !tc.enabled {
		return
	}

	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.buffer = append(tc.buffer, event)

	// Flush if buffer is full
	if len(tc.buffer) >= tc.bufferSize {
		go tc.flush()
	}
}

// flushLoop periodically flushes telemetry data
func (tc *TelemetryCollector) flushLoop() {
	defer tc.wg.Done()

	ticker := time.NewTicker(tc.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tc.done:
			tc.flush() // Final flush
			return
		case <-ticker.C:
			tc.flush()
		}
	}
}

// flush sends buffered telemetry data
func (tc *TelemetryCollector) flush() {
	tc.mu.Lock()
	if len(tc.buffer) == 0 {
		tc.mu.Unlock()
		return
	}

	events := make([]TelemetryEvent, len(tc.buffer))
	copy(events, tc.buffer)
	tc.buffer = tc.buffer[:0]
	tc.mu.Unlock()

	// Send events (simplified - real implementation would batch and retry)
	// This is where you would send to your telemetry endpoint
	// For now, just log that we would send
	fmt.Printf("Would send %d telemetry events to %s\n", len(events), tc.endpoint)
}

// Close shuts down the telemetry collector
func (tc *TelemetryCollector) Close() {
	if !tc.enabled {
		return
	}

	close(tc.done)
	tc.wg.Wait()
}

// InstallationMetrics tracks detailed installation metrics
type InstallationMetrics struct {
	StartTime   time.Time          `json:"start_time"`
	EndTime     time.Time          `json:"end_time"`
	Duration    time.Duration      `json:"duration"`
	Platform    PlatformMetrics    `json:"platform"`
	Network     NetworkMetrics     `json:"network"`
	Performance PerformanceMetrics `json:"performance"`
	Steps       []StepMetrics      `json:"steps"`
	Errors      []ErrorMetrics     `json:"errors"`
	Success     bool               `json:"success"`
}

// PlatformMetrics contains platform information
type PlatformMetrics struct {
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Distribution string `json:"distribution,omitempty"`
	Version      string `json:"version,omitempty"`
	IsContainer  bool   `json:"is_container"`
	IsWSL        bool   `json:"is_wsl"`
}

// NetworkMetrics contains network performance metrics
type NetworkMetrics struct {
	DownloadSize     int64         `json:"download_size"`
	DownloadDuration time.Duration `json:"download_duration"`
	DownloadSpeed    float64       `json:"download_speed_mbps"`
	RetryCount       int           `json:"retry_count"`
	ProxyUsed        bool          `json:"proxy_used"`
}

// PerformanceMetrics contains system performance metrics
type PerformanceMetrics struct {
	CPUUsage    float64 `json:"cpu_usage_percent"`
	MemoryUsage float64 `json:"memory_usage_percent"`
	DiskUsage   float64 `json:"disk_usage_percent"`
	DiskIO      int64   `json:"disk_io_bytes"`
}

// StepMetrics contains metrics for a single step
type StepMetrics struct {
	Name      string        `json:"name"`
	StartTime time.Time     `json:"start_time"`
	Duration  time.Duration `json:"duration"`
	Success   bool          `json:"success"`
	Error     string        `json:"error,omitempty"`
	Retries   int           `json:"retries"`
}

// ErrorMetrics contains error information
type ErrorMetrics struct {
	Step      string    `json:"step"`
	Error     string    `json:"error"`
	Timestamp time.Time `json:"timestamp"`
	Fatal     bool      `json:"fatal"`
}
