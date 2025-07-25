package collector

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Collector defines the interface all collectors must implement
type Collector interface {
	Start(ctx context.Context) error
	Stop() error
	Events() <-chan domain.UnifiedEvent
	Health() domain.HealthStatus
}

// CollectorManager orchestrates multiple collectors and aggregates their events
type CollectorManager struct {
	collectors    map[string]Collector
	eventChan     chan domain.UnifiedEvent
	totalEvents   int64
	droppedEvents int64
	ctx           context.Context
	cancel        context.CancelFunc
}

// Statistics provides real-time collector metrics
type Statistics struct {
	ActiveCollectors int
	TotalEvents      int64
	DroppedEvents    int64
	EventRate        float64
}

// NewCollectorManager creates a fully operational collector manager
func NewCollectorManager() *CollectorManager {
	return &CollectorManager{
		collectors: make(map[string]Collector),
		eventChan:  make(chan domain.UnifiedEvent, 10000),
	}
}

// AddCollector registers a collector with the manager
func (cm *CollectorManager) AddCollector(name string, collector Collector) {
	if cm.collectors == nil {
		cm.collectors = make(map[string]Collector)
	}
	cm.collectors[name] = collector
}

// Start activates all registered collectors and begins event aggregation
func (cm *CollectorManager) Start(ctx context.Context) error {
	cm.ctx, cm.cancel = context.WithCancel(ctx)

	if len(cm.collectors) == 0 {
		return fmt.Errorf("no collectors registered")
	}

	for name, collector := range cm.collectors {
		if err := collector.Start(cm.ctx); err != nil {
			return fmt.Errorf("collector %s start failed: %w", name, err)
		}

		go cm.routeCollectorEvents(name, collector)
	}

	return nil
}

// routeCollectorEvents routes events from individual collector to manager channel
func (cm *CollectorManager) routeCollectorEvents(name string, collector Collector) {
	for {
		select {
		case event, ok := <-collector.Events():
			if !ok {
				log.Printf("Collector %s event channel closed", name)
				return
			}

			select {
			case cm.eventChan <- event:
				atomic.AddInt64(&cm.totalEvents, 1)
			default:
				atomic.AddInt64(&cm.droppedEvents, 1)
				log.Printf("⚠️  Event dropped from %s collector - buffer full", name)
			}

		case <-cm.ctx.Done():
			return
		}
	}
}

// Stop gracefully shuts down all collectors
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

// Events returns the aggregated event channel from all collectors
func (cm *CollectorManager) Events() <-chan domain.UnifiedEvent {
	return cm.eventChan
}

// Statistics returns comprehensive collector performance metrics
func (cm *CollectorManager) Statistics() Statistics {
	totalEvents := atomic.LoadInt64(&cm.totalEvents)
	droppedEvents := atomic.LoadInt64(&cm.droppedEvents)

	eventRate := 0.0
	if totalEvents > 0 {
		eventRate = float64(totalEvents-droppedEvents) / float64(totalEvents) * 100
	}

	return Statistics{
		ActiveCollectors: len(cm.collectors),
		TotalEvents:      totalEvents,
		DroppedEvents:    droppedEvents,
		EventRate:        eventRate,
	}
}

// Health returns aggregated health status of all collectors
func (cm *CollectorManager) Health() domain.HealthStatus {
	if len(cm.collectors) == 0 {
		return domain.NewHealthStatus(domain.HealthUnhealthy, "No collectors registered", nil)
	}

	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0
	details := make(map[string]interface{})
	collectorStatuses := make(map[string]string)

	for name, collector := range cm.collectors {
		health := collector.Health()
		status := health.Status()
		collectorStatuses[name] = string(status)

		switch status {
		case domain.HealthHealthy:
			healthyCount++
		case domain.HealthDegraded:
			degradedCount++
		case domain.HealthUnhealthy:
			unhealthyCount++
		}
	}

	details["collectors"] = collectorStatuses
	details["healthy_count"] = healthyCount
	details["degraded_count"] = degradedCount
	details["unhealthy_count"] = unhealthyCount
	details["total_count"] = len(cm.collectors)

	var status domain.HealthStatusValue
	var message string

	if healthyCount == len(cm.collectors) {
		status = domain.HealthHealthy
		message = fmt.Sprintf("All %d collectors are healthy", len(cm.collectors))
	} else if healthyCount > 0 || degradedCount > 0 {
		status = domain.HealthDegraded
		message = fmt.Sprintf("System degraded: %d healthy, %d degraded, %d unhealthy collectors",
			healthyCount, degradedCount, unhealthyCount)
	} else {
		status = domain.HealthUnhealthy
		message = fmt.Sprintf("All %d collectors are unhealthy", len(cm.collectors))
	}

	return domain.NewHealthStatus(status, message, details)
}
