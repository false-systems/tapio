//go:build !linux
// +build !linux

package network

import (
	"context"
	"fmt"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// IntelligenceCollector stub for non-Linux platforms
type IntelligenceCollector struct {
	*Collector // Embed base collector
	name       string
	logger     *zap.Logger
	events     chan *domain.CollectorEvent
}

// NewIntelligenceCollector creates a stub collector for non-Linux platforms
func NewIntelligenceCollector(name string, config *IntelligenceCollectorConfig, logger *zap.Logger) (*IntelligenceCollector, error) {
	// Create base collector
	baseCollector, err := NewCollector(name, config.NetworkCollectorConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("creating base network collector: %w", err)
	}

	return &IntelligenceCollector{
		Collector: baseCollector,
		name:      name,
		logger:    logger,
		events:    make(chan *domain.CollectorEvent, 100),
	}, nil
}

// Start is a stub implementation for non-Linux platforms
func (ic *IntelligenceCollector) Start(ctx context.Context) error {
	ic.logger.Warn("Network intelligence collector not supported on this platform")
	return ic.Collector.Start(ctx)
}

// Stop is a stub implementation for non-Linux platforms
func (ic *IntelligenceCollector) Stop() error {
	ic.logger.Debug("Network intelligence collector stop called (stub)")
	return ic.Collector.Stop()
}

// GetIntelligenceStats returns empty stats for stub
func (ic *IntelligenceCollector) GetIntelligenceStats() *IntelligenceCollectorStats {
	return &IntelligenceCollectorStats{
		EventsProcessed:     0,
		DependenciesFound:   0,
		ErrorPatternsFound:  0,
		LatencyAnomalies:    0,
		DNSFailures:         0,
		SecurityConcerns:    0,
		FilteringEfficiency: 0.0,
	}
}

// GetServiceDependencies returns empty deps for stub
func (ic *IntelligenceCollector) GetServiceDependencies() map[string]*ServiceDependency {
	return make(map[string]*ServiceDependency)
}
