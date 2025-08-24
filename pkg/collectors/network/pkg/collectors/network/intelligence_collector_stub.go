//go:build !linux
// +build !linux

package network

import (
	"context"

	"go.uber.org/zap"
)

// IntelligenceCollector stub for non-Linux platforms
type IntelligenceCollector struct {
	name   string
	logger *zap.Logger
}

// IntelligenceCollectorConfig stub for non-Linux platforms
type IntelligenceCollectorConfig struct {
	// Placeholder config
	Enabled bool
}

// NewIntelligenceCollector creates a stub collector for non-Linux platforms
func NewIntelligenceCollector(name string, config *IntelligenceCollectorConfig, logger *zap.Logger) (*IntelligenceCollector, error) {
	return &IntelligenceCollector{
		name:   name,
		logger: logger,
	}, nil
}

// DefaultIntelligenceConfig returns default config for stub
func DefaultIntelligenceConfig() *IntelligenceCollectorConfig {
	return &IntelligenceCollectorConfig{
		Enabled: false,
	}
}

// Start is a stub implementation for non-Linux platforms
func (c *IntelligenceCollector) Start(ctx context.Context) error {
	c.logger.Warn("Network intelligence collector not supported on this platform")
	return nil
}

// Stop is a stub implementation for non-Linux platforms
func (c *IntelligenceCollector) Stop() error {
	c.logger.Debug("Network intelligence collector stop called (stub)")
	return nil
}

// Name returns the collector name
func (c *IntelligenceCollector) Name() string {
	return c.name
}

// IsHealthy returns false for stub implementation
func (c *IntelligenceCollector) IsHealthy() bool {
	return false
}

// StubHealthInfo represents health information for stub implementation
type StubHealthInfo struct {
	Status    string `json:"status"`
	Platform  string `json:"platform"`
	Supported bool   `json:"supported"`
}

// StubStatistics represents statistics for stub implementation
type StubStatistics struct {
	EventsProcessed int64  `json:"events_processed"`
	Platform        string `json:"platform"`
	Supported       bool   `json:"supported"`
}

// Health returns stub health information
func (c *IntelligenceCollector) Health() interface{} {
	return &StubHealthInfo{
		Status:    "unsupported",
		Platform:  "non-linux",
		Supported: false,
	}
}

// Statistics returns empty stats for stub
func (c *IntelligenceCollector) Statistics() interface{} {
	return &StubStatistics{
		EventsProcessed: 0,
		Platform:        "non-linux",
		Supported:       false,
	}
}
