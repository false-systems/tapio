//go:build !linux
// +build !linux

package dns

import (
	"go.uber.org/zap"
)

// ebpfState stub for non-Linux platforms
type ebpfState struct{}

// startEBPF is a no-op on non-Linux platforms
func (c *Collector) startEBPF() error {
	c.logger.Info("eBPF monitoring not supported on this platform, using polling mode",
		zap.String("collector", c.name),
		zap.String("platform", "non-linux"),
	)
	
	// Disable eBPF and fall back to alternative collection methods
	c.config.EnableEBPF = false
	c.ebpfState = &ebpfState{}
	
	return nil
}

// stopEBPF is a no-op on non-Linux platforms
func (c *Collector) stopEBPF() {
	// No-op
}

// readEBPFEvents is a no-op on non-Linux platforms
func (c *Collector) readEBPFEvents() {
	// No-op - this should never be called on non-Linux platforms
	c.logger.Debug("readEBPFEvents called on non-Linux platform (no-op)",
		zap.String("collector", c.name),
	)
}