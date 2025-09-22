//go:build !linux
// +build !linux

package link

import (
	"time"

	"go.uber.org/zap"
)

// startEBPF is a no-op on non-Linux systems
func (o *Observer) startEBPF() error {
	o.logger.Info("eBPF not supported on this platform, running in mock mode")

	// Start mock event generator for testing
	if o.config.Enabled {
		go o.generateMockEvents()
	}

	return nil
}

// stopEBPF is a no-op on non-Linux systems
func (o *Observer) stopEBPF() error {
	return nil
}

// generateMockEvents generates mock link failure events for testing
func (o *Observer) generateMockEvents() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	eventTypes := []uint8{EventSYNTimeout, EventConnectionRST}
	currentType := 0

	for {
		select {
		case <-o.LifecycleManager.StopChannel():
			return
		case <-ticker.C:
			// Generate a mock failure event
			srcIP := "192.168.1.100"
			dstIP := "10.0.0.1"

			// Track the failure
			o.trackFailure(srcIP, dstIP, eventTypes[currentType])

			o.logger.Debug("Generated mock link failure",
				zap.String("type", GetEventTypeName(eventTypes[currentType])),
				zap.String("src", srcIP),
				zap.String("dst", dstIP),
			)

			// Rotate event type
			currentType = (currentType + 1) % len(eventTypes)
		}
	}
}
