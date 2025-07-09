package collectors

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// MockCollector provides mock data for testing and non-Linux platforms
type MockCollector struct {
	enabled   bool
	scenarios map[string]MockScenario
}

// MockScenario defines a testing scenario
type MockScenario struct {
	Name        string
	Description string
	Metrics     []Metric
	Events      []Event
	Errors      []error
}

// NewMockCollector creates a new mock collector
func NewMockCollector() *MockCollector {
	return &MockCollector{
		enabled:   true,
		scenarios: createDefaultScenarios(),
	}
}

// IsAvailable always returns true for mock collector
func (c *MockCollector) IsAvailable(ctx context.Context) bool {
	return c.enabled
}

// Start initializes the mock collector
func (c *MockCollector) Start(ctx context.Context) error {
	return nil
}

// Stop cleans up the mock collector
func (c *MockCollector) Stop(ctx context.Context) error {
	return nil
}

// Collect generates mock data
func (c *MockCollector) Collect(ctx context.Context, targets []Target) (DataSet, error) {
	dataset := DataSet{
		Timestamp: time.Now(),
		Source:    "mock",
		Metrics:   []Metric{},
		Events:    []Event{},
		Errors:    []error{},
	}

	for _, target := range targets {
		if c.supportsTarget(target) {
			metrics := c.generateMetricsForTarget(target)
			dataset.Metrics = append(dataset.Metrics, metrics...)

			events := c.generateEventsForTarget(target)
			dataset.Events = append(dataset.Events, events...)
		}
	}

	return dataset, nil
}

// supportsTarget checks if the collector can monitor the target
func (c *MockCollector) supportsTarget(target Target) bool {
	switch target.Type {
	case "pod", "container", "process", "service":
		return true
	default:
		return false
	}
}

// generateMetricsForTarget generates realistic mock metrics
func (c *MockCollector) generateMetricsForTarget(target Target) []Metric {
	now := time.Now()

	// Use deterministic randomness based on target name for consistency
	seed := int64(0)
	for _, b := range target.Name {
		seed += int64(b)
	}
	r := rand.New(rand.NewSource(seed))

	return []Metric{
		{
			Name:      "memory_usage_bytes",
			Value:     float64(r.Intn(500)) * 1024 * 1024, // 0-500MB
			Unit:      "bytes",
			Target:    target,
			Timestamp: now,
			Labels: map[string]string{
				"collector": "mock",
				"type":      "memory",
			},
		},
		{
			Name:      "cpu_usage_percent",
			Value:     r.Float64() * 100, // 0-100%
			Unit:      "percent",
			Target:    target,
			Timestamp: now,
			Labels: map[string]string{
				"collector": "mock",
				"type":      "cpu",
			},
		},
		{
			Name:      "network_bytes_sent",
			Value:     float64(r.Intn(1024 * 1024)), // 0-1MB
			Unit:      "bytes",
			Target:    target,
			Timestamp: now,
			Labels: map[string]string{
				"collector": "mock",
				"type":      "network",
				"direction": "sent",
			},
		},
		{
			Name:      "network_bytes_received",
			Value:     float64(r.Intn(1024 * 1024)), // 0-1MB
			Unit:      "bytes",
			Target:    target,
			Timestamp: now,
			Labels: map[string]string{
				"collector": "mock",
				"type":      "network",
				"direction": "received",
			},
		},
		{
			Name:      "disk_usage_bytes",
			Value:     float64(r.Intn(1024)) * 1024 * 1024, // 0-1GB
			Unit:      "bytes",
			Target:    target,
			Timestamp: now,
			Labels: map[string]string{
				"collector": "mock",
				"type":      "disk",
			},
		},
	}
}

// generateEventsForTarget generates realistic mock events
func (c *MockCollector) generateEventsForTarget(target Target) []Event {
	now := time.Now()

	// Use deterministic randomness based on target name for consistency
	seed := int64(0)
	for _, b := range target.Name {
		seed += int64(b)
	}
	r := rand.New(rand.NewSource(seed))

	events := []Event{}

	// Generate different types of events based on target type
	switch target.Type {
	case "pod":
		events = append(events, Event{
			Type:      "pod_started",
			Message:   fmt.Sprintf("Pod %s started successfully", target.Name),
			Target:    target,
			Timestamp: now.Add(-time.Duration(r.Intn(3600)) * time.Second),
			Severity:  "info",
			Data: map[string]interface{}{
				"collector": "mock",
				"node":      "mock-node-1",
				"image":     "mock-image:latest",
			},
		})

		// Sometimes add restart events
		if r.Float32() < 0.3 {
			events = append(events, Event{
				Type:      "pod_restart",
				Message:   fmt.Sprintf("Pod %s restarted", target.Name),
				Target:    target,
				Timestamp: now.Add(-time.Duration(r.Intn(1800)) * time.Second),
				Severity:  "warning",
				Data: map[string]interface{}{
					"collector":     "mock",
					"restart_count": r.Intn(5) + 1,
				},
			})
		}

	case "container":
		events = append(events, Event{
			Type:      "container_created",
			Message:   fmt.Sprintf("Container %s created", target.Name),
			Target:    target,
			Timestamp: now.Add(-time.Duration(r.Intn(3600)) * time.Second),
			Severity:  "info",
			Data: map[string]interface{}{
				"collector": "mock",
				"runtime":   "containerd",
			},
		})

	case "process":
		events = append(events, Event{
			Type:      "process_start",
			Message:   fmt.Sprintf("Process %s started", target.Name),
			Target:    target,
			Timestamp: now.Add(-time.Duration(r.Intn(3600)) * time.Second),
			Severity:  "info",
			Data: map[string]interface{}{
				"collector": "mock",
				"pid":       r.Intn(65536),
				"ppid":      r.Intn(65536),
			},
		})
	}

	return events
}

// createDefaultScenarios creates predefined testing scenarios
func createDefaultScenarios() map[string]MockScenario {
	return map[string]MockScenario{
		"healthy": {
			Name:        "Healthy System",
			Description: "All systems operating normally",
			Metrics:     []Metric{},
			Events:      []Event{},
			Errors:      []error{},
		},
		"high_memory": {
			Name:        "High Memory Usage",
			Description: "System with high memory consumption",
			Metrics:     []Metric{},
			Events:      []Event{},
			Errors:      []error{},
		},
		"oom_killer": {
			Name:        "OOM Killer Active",
			Description: "System experiencing out-of-memory conditions",
			Metrics:     []Metric{},
			Events:      []Event{},
			Errors:      []error{},
		},
		"network_issues": {
			Name:        "Network Problems",
			Description: "Network connectivity issues",
			Metrics:     []Metric{},
			Events:      []Event{},
			Errors:      []error{},
		},
	}
}

// SetScenario activates a specific testing scenario
func (c *MockCollector) SetScenario(name string) error {
	if _, exists := c.scenarios[name]; !exists {
		return fmt.Errorf("scenario %s not found", name)
	}

	// TODO: Implement scenario-specific behavior
	return nil
}

// GetAvailableScenarios returns all available scenarios
func (c *MockCollector) GetAvailableScenarios() []string {
	scenarios := make([]string, 0, len(c.scenarios))
	for name := range c.scenarios {
		scenarios = append(scenarios, name)
	}
	return scenarios
}
