package noderuntime

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EndpointCollector defines the interface for kubelet endpoint collectors.
// Each collector is responsible for monitoring one kubelet API endpoint
// and emitting domain events based on the collected data.
type EndpointCollector interface {
	// Name returns the collector's name (e.g., "stats", "pods", "healthz")
	Name() string

	// Endpoint returns the kubelet API endpoint path (e.g., "/stats/summary")
	Endpoint() string

	// Collect fetches data from the endpoint and returns domain events.
	// Returns nil if no events need to be emitted.
	Collect(ctx context.Context) ([]domain.CollectorEvent, error)

	// IsHealthy checks if the collector is functioning properly
	IsHealthy() bool
}

// BaseCollector provides common functionality for all endpoint collectors
type BaseCollector struct {
	name         string
	endpoint     string
	observerName string
	healthy      bool
}

// NewBaseCollector creates a new BaseCollector with common fields
func NewBaseCollector(name, endpoint, observerName string) *BaseCollector {
	return &BaseCollector{
		name:         name,
		endpoint:     endpoint,
		observerName: observerName,
		healthy:      true,
	}
}

// Name returns the collector's name
func (bc *BaseCollector) Name() string {
	return bc.name
}

// Endpoint returns the kubelet API endpoint path
func (bc *BaseCollector) Endpoint() string {
	return bc.endpoint
}

// IsHealthy returns the collector's health status
func (bc *BaseCollector) IsHealthy() bool {
	return bc.healthy
}

// SetHealthy updates the collector's health status
func (bc *BaseCollector) SetHealthy(healthy bool) {
	bc.healthy = healthy
}
