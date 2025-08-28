//go:build linux
// +build linux

package bpf

import (
	"fmt"

	"github.com/cilium/ebpf"
)

// LoadNetworkMonitor loads the network monitoring eBPF programs
func LoadNetworkMonitor() (*ebpf.CollectionSpec, error) {
	spec, err := LoadNetworkmonitor()
	if err != nil {
		return nil, fmt.Errorf("loading network monitor BPF spec: %w", err)
	}
	return spec, nil
}

// GetNetworkMonitorSpecs returns the eBPF collection spec for the network monitor
func GetNetworkMonitorSpecs() (*ebpf.CollectionSpec, *ebpf.CollectionSpec, error) {
	// Load base network monitor
	baseSpec, err := LoadNetworkmonitor()
	if err != nil {
		return nil, nil, fmt.Errorf("loading base network monitor: %w", err)
	}

	// For now, return the same spec twice (intelligence spec would be loaded separately)
	// Load network_monitor_intelligence.c when intelligence features are enabled
	// This will be activated based on configuration
	return baseSpec, baseSpec, nil
}
