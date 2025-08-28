//go:build !linux

package oom

import (
	"context"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// Collector monitors OOM events (Linux only)
type Collector struct {
	name   string
	logger *zap.Logger
	events chan *domain.CollectorEvent
}

// NewCollector creates a new OOM collector (stub for non-Linux)
func NewCollector(name string, cfg collectors.CollectorConfig, logger *zap.Logger) (collectors.Collector, error) {
	logger.Warn("OOM collector is only supported on Linux, returning stub implementation")
	return &Collector{
		name:   name,
		logger: logger,
		events: make(chan *domain.CollectorEvent),
	}, nil
}

func (c *Collector) Name() string {
	return c.name
}

func (c *Collector) Start(ctx context.Context) error {
	c.logger.Warn("OOM collector not supported on this platform")
	return nil
}

func (c *Collector) Stop() error {
	return nil
}

func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

func (c *Collector) IsHealthy() bool {
	return false
}

// CreateCollector is the factory function for registry
func CreateCollector(config collectors.CollectorConfig) (collectors.Collector, error) {
	logger := zap.NewNop()
	return NewCollector("oom", config, logger)
}
