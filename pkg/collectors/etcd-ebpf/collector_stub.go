//go:build !linux

package etcdebpf

import (
	"context"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// Collector monitors etcd operations via eBPF (Linux only)
type Collector struct {
	name   string
	logger *zap.Logger
	events chan *domain.CollectorEvent
}

// NewCollector creates a new etcd eBPF collector (stub for non-Linux)
func NewCollector(name string, cfg collectors.CollectorConfig, logger *zap.Logger) (collectors.Collector, error) {
	logger.Warn("etcd eBPF collector is only supported on Linux, returning stub implementation")
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
	c.logger.Warn("etcd eBPF collector not supported on this platform")
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

// Config represents etcd eBPF collector configuration (stub)
type Config struct {
	EtcdPort           int                `json:"etcd_port"`
	EnableWatchEvents  bool               `json:"enable_watch_events"`
	EnableRangeQueries bool               `json:"enable_range_queries"`
	EnableTransactions bool               `json:"enable_transactions"`
	TrackedKeys        []string           `json:"tracked_keys"`
	RateLimiting       RateLimitingConfig `json:"rate_limiting"`
}

type RateLimitingConfig struct {
	Enabled         bool `json:"enabled"`
	EventsPerSecond int  `json:"events_per_second"`
}

// CreateCollector is the factory function for registry
func CreateCollector(config *Config) (collectors.Collector, error) {
	logger := zap.NewNop()
	return NewCollector("etcd-ebpf", collectors.CollectorConfig{}, logger)
}
