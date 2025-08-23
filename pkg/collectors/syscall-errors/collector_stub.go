//go:build !linux
// +build !linux

package syscallerrors

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// Collector stub for non-Linux systems
type Collector struct {
	name      string
	logger    *zap.Logger
	eventChan chan *domain.ObservationEvent
}

// Config holds collector configuration
type Config struct {
	RingBufferSize    int
	EventChannelSize  int
	RateLimitMs       int
	EnabledCategories []string
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		RingBufferSize:    8 * 1024 * 1024,
		EventChannelSize:  10000,
		RateLimitMs:       100,
		EnabledCategories: []string{"file", "network", "memory"},
	}
}

// NewCollector creates a stub collector for non-Linux systems
func NewCollector(logger *zap.Logger, config *Config) (*Collector, error) {
	if config == nil {
		config = DefaultConfig()
	}

	return &Collector{
		name:      "syscall-errors",
		logger:    logger,
		eventChan: make(chan *domain.ObservationEvent),
	}, nil
}

// Start is a no-op on non-Linux systems
func (c *Collector) Start(ctx context.Context) error {
	c.logger.Warn("Syscall error collector is only supported on Linux")
	return fmt.Errorf("syscall error collector requires Linux with eBPF support")
}

// Stop is a no-op on non-Linux systems
func (c *Collector) Stop() error {
	close(c.eventChan)
	return nil
}

// GetEventChannel returns the event channel
func (c *Collector) GetEventChannel() <-chan *domain.ObservationEvent {
	return c.eventChan
}

// GetName returns the collector name
func (c *Collector) GetName() string {
	return c.name
}

// IsHealthy always returns false on non-Linux systems
func (c *Collector) IsHealthy() bool {
	return false
}
