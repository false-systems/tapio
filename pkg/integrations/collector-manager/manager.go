package manager

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CollectorManager manages multiple collectors (L3: Integration Layer)
type CollectorManager struct {
	collectors map[string]Collector
	eventChan  chan domain.UnifiedEvent // Modern UnifiedEvent with rich semantic context
	ctx        context.Context
	cancel     context.CancelFunc
}

// Collector interface for all collector types (modernized for UnifiedEvent)
type Collector interface {
	Start(ctx context.Context) error
	Stop() error
	Events() <-chan domain.UnifiedEvent // Modern UnifiedEvent with rich semantic context
	Health() CollectorHealth            // Rich health interface
	Statistics() CollectorStatistics    // Monitoring and metrics
}

// CollectorHealth provides detailed health information
type CollectorHealth interface {
	Status() string
	IsHealthy() bool
	LastEventTime() time.Time
	ErrorCount() uint64
	Metrics() map[string]float64
}

// CollectorStatistics provides runtime statistics
type CollectorStatistics interface {
	EventsProcessed() uint64
	EventsDropped() uint64
	StartTime() time.Time
	Custom() map[string]interface{}
}

// NewCollectorManager creates a new collector manager
func NewCollectorManager() *CollectorManager {
	return &CollectorManager{
		collectors: make(map[string]Collector),
		eventChan:  make(chan domain.UnifiedEvent, 10000), // UnifiedEvent channel
	}
}

// AddCollector adds a collector to the manager
func (cm *CollectorManager) AddCollector(name string, collector Collector) {
	cm.collectors[name] = collector
}

// Start starts all collectors
func (cm *CollectorManager) Start(ctx context.Context) error {
	cm.ctx, cm.cancel = context.WithCancel(ctx)

	// Start all collectors
	for name, collector := range cm.collectors {
		if err := collector.Start(cm.ctx); err != nil {
			return fmt.Errorf("failed to start %s collector: %w", name, err)
		}

		// Route events from collector to manager channel
		go func(name string, c Collector) {
			for event := range c.Events() {
				select {
				case cm.eventChan <- event:
				case <-cm.ctx.Done():
					return
				}
			}
		}(name, collector)
	}

	return nil
}

// Stop stops all collectors
func (cm *CollectorManager) Stop() {
	if cm.cancel != nil {
		cm.cancel()
	}

	for name, collector := range cm.collectors {
		if err := collector.Stop(); err != nil {
			log.Printf("Error stopping %s collector: %v", name, err)
		}
	}

	close(cm.eventChan)
}

// Events returns the merged event channel with rich UnifiedEvent data
func (cm *CollectorManager) Events() <-chan domain.UnifiedEvent {
	return cm.eventChan
}

// Statistics returns collector statistics
func (cm *CollectorManager) Statistics() struct {
	ActiveCollectors int
	TotalEvents      int64
} {
	// Aggregate events from all collectors
	var totalEvents int64
	var activeCollectors int
	for _, collector := range cm.collectors {
		if collector != nil {
			activeCollectors++
			if stats := collector.Statistics(); stats != nil {
				totalEvents += int64(stats.EventsProcessed())
			}
		}
	}

	return struct {
		ActiveCollectors int
		TotalEvents      int64
	}{
		ActiveCollectors: activeCollectors,
		TotalEvents:      totalEvents,
	}
}
