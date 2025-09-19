//go:build !linux
// +build !linux

package status

import (
	"time"
)

// startEBPF is a no-op on non-Linux systems
func (o *Observer) startEBPF() error {
	o.logger.Info("eBPF not supported on this platform, running in limited mode")

	// Start a mock event generator for testing
	if o.config.Enabled {
		go o.generateMockEvents()
	}

	return nil
}

// stopEBPF is a no-op on non-Linux systems
func (o *Observer) stopEBPF() error {
	return nil
}

// generateMockEvents generates mock status events for testing
func (o *Observer) generateMockEvents() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.StopChannel():
			return
		case <-ticker.C:
			// Generate a mock status event
			event := &StatusEvent{
				ServiceHash:  12345,
				EndpointHash: 67890,
				StatusCode:   200,
				ErrorType:    ErrorNone,
				Timestamp:    uint64(time.Now().UnixNano()),
				Latency:      uint32(100), // 100us
				PID:          uint32(1234),
			}

			// Add to aggregator
			o.aggregator.Add(event)

			o.logger.Debug("Generated mock status event")
		}
	}
}
