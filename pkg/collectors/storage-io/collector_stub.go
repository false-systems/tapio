//go:build !linux
// +build !linux

package storageio

import (
	"fmt"
)

// startEBPFImpl is a stub implementation for non-Linux platforms
func (c *Collector) startEBPFImpl() error {
	c.logger.Warn("eBPF storage monitoring is only supported on Linux")
	return fmt.Errorf("eBPF storage monitoring is not supported on this platform")
}

// stopEBPFImpl is a stub implementation for non-Linux platforms
func (c *Collector) stopEBPFImpl() {
	// No-op on non-Linux platforms
}

// processStorageEventsImpl is a stub implementation for non-Linux platforms
func (c *Collector) processStorageEventsImpl() {
	defer c.wg.Done()

	c.logger.Warn("Storage I/O event processing is only supported on Linux")

	// Wait for context cancellation
	<-c.ctx.Done()
}
