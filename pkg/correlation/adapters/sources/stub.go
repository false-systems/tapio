package sources

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/correlation/domain"
)

// StubEventSource is a stub implementation for testing and platforms where actual sources are unavailable
type StubEventSource struct {
	sourceType string
	available  bool
	events     []domain.Event
}

// NewStubEventSource creates a new stub event source
func NewStubEventSource(sourceType string, available bool) *StubEventSource {
	return &StubEventSource{
		sourceType: sourceType,
		available:  available,
		events:     make([]domain.Event, 0),
	}
}

// GetEvents retrieves events matching the filter
func (s *StubEventSource) GetEvents(ctx context.Context, filter domain.Filter) ([]domain.Event, error) {
	if !s.available {
		return nil, fmt.Errorf("source %s is not available", s.sourceType)
	}
	
	var result []domain.Event
	for _, event := range s.events {
		if filter.Matches(event) {
			result = append(result, event)
		}
	}
	
	// Apply limit if specified
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	
	return result, nil
}

// Stream provides a continuous stream of events
func (s *StubEventSource) Stream(ctx context.Context, filter domain.Filter) (<-chan domain.Event, error) {
	if !s.available {
		return nil, fmt.Errorf("source %s is not available", s.sourceType)
	}
	
	eventChan := make(chan domain.Event, 100)
	
	go func() {
		defer close(eventChan)
		
		for _, event := range s.events {
			if filter.Matches(event) {
				select {
				case eventChan <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	
	return eventChan, nil
}

// GetSourceType returns the source type identifier
func (s *StubEventSource) GetSourceType() string {
	return s.sourceType
}

// IsAvailable checks if the source is available
func (s *StubEventSource) IsAvailable() bool {
	return s.available
}

// Close closes the event source
func (s *StubEventSource) Close() error {
	return nil
}

// AddEvent adds an event to the stub source (for testing)
func (s *StubEventSource) AddEvent(event domain.Event) {
	s.events = append(s.events, event)
}

// SetAvailable sets the availability status
func (s *StubEventSource) SetAvailable(available bool) {
	s.available = available
}

// EBPFStubSource is a stub for eBPF event source
type EBPFStubSource struct {
	*StubEventSource
}

// NewEBPFStubSource creates a new eBPF stub source
func NewEBPFStubSource() *EBPFStubSource {
	return &EBPFStubSource{
		StubEventSource: NewStubEventSource("ebpf", false), // Not available by default on non-Linux
	}
}

// SystemdStubSource is a stub for systemd event source
type SystemdStubSource struct {
	*StubEventSource
}

// NewSystemdStubSource creates a new systemd stub source
func NewSystemdStubSource() *SystemdStubSource {
	return &SystemdStubSource{
		StubEventSource: NewStubEventSource("systemd", false), // Not available by default on non-Linux
	}
}

// JournaldStubSource is a stub for journald event source
type JournaldStubSource struct {
	*StubEventSource
}

// NewJournaldStubSource creates a new journald stub source
func NewJournaldStubSource() *JournaldStubSource {
	return &JournaldStubSource{
		StubEventSource: NewStubEventSource("journald", false), // Not available by default on non-Linux
	}
}

// KubernetesStubSource is a stub for Kubernetes event source
type KubernetesStubSource struct {
	*StubEventSource
}

// NewKubernetesStubSource creates a new Kubernetes stub source
func NewKubernetesStubSource() *KubernetesStubSource {
	return &KubernetesStubSource{
		StubEventSource: NewStubEventSource("kubernetes", true), // Available on all platforms
	}
}