//go:build !linux

package otel

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// Collector stub for non-Linux platforms
type Collector struct {
	name   string
	config *Config
	logger *zap.Logger
	events chan *domain.CollectorEvent
}

// Interface verification
var _ collectors.Collector = (*Collector)(nil)

// NewCollector creates a stub collector for non-Linux platforms
func NewCollector(name string, config *Config) (*Collector, error) {
	if config == nil {
		config = DefaultConfig()
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	collector := &Collector{
		name:   name,
		config: config,
		logger: logger,
		events: make(chan *domain.CollectorEvent),
	}

	// Close the channel immediately since we won't send any events
	close(collector.events)

	logger.Warn("OTEL collector is only supported on Linux",
		zap.String("collector", name),
		zap.String("platform", "non-linux"))

	return collector, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.name
}

// Start does nothing on non-Linux platforms
func (c *Collector) Start(ctx context.Context) error {
	c.logger.Info("OTEL collector stub started (no-op)",
		zap.String("collector", c.name))
	return fmt.Errorf("OTEL collector requires Linux")
}

// Stop does nothing on non-Linux platforms
func (c *Collector) Stop() error {
	c.logger.Info("OTEL collector stub stopped",
		zap.String("collector", c.name))
	return nil
}

// Events returns the closed events channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy always returns false for stub collector
func (c *Collector) IsHealthy() bool {
	return false
}
