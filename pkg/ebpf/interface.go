//go:build !linux || !ebpf
// +build !linux !ebpf

package ebpf

// All types and interfaces are defined in types.go
// This file only contains the stub implementation for non-Linux platforms

// NewMonitor creates a new eBPF monitor
func NewMonitor(config *Config) Monitor {
	if config == nil {
		config = DefaultConfig()
	}
	return &stubMonitor{lastError: ErrNotSupported}
}