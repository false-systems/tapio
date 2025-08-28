//go:build !linux
// +build !linux

package syscallerrors

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// Config holds collector configuration
type Config struct {
	RingBufferSize    int
	EventChannelSize  int
	RateLimitMs       int
	EnabledCategories map[string]bool
	RequireAllMetrics bool
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		RingBufferSize:   8 * 1024 * 1024, // 8MB
		EventChannelSize: 10000,
		RateLimitMs:      100,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
		},
		RequireAllMetrics: false,
	}
}

// Collector implements a stub syscall error collector for non-Linux systems
type Collector struct {
	name      string
	logger    *zap.Logger
	eventChan chan *domain.ObservationEvent
	stopped   bool
}

// Config holds collector configuration
type Config struct {
	RingBufferSize    int
	EventChannelSize  int
	RateLimitMs       int
	EnabledCategories map[string]bool // Map for O(1) lookup
	RequireAllMetrics bool            // If true, fail startup when metrics can't be created
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		RingBufferSize:   8 * 1024 * 1024,
		EventChannelSize: 10000,
		RateLimitMs:      100,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
		},
		RequireAllMetrics: false,
	}
}

// NewCollector creates a stub collector for non-Linux systems
func NewCollector(logger *zap.Logger, config *Config) (*Collector, error) {
	if config == nil {
		config = DefaultConfig()
	}

	logger.Warn("Syscall error collector is not supported on this platform")

	return &Collector{
		name:      "syscall-errors",
		logger:    logger,
		eventChan: make(chan *domain.ObservationEvent),
	}, nil
}

// Start begins collecting (no-op on non-Linux)
func (c *Collector) Start(ctx context.Context) error {
	c.logger.Warn("Syscall error collector Start() called on non-Linux platform")
	return fmt.Errorf("syscall error collector is only supported on Linux")
}

// Stop stops the collector
func (c *Collector) Stop() error {
	if !c.stopped {
		close(c.eventChan)
		c.stopped = true
	}
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

// IsHealthy checks if the collector is healthy
func (c *Collector) IsHealthy() bool {
	return !c.stopped
}
