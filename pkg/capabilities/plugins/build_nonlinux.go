//go:build !linux
// +build !linux

package plugins

import "github.com/yairfalse/tapio/pkg/capabilities"

// NewEBPFMemoryPlugin creates a not-available plugin on non-Linux platforms
// This provides build optimization - no eBPF code compiled for non-Linux
func NewEBPFMemoryPlugin(config interface{}) capabilities.MemoryCapability {
	return &NotAvailableMemoryPlugin{
		NotAvailablePlugin: NewNotAvailablePlugin(
			"ebpf-memory", 
			"eBPF memory monitoring only available on Linux",
		),
	}
}

// NotAvailableMemoryPlugin implements MemoryCapability for unavailable features
type NotAvailableMemoryPlugin struct {
	*NotAvailablePlugin
}

// GetMemoryStats returns capability error
func (p *NotAvailableMemoryPlugin) GetMemoryStats() ([]capabilities.ProcessMemoryStats, error) {
	return nil, capabilities.NewCapabilityError(
		p.name, 
		p.reason, 
		p.platform,
	)
}

// GetMemoryPredictions returns capability error
func (p *NotAvailableMemoryPlugin) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*capabilities.OOMPrediction, error) {
	return nil, capabilities.NewCapabilityError(
		p.name, 
		p.reason, 
		p.platform,
	)
}