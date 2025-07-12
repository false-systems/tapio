package sources

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// MockSource implements mock data collection for testing
type MockSource struct {
	name      string
	collector *collectors.MockCollector
	started   bool
}

// NewMockSource creates a new mock data source
func NewMockSource() *MockSource {
	return &MockSource{
		name:      "mock",
		collector: collectors.NewMockCollector(),
		started:   false,
	}
}

// Name returns the name of the data source
func (s *MockSource) Name() string {
	return s.name
}

// IsAvailable always returns true for mock source
func (s *MockSource) IsAvailable(ctx context.Context) bool {
	return s.collector.IsAvailable(ctx)
}

// Start begins mock data collection
func (s *MockSource) Start(ctx context.Context) error {
	if s.started {
		return fmt.Errorf("mock source already started")
	}

	if err := s.collector.Start(ctx); err != nil {
		return fmt.Errorf("failed to start mock collector: %w", err)
	}

	s.started = true
	return nil
}

// Stop stops mock data collection
func (s *MockSource) Stop(ctx context.Context) error {
	if !s.started {
		return nil
	}

	if err := s.collector.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop mock collector: %w", err)
	}

	s.started = false
	return nil
}

// Collect gathers mock data
func (s *MockSource) Collect(ctx context.Context, targets []collectors.Target) (collectors.DataSet, error) {
	if !s.started {
		return collectors.DataSet{}, fmt.Errorf("mock source not started")
	}

	dataset, err := s.collector.Collect(ctx, targets)
	if err != nil {
		return collectors.DataSet{}, fmt.Errorf("failed to collect mock data: %w", err)
	}

	dataset.Source = s.name
	return dataset, nil
}

// SupportsTarget checks if mock source can monitor the given target
func (s *MockSource) SupportsTarget(target collectors.Target) bool {
	// Mock source supports all target types
	return true
}

// SetScenario sets a specific testing scenario
func (s *MockSource) SetScenario(scenario string) error {
	return s.collector.SetScenario(scenario)
}

// GetAvailableScenarios returns available testing scenarios
func (s *MockSource) GetAvailableScenarios() []string {
	return s.collector.GetAvailableScenarios()
}

// CreateTestTarget creates a test target for the given type
func (s *MockSource) CreateTestTarget(targetType, name string) collectors.Target {
	target := collectors.Target{
		Type:   targetType,
		Name:   name,
		Labels: make(map[string]string),
	}

	// Add type-specific properties
	switch targetType {
	case "pod":
		target.Namespace = "test-namespace"
		target.Labels["app"] = name
		target.Labels["version"] = "1.0.0"

	case "container":
		target.Namespace = "test-namespace"
		target.Labels["container"] = name
		target.PID = 1234

	case "process":
		target.PID = 5678
		target.Labels["process"] = name

	case "service":
		target.Namespace = "test-namespace"
		target.Labels["service"] = name
		target.Labels["type"] = "ClusterIP"

	case "namespace":
		target.Labels["namespace"] = name

	default:
		target.Labels["type"] = targetType
	}

	return target
}

// CreateTestTargets creates multiple test targets
func (s *MockSource) CreateTestTargets(count int) []collectors.Target {
	targets := make([]collectors.Target, 0, count)

	targetTypes := []string{"pod", "container", "process", "service"}

	for i := 0; i < count; i++ {
		targetType := targetTypes[i%len(targetTypes)]
		name := fmt.Sprintf("test-%s-%d", targetType, i+1)
		target := s.CreateTestTarget(targetType, name)
		targets = append(targets, target)
	}

	return targets
}

// SimulateStressScenario simulates a high-load scenario
func (s *MockSource) SimulateStressScenario(ctx context.Context, targets []collectors.Target) (collectors.DataSet, error) {
	if err := s.SetScenario("high_memory"); err != nil {
		return collectors.DataSet{}, err
	}

	return s.Collect(ctx, targets)
}

// SimulateHealthyScenario simulates a healthy system scenario
func (s *MockSource) SimulateHealthyScenario(ctx context.Context, targets []collectors.Target) (collectors.DataSet, error) {
	if err := s.SetScenario("healthy"); err != nil {
		return collectors.DataSet{}, err
	}

	return s.Collect(ctx, targets)
}

// SimulateFailureScenario simulates a system failure scenario
func (s *MockSource) SimulateFailureScenario(ctx context.Context, targets []collectors.Target) (collectors.DataSet, error) {
	if err := s.SetScenario("oom_killer"); err != nil {
		return collectors.DataSet{}, err
	}

	return s.Collect(ctx, targets)
}
