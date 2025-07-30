//go:build !linux
// +build !linux

package systemd

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// MinimalSystemdCollector stub for non-Linux platforms
type MinimalSystemdCollector struct {
	config collectors.CollectorConfig
	events chan collectors.RawEvent
}

// NewMinimalSystemdCollector creates a stub collector on non-Linux platforms
func NewMinimalSystemdCollector(config collectors.CollectorConfig) (*MinimalSystemdCollector, error) {
	return nil, fmt.Errorf("systemd collector is only supported on Linux")
}

// Name returns the collector name
func (c *MinimalSystemdCollector) Name() string {
	return "systemd-minimal-stub"
}

// Start returns an error on non-Linux platforms
func (c *MinimalSystemdCollector) Start(ctx context.Context) error {
	return fmt.Errorf("systemd collector is only supported on Linux")
}

// Stop is a no-op on non-Linux platforms
func (c *MinimalSystemdCollector) Stop() error {
	return nil
}

// Events returns an empty channel on non-Linux platforms
func (c *MinimalSystemdCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns false on non-Linux platforms
func (c *MinimalSystemdCollector) IsHealthy() bool {
	return false
}

// DefaultSystemdConfig returns default configuration
func DefaultSystemdConfig() collectors.CollectorConfig {
	return collectors.CollectorConfig{
		BufferSize:     1000,
		MetricsEnabled: true,
		Labels: map[string]string{
			"collector": "systemd-minimal",
		},
	}
}