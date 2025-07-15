//go:build !linux
// +build !linux

package ebpf

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// StubMonitor provides a cross-platform stub implementation for eBPF monitoring
type StubMonitor struct {
	config    *Config
	platform  string
	started   bool
	lastError error
}

// NewMonitor creates a new stub eBPF monitor for non-Linux platforms
func NewMonitor(config *Config) Monitor {
	if config == nil {
		config = DefaultConfig()
	}

	return &StubMonitor{
		config:   config,
		platform: runtime.GOOS,
	}
}

// Start starts the stub monitor (no-op)
func (s *StubMonitor) Start(ctx context.Context) error {
	s.started = true
	s.lastError = nil

	// Log that we're running in stub mode
	fmt.Printf("eBPF monitor started in stub mode on %s\n", s.platform)

	return nil
}

// Stop stops the stub monitor
func (s *StubMonitor) Stop() error {
	s.started = false
	return nil
}

// GetMemoryStats returns mock memory statistics for development
func (s *StubMonitor) GetMemoryStats() ([]ProcessMemoryStats, error) {
	if !s.started {
		return nil, fmt.Errorf("monitor not started")
	}

	// Return mock data for development
	return []ProcessMemoryStats{
		{
			PID:            1,
			Command:        "mock-process",
			TotalAllocated: 1024 * 1024 * 10, // 10MB
			TotalFreed:     1024 * 1024 * 5,  // 5MB
			CurrentUsage:   1024 * 1024 * 5,  // 5MB
			AllocationRate: 1024.0,           // 1KB/s
			LastUpdate:     time.Now(),
			InContainer:    false,
			ContainerPID:   0,
			GrowthPattern: []MemoryDataPoint{
				{
					Timestamp: time.Now().Add(-time.Minute),
					Usage:     1024 * 1024 * 4,
				},
				{
					Timestamp: time.Now(),
					Usage:     1024 * 1024 * 5,
				},
			},
		},
		{
			PID:            100,
			Command:        "mock-container-process",
			TotalAllocated: 1024 * 1024 * 50, // 50MB
			TotalFreed:     1024 * 1024 * 20, // 20MB
			CurrentUsage:   1024 * 1024 * 30, // 30MB
			AllocationRate: 5120.0,           // 5KB/s
			LastUpdate:     time.Now(),
			InContainer:    true,
			ContainerPID:   99,
			GrowthPattern: []MemoryDataPoint{
				{
					Timestamp: time.Now().Add(-time.Minute),
					Usage:     1024 * 1024 * 25,
				},
				{
					Timestamp: time.Now(),
					Usage:     1024 * 1024 * 30,
				},
			},
		},
	}, nil
}

// GetMemoryPredictions returns mock OOM predictions for development
func (s *StubMonitor) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error) {
	if !s.started {
		return nil, fmt.Errorf("monitor not started")
	}

	// Return mock predictions for development
	predictions := make(map[uint32]*OOMPrediction)

	for pid, limit := range limits {
		// Create a mock prediction
		currentUsage := limit / 2 // 50% of limit
		predictions[pid] = &OOMPrediction{
			PID:                pid,
			TimeToOOM:          time.Hour * 2, // 2 hours
			Confidence:         0.2,           // Low confidence for mock
			CurrentUsage:       currentUsage,
			MemoryLimit:        limit,
			PredictedPeakUsage: currentUsage + (limit / 4), // 75% of limit
		}
	}

	return predictions, nil
}

// IsAvailable returns false for non-Linux platforms
func (s *StubMonitor) IsAvailable() bool {
	return false
}

// GetLastError returns platform not supported error
func (s *StubMonitor) GetLastError() error {
	if s.lastError != nil {
		return s.lastError
	}
	return fmt.Errorf("eBPF is not supported on %s", s.platform)
}
