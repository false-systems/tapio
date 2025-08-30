// Package base provides common functionality for all Tapio collectors
// This reduces code duplication and ensures consistent observability
package base

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// BaseCollector provides common statistics and health tracking for all collectors
// Embed this in your collector to get Statistics() and Health() methods automatically
type BaseCollector struct {
	// Basic info
	name      string
	startTime time.Time

	// Statistics tracking (atomic for thread safety)
	eventsProcessed atomic.Int64
	eventsDropped   atomic.Int64
	errorCount      atomic.Int64
	
	// Atomic values for complex types  
	lastEventTime atomic.Value // stores time.Time
	lastError     atomic.Value // stores error

	// Health tracking
	isHealthy          atomic.Bool
	healthCheckTimeout time.Duration
}

// NewBaseCollector creates a new base collector with the given name
// healthCheckTimeout determines how long without events before marking degraded
func NewBaseCollector(name string, healthCheckTimeout time.Duration) *BaseCollector {
	bc := &BaseCollector{
		name:               name,
		startTime:          time.Now(),
		healthCheckTimeout: healthCheckTimeout,
	}
	bc.isHealthy.Store(true)
	bc.lastEventTime.Store(time.Now())
	return bc
}

// RecordEvent should be called when an event is successfully processed
func (bc *BaseCollector) RecordEvent() {
	bc.eventsProcessed.Add(1)
	bc.lastEventTime.Store(time.Now())
}

// RecordError should be called when an error occurs
func (bc *BaseCollector) RecordError(err error) {
	bc.errorCount.Add(1)
	if err != nil {
		bc.lastError.Store(err)
	}
}

// RecordDrop should be called when an event is dropped
func (bc *BaseCollector) RecordDrop() {
	bc.eventsDropped.Add(1)
}

// SetHealthy sets the collector health status
func (bc *BaseCollector) SetHealthy(healthy bool) {
	bc.isHealthy.Store(healthy)
}

// IsHealthy returns true if the collector is healthy
func (bc *BaseCollector) IsHealthy() bool {
	return bc.isHealthy.Load()
}

// Statistics returns collector statistics (implements CollectorWithStats)
func (bc *BaseCollector) Statistics() *domain.CollectorStats {
	lastEventTime := time.Time{}
	if t, ok := bc.lastEventTime.Load().(time.Time); ok {
		lastEventTime = t
	}

	return &domain.CollectorStats{
		EventsProcessed: bc.eventsProcessed.Load(),
		ErrorCount:      bc.errorCount.Load(),
		LastEventTime:   lastEventTime,
		Uptime:          time.Since(bc.startTime),
		CustomMetrics: map[string]string{
			"events_dropped": fmt.Sprintf("%d", bc.eventsDropped.Load()),
		},
	}
}

// Health returns health status (implements CollectorWithStats)
func (bc *BaseCollector) Health() *domain.HealthStatus {
	if !bc.isHealthy.Load() {
		var lastErr error
		if e := bc.lastError.Load(); e != nil {
			lastErr = e.(error)
		}
		return domain.NewUnhealthyStatus(
			fmt.Sprintf("%s collector is unhealthy", bc.name),
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

	if errorRate > 0.1 { // More than 10% errors
		return domain.NewHealthStatus(
			domain.HealthDegraded,
			fmt.Sprintf("High error rate: %.1f%%", errorRate*100),
		)
	}

	return domain.NewHealthyStatus(fmt.Sprintf("%s collector operating normally", bc.name))
}

// GetName returns the collector name
func (bc *BaseCollector) GetName() string {
	return bc.name
}

// GetUptime returns how long the collector has been running
func (bc *BaseCollector) GetUptime() time.Duration {
	return time.Since(bc.startTime)
}

// GetEventCount returns the total number of events processed
func (bc *BaseCollector) GetEventCount() int64 {
	return bc.eventsProcessed.Load()
}

// GetErrorCount returns the total number of errors
func (bc *BaseCollector) GetErrorCount() int64 {
	return bc.errorCount.Load()
}

// GetDroppedCount returns the total number of dropped events
func (bc *BaseCollector) GetDroppedCount() int64 {
	return bc.eventsDropped.Load()
}