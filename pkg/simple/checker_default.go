//go:build !linux || !ebpf
// +build !linux !ebpf

package simple

import "github.com/yairfalse/tapio/pkg/ebpf"

// initializeEBPFMonitor is a no-op on non-Linux platforms
func (c *Checker) initializeEBPFMonitor(config *ebpf.Config) {
	// eBPF not available on this platform
	c.ebpfMonitor = nil
	c.enhancedExplainer = nil
}