package base

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// SetHealthy sets the observer health status
func (bc *BaseObserver) SetHealthy(healthy bool) {
	bc.isHealthy.Store(healthy)
}

// IsHealthy returns true if the observer is healthy
func (bc *BaseObserver) IsHealthy() bool {
	return bc.isHealthy.Load()
}

// Statistics returns observer statistics (implements ObserverWithStats)
func (bc *BaseObserver) Statistics() *domain.CollectorStats {
	lastEventTime := time.Time{}
	if t, ok := bc.lastEventTime.Load().(time.Time); ok {
		lastEventTime = t
	}

	customMetrics := map[string]string{
		"events_dropped":  fmt.Sprintf("%d", bc.eventsDropped.Load()),
		"events_filtered": fmt.Sprintf("%d", bc.eventsFiltered.Load()),
	}

	// Add ring buffer stats if enabled
	if rbStats := bc.GetRingBufferStats(); rbStats != nil {
		customMetrics["ring_buffer_capacity"] = fmt.Sprintf("%d", rbStats.Capacity)
		customMetrics["ring_buffer_produced"] = fmt.Sprintf("%d", rbStats.Produced)
		customMetrics["ring_buffer_consumed"] = fmt.Sprintf("%d", rbStats.Consumed)
		customMetrics["ring_buffer_dropped"] = fmt.Sprintf("%d", rbStats.Dropped)
		customMetrics["ring_buffer_utilization"] = fmt.Sprintf("%.2f%%", rbStats.Utilization)
		customMetrics["ring_buffer_consumers"] = fmt.Sprintf("%d", rbStats.Consumers)
	}

	// Add filter stats if enabled
	if filterStats := bc.GetFilterStatistics(); filterStats != nil {
		customMetrics["filter_version"] = fmt.Sprintf("%d", filterStats.Version)
		customMetrics["filter_allow_count"] = fmt.Sprintf("%d", filterStats.AllowFilters)
		customMetrics["filter_deny_count"] = fmt.Sprintf("%d", filterStats.DenyFilters)
		customMetrics["filter_events_processed"] = fmt.Sprintf("%d", filterStats.EventsProcessed)
		customMetrics["filter_events_allowed"] = fmt.Sprintf("%d", filterStats.EventsAllowed)
		customMetrics["filter_events_denied"] = fmt.Sprintf("%d", filterStats.EventsDenied)
	}

	return &domain.CollectorStats{
		EventsProcessed: bc.eventsProcessed.Load(),
		ErrorCount:      bc.errorCount.Load(),
		LastEventTime:   lastEventTime,
		Uptime:          time.Since(bc.startTime),
		CustomMetrics:   customMetrics,
	}
}

// Health returns health status (implements ObserverWithStats)
func (bc *BaseObserver) Health() *domain.HealthStatus {
	if !bc.isHealthy.Load() {
		var lastErr error
		if e := bc.lastError.Load(); e != nil {
			lastErr = e.(error)
		}
		return domain.NewUnhealthyStatus(
			fmt.Sprintf("%s observer is unhealthy", bc.name),
			lastErr,
		)
	}

	// Check if we're receiving events (only if we've processed at least one)
	if bc.eventsProcessed.Load() > 0 {
		lastEventTime := time.Time{}
		if t, ok := bc.lastEventTime.Load().(time.Time); ok {
			lastEventTime = t
		}

		timeSinceLastEvent := time.Since(lastEventTime)
		if timeSinceLastEvent > bc.healthCheckTimeout {
			return domain.NewHealthStatus(
				domain.HealthDegraded,
				fmt.Sprintf("No events received for %v", timeSinceLastEvent),
			)
		}
	}

	// Check error rate
	errorRate := float64(0)
	if processed := bc.eventsProcessed.Load(); processed > 0 {
		errorRate = float64(bc.errorCount.Load()) / float64(processed)
	}

	if errorRate > bc.errorRateThreshold {
		// Update health gauge
		if bc.healthStatus != nil {
			bc.healthStatus.Record(context.Background(), 1, // 1 = degraded
				metric.WithAttributes(attribute.String("reason", "high_error_rate")))
		}
		return domain.NewHealthStatus(
			domain.HealthDegraded,
			fmt.Sprintf("High error rate: %.1f%% (threshold: %.1f%%)",
				errorRate*100, bc.errorRateThreshold*100),
		)
	}

	// Update health gauge to healthy
	if bc.healthStatus != nil {
		bc.healthStatus.Record(context.Background(), 2) // 2 = healthy
	}

	return domain.NewHealthyStatus(fmt.Sprintf("%s observer operating normally", bc.name))
}

// GetName returns the observer name
func (bc *BaseObserver) GetName() string {
	return bc.name
}

// GetUptime returns how long the observer has been running
func (bc *BaseObserver) GetUptime() time.Duration {
	return time.Since(bc.startTime)
}

// GetEventCount returns the total number of events processed
func (bc *BaseObserver) GetEventCount() int64 {
	return bc.eventsProcessed.Load()
}

// GetErrorCount returns the total number of errors
func (bc *BaseObserver) GetErrorCount() int64 {
	return bc.errorCount.Load()
}

// GetDroppedCount returns the total number of dropped events
func (bc *BaseObserver) GetDroppedCount() int64 {
	return bc.eventsDropped.Load()
}
