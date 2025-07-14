package collectors

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/types"
)

// EBPFCollectorFactory creates eBPF collectors
type EBPFCollectorFactory struct{}

// NewEBPFCollectorFactory creates a new eBPF collector factory
func NewEBPFCollectorFactory() CollectorFactory {
	return &EBPFCollectorFactory{}
}

// CreateCollector creates a new eBPF collector instance
func (f *EBPFCollectorFactory) CreateCollector(config types.CollectorConfig) (Collector, error) {
	// Create the eBPF adapter (it handles platform detection internally)
	adapter, err := NewEBPFAdapter()
	if err != nil {
		return nil, fmt.Errorf("failed to create eBPF adapter: %w", err)
	}
	
	// Configure the adapter
	if err := adapter.Configure(config); err != nil {
		return nil, fmt.Errorf("failed to configure eBPF adapter: %w", err)
	}
	
	return adapter, nil
}

// ValidateConfig validates the eBPF collector configuration
func (f *EBPFCollectorFactory) ValidateConfig(config types.CollectorConfig) error {
	// Validate basic configuration
	if config.Type != "ebpf" {
		return fmt.Errorf("invalid collector type: %s", config.Type)
	}
	
	// Validate eBPF-specific configuration
	if config.Extra != nil {
		// Check ML prediction threshold
		if threshold, exists := config.Extra["prediction_threshold"]; exists {
			if val, ok := threshold.(float64); ok {
				if val < 0.0 || val > 1.0 {
					return fmt.Errorf("prediction_threshold must be between 0.0 and 1.0")
				}
			} else {
				return fmt.Errorf("prediction_threshold must be a float")
			}
		}
		
		// Check ring buffer size
		if size, exists := config.Extra["ring_buffer_size"]; exists {
			if val, ok := size.(float64); ok {
				if val < 1024*1024 || val > 128*1024*1024 {
					return fmt.Errorf("ring_buffer_size must be between 1MB and 128MB")
				}
			} else {
				return fmt.Errorf("ring_buffer_size must be a number")
			}
		}
		
		// Check event rate limit
		if limit, exists := config.Extra["event_rate_limit"]; exists {
			if val, ok := limit.(float64); ok {
				if val < 0 {
					return fmt.Errorf("event_rate_limit must be positive")
				}
			} else {
				return fmt.Errorf("event_rate_limit must be a number")
			}
		}
	}
	
	return nil
}

// GetRequirements returns the requirements for running eBPF collectors
func (f *EBPFCollectorFactory) GetRequirements() types.CollectorRequirements {
	platform := GetCurrentPlatform()
	
	if !platform.HasEBPF {
		// Minimal requirements for stub mode
		return types.CollectorRequirements{
			Capabilities:  []string{},
			KernelVersion: "",
			Features:      []string{},
			Resources: types.ResourceRequirements{
				MinMemoryMB: 1,
				MinCPUMilli: 1,
			},
		}
	}
	
	return types.CollectorRequirements{
		Capabilities: []string{
			"CAP_SYS_ADMIN",
			"CAP_SYS_RESOURCE",
			"CAP_NET_ADMIN",
		},
		KernelVersion: "4.18.0", // Minimum kernel version for BTF support
		Features: []string{
			"BTF",
			"BPF_PROG_TYPE_TRACEPOINT",
			"BPF_MAP_TYPE_RINGBUF",
		},
		Resources: types.ResourceRequirements{
			MinMemoryMB: 50,
			MinCPUMilli: 5,
		},
	}
}

// isEBPFSupported checks if eBPF is supported on this system
func isEBPFSupported() bool {
	platform := GetCurrentPlatform()
	return platform.HasEBPF
}