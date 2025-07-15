//go:build linux && ebpf
// +build linux,ebpf

package simple

import "github.com/yairfalse/tapio/pkg/ebpf"

// initializeEBPFMonitor initializes eBPF monitoring on Linux systems
func (c *Checker) initializeEBPFMonitor(config *ebpf.Config) {
	// Create eBPF monitor with provided config
	c.ebpfMonitor = ebpf.NewMonitor(config)
	
	// Initialize enhanced explainer if eBPF is available
	if c.ebpfMonitor != nil {
		c.enhancedExplainer = NewSimpleEnhancedExplainer(c.ebpfMonitor)
	}
}