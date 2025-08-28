//go:build !linux

package systemdapi

import (
	"context"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// Collector monitors systemd via journal API (Linux only)
type Collector struct {
	name   string
	logger *zap.Logger
	events chan *domain.CollectorEvent
	config *Config
}

// Config represents systemd API collector configuration
type Config struct {
	ServiceFilter  []string `json:"service_filter"`
	PriorityFilter []string `json:"priority_filter"`
	EnableMetrics  bool     `json:"enable_metrics"`
	BufferSize     int      `json:"buffer_size"`
	FollowMode     bool     `json:"follow_mode"`
}

// NewCollector creates a new systemd API collector (stub for non-Linux)
func NewCollector(name string, config *Config, logger *zap.Logger) (*Collector, error) {
	logger.Warn("systemd API collector is only supported on Linux, returning stub implementation")
	return &Collector{
		name:   name,
		config: config,
		logger: logger,
		events: make(chan *domain.CollectorEvent),
	}, nil
}

func (c *Collector) Name() string {
	return c.name
}

func (c *Collector) Start(ctx context.Context) error {
	c.logger.Warn("systemd API collector not supported on this platform")
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
func CreateCollector(config *Config) (collectors.Collector, error) {
	logger := zap.NewNop()
	return NewCollector("systemd-api", config, logger)
}
