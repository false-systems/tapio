//go:build !linux

package oom

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Interface verification
var _ collectors.Collector = (*StubCollector)(nil)

// StubCollector is a no-op collector for non-Linux platforms
type StubCollector struct {
	name   string
	logger *zap.Logger
	events chan *domain.CollectorEvent
}

// NewCollector creates a stub collector for non-Linux platforms
func NewCollector(name string, config *OOMConfig, logger *zap.Logger) (*StubCollector, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	collector := &StubCollector{
		name:   name,
		logger: logger,
		events: make(chan *domain.CollectorEvent),
	}

	// Close the channel immediately since we won't send any events
	close(collector.events)

	logger.Warn("OOM collector is not supported on this platform",
		zap.String("collector", name),
		zap.String("platform", "non-linux"))

	return collector, nil
}

// Name returns the collector name
func (c *StubCollector) Name() string {
	return c.name
}

// Start does nothing on non-Linux platforms
func (c *StubCollector) Start(ctx context.Context) error {
	c.logger.Info("OOM collector stub started (no-op)",
		zap.String("collector", c.name))
	return nil
}

// Stop does nothing on non-Linux platforms
func (c *StubCollector) Stop() error {
	c.logger.Info("OOM collector stub stopped",
		zap.String("collector", c.name))
	return nil
}

// Events returns the closed events channel
func (c *StubCollector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy always returns true for stub collector
func (c *StubCollector) IsHealthy() bool {
	return true
}
