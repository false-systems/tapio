//go:build !linux
// +build !linux

package bpf

import (
	"fmt"
	"runtime"

	"github.com/cilium/ebpf"
)

// LoadNetworkMonitor is a stub for non-Linux platforms
func LoadNetworkMonitor() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("network eBPF monitoring not supported on %s", runtime.GOOS)
}

// GetNetworkMonitorSpecs is a stub for non-Linux platforms
func GetNetworkMonitorSpecs() (*ebpf.CollectionSpec, *ebpf.CollectionSpec, error) {
	return nil, nil, fmt.Errorf("network eBPF monitoring not supported on %s", runtime.GOOS)
}
