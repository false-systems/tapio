package sources

import (
	"context"
	"fmt"
	"runtime"

	"github.com/falseyair/tapio/pkg/collectors"
)

// EBPFSource implements eBPF-based data collection with platform detection
type EBPFSource struct {
	name      string
	platform  *collectors.Platform
	collector interface{}
	started   bool
}

// NewEBPFSource creates a new eBPF data source
func NewEBPFSource() *EBPFSource {
	platform := collectors.DetectPlatform()

	source := &EBPFSource{
		name:     "ebpf",
		platform: platform,
		started:  false,
	}

	// Choose collector based on platform
	if platform.SupportseBPF {
		source.collector = collectors.NewLinuxEBPFCollector()
	} else {
		// Fallback to mock collector on non-Linux platforms
		source.collector = collectors.NewMockCollector()
	}

	return source
}

// Name returns the name of the data source
func (s *EBPFSource) Name() string {
	return s.name
}

// IsAvailable checks if eBPF is available on the current platform
func (s *EBPFSource) IsAvailable(ctx context.Context) bool {
	switch collector := s.collector.(type) {
	case interface{ IsAvailable(context.Context) bool }:
		return collector.IsAvailable(ctx)
	default:
		return false
	}
}

// Start begins eBPF data collection
func (s *EBPFSource) Start(ctx context.Context) error {
	if s.started {
		return fmt.Errorf("eBPF source already started")
	}

	// Start the appropriate collector
	switch collector := s.collector.(type) {
	case interface{ Start(context.Context) error }:
		if err := collector.Start(ctx); err != nil {
			return fmt.Errorf("failed to start eBPF collector: %w", err)
		}
	default:
		return fmt.Errorf("collector does not support starting")
	}

	s.started = true
	return nil
}

// Stop stops eBPF data collection
func (s *EBPFSource) Stop(ctx context.Context) error {
	if !s.started {
		return nil
	}

	// Stop the appropriate collector
	switch collector := s.collector.(type) {
	case interface{ Stop(context.Context) error }:
		if err := collector.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop eBPF collector: %w", err)
		}
	default:
		return fmt.Errorf("collector does not support stopping")
	}

	s.started = false
	return nil
}

// Collect gathers data using eBPF or mock collector
func (s *EBPFSource) Collect(ctx context.Context, targets []collectors.Target) (collectors.DataSet, error) {
	if !s.started {
		return collectors.DataSet{}, fmt.Errorf("eBPF source not started")
	}

	switch collector := s.collector.(type) {
	case interface {
		Collect(context.Context, []Target) (DataSet, error)
	}:
		dataset, err := collector.Collect(ctx, targets)
		if err != nil {
			return DataSet{}, fmt.Errorf("failed to collect eBPF data: %w", err)
		}

		// Add platform information to the dataset
		dataset.Source = s.getSourceName()
		return dataset, nil

	default:
		return DataSet{}, fmt.Errorf("collector does not support data collection")
	}
}

// SupportsTarget checks if eBPF can monitor the given target
func (s *EBPFSource) SupportsTarget(target collectors.Target) bool {
	// eBPF works best with process-level targets
	switch target.Type {
	case "pod", "container", "process":
		return true
	case "service":
		// Services can be monitored if they have associated processes
		return target.PID > 0
	default:
		return false
	}
}

// getSourceName returns the appropriate source name based on platform
func (s *EBPFSource) getSourceName() string {
	if s.platform.SupportseBPF {
		return fmt.Sprintf("ebpf_%s", runtime.GOOS)
	}
	return fmt.Sprintf("mock_%s", runtime.GOOS)
}

// GetPlatformInfo returns information about the current platform
func (s *EBPFSource) GetPlatformInfo() *collectors.Platform {
	return s.platform
}

// GetCapabilities returns the capabilities of the current platform
func (s *EBPFSource) GetCapabilities(ctx context.Context) *collectors.Capabilities {
	return collectors.DetectCapabilities(ctx)
}

// IsUsingMock returns true if the source is using mock collector
func (s *EBPFSource) IsUsingMock() bool {
	_, isMock := s.collector.(*collectors.MockCollector)
	return isMock
}

// SetMockScenario sets a specific scenario for mock collector
func (s *EBPFSource) SetMockScenario(scenario string) error {
	if mockCollector, ok := s.collector.(*collectors.MockCollector); ok {
		return mockCollector.SetScenario(scenario)
	}
	return fmt.Errorf("not using mock collector")
}

// GetAvailableMockScenarios returns available mock scenarios
func (s *EBPFSource) GetAvailableMockScenarios() []string {
	if mockCollector, ok := s.collector.(*collectors.MockCollector); ok {
		return mockCollector.GetAvailableScenarios()
	}
	return []string{}
}
