//go:build !linux
// +build !linux

package criebpf

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Collector is a stub implementation for non-Linux platforms
type Collector struct {
	name string
}

// NewCollector creates a stub collector that always returns an error
func NewCollector(name string, cfg *Config) (*Collector, error) {
	return nil, fmt.Errorf("CRI eBPF collector is only supported on Linux")
}

// Name returns collector name (stub)
func (c *Collector) Name() string {
	return c.name
}

// Start always returns an error on non-Linux platforms
func (c *Collector) Start(ctx context.Context) error {
	return fmt.Errorf("CRI eBPF collector is only supported on Linux")
}

// Stop is a no-op on non-Linux platforms
func (c *Collector) Stop() error {
	return nil
}

// Events returns nil channel on non-Linux platforms
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return nil
}

// IsHealthy always returns false on non-Linux platforms
func (c *Collector) IsHealthy() bool {
	return false
}

// UpdateContainerMetadata is a no-op on non-Linux platforms
func (c *Collector) UpdateContainerMetadata(containerID string, meta *ContainerMetadata) {
	// No-op
}
