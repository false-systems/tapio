package runtime

import (
	"context"
	"sync"
	"time"
)

// HealthChecker monitors observer health
type HealthChecker struct {
	config       HealthConfig
	observerName string

	mu        sync.RWMutex
	healthy   bool
	reason    string
	lastCheck time.Time
}

// HealthStatus represents the current health status
type HealthStatus struct {
	Healthy      bool
	Reason       string
	ObserverName string
	LastCheck    time.Time
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(config HealthConfig, observerName string) *HealthChecker {
	return &HealthChecker{
		config:       config,
		observerName: observerName,
		healthy:      true, // Start healthy
		lastCheck:    time.Now(),
	}
}

// IsHealthy returns true if observer is currently healthy
func (h *HealthChecker) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.healthy
}

// MarkUnhealthy marks the observer as unhealthy with a reason
func (h *HealthChecker) MarkUnhealthy(reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.healthy = false
	h.reason = reason
	h.lastCheck = time.Now()
}

// MarkHealthy marks the observer as healthy
func (h *HealthChecker) MarkHealthy() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.healthy = true
	h.reason = ""
	h.lastCheck = time.Now()
}

// LastCheck returns the time of the last health check
func (h *HealthChecker) LastCheck() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastCheck
}

// GetStatus returns the current health status
func (h *HealthChecker) GetStatus() HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return HealthStatus{
		Healthy:      h.healthy,
		Reason:       h.reason,
		ObserverName: h.observerName,
		LastCheck:    h.lastCheck,
	}
}

// Run starts the health checker and blocks until context is cancelled
func (h *HealthChecker) Run(ctx context.Context) {
	ticker := time.NewTicker(h.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Update last check time
			h.mu.Lock()
			h.lastCheck = time.Now()
			h.mu.Unlock()
		}
	}
}
