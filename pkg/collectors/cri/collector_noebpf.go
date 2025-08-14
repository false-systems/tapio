//go:build !linux || !cgo

package cri

import (
	"context"
)

// EBPFCollector is a stub for non-Linux platforms
type EBPFCollector struct {
	*Collector
}

// NewEBPFCollector creates a CRI collector without eBPF enhancement
// On non-Linux platforms, this returns the base collector
func NewEBPFCollector(name string, config Config) (*EBPFCollector, error) {
	baseCollector, err := NewCollector(name, config)
	if err != nil {
		return nil, err
	}

	return &EBPFCollector{
		Collector: baseCollector,
	}, nil
}

// Start starts the base collector (no eBPF enhancement)
func (c *EBPFCollector) Start(ctx context.Context) error {
	c.logger.Info("Starting CRI collector without eBPF enhancement (unsupported platform)")
	return c.Collector.Start(ctx)
}

// Stop stops the base collector
func (c *EBPFCollector) Stop() error {
	return c.Collector.Stop()
}

// UpdateContainerMetadata is a no-op on non-eBPF platforms
func (c *EBPFCollector) UpdateContainerMetadata(pid uint32, containerID, podUID string, memoryLimit uint64) error {
	// No-op - eBPF not available
	return nil
}

// GetEBPFStats returns empty stats on non-eBPF platforms
func (c *EBPFCollector) GetEBPFStats() map[string]uint64 {
	return nil
}
