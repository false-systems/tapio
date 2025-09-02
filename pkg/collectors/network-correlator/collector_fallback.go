//go:build !linux
// +build !linux

package networkcorrelator

import (
	"context"
	"fmt"
)

// Platform-specific fallback for non-Linux systems

func (c *Collector) startPlatformSpecific(ctx context.Context) error {
	c.logger.Warn("eBPF not available on this platform, running in limited mode")

	// Could implement packet capture using pcap or other methods
	// For now, just return nil to allow development on Mac/Windows

	return nil
}

func (c *Collector) stopPlatformSpecific() {
	// Nothing to clean up on non-Linux
}

func (c *Collector) readEvents(ctx context.Context) error {
	// On non-Linux, we can't read eBPF events
	// Could implement alternative using pcap
	<-ctx.Done()
	return ctx.Err()
}

func (c *Collector) attachToInterfaces() error {
	return fmt.Errorf("eBPF not supported on this platform")
}
