//go:build !linux
// +build !linux

package plugins

// NewEBPFMemoryPlugin creates a not-available plugin on non-Linux platforms
// This provides build optimization - no eBPF code compiled for non-Linux
func NewEBPFMemoryPlugin(config interface{}) MemoryCapability {
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
func (p *NotAvailableMemoryPlugin) GetMemoryStats() ([]ProcessMemoryStats, error) {
	return nil, NewCapabilityError(
		p.name,
		p.reason,
		p.platform,
	)
}

// GetMemoryPredictions returns capability error
func (p *NotAvailableMemoryPlugin) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error) {
	return nil, NewCapabilityError(
		p.name,
		p.reason,
		p.platform,
	)
}
