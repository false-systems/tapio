package grpc

import (
	"fmt"
	"sync"
	"time"

	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
)

// InMemoryCollectorRegistry implements CollectorRegistry for managing collectors
type InMemoryCollectorRegistry struct {
	logger *zap.Logger

	mu         sync.RWMutex
	collectors map[string]*RegisteredCollector

	// Health check configuration
	healthCheckInterval time.Duration
	healthCheckTimeout  time.Duration

	// Statistics
	registrationCount int64
	healthCheckCount  int64

	// Lifecycle
	shutdown chan struct{}
	wg       sync.WaitGroup
}

// RegisteredCollector represents a registered collector with runtime information
type RegisteredCollector struct {
	Info            CollectorInfo
	RegisteredAt    time.Time
	LastHealthCheck time.Time
	HealthStatus    HealthStatus
	Metrics         map[string]float64
	mu              sync.RWMutex
}

// NewInMemoryCollectorRegistry creates a new collector registry
func NewInMemoryCollectorRegistry(logger *zap.Logger) *InMemoryCollectorRegistry {
	registry := &InMemoryCollectorRegistry{
		logger:              logger,
		collectors:          make(map[string]*RegisteredCollector),
		healthCheckInterval: 30 * time.Second,
		healthCheckTimeout:  5 * time.Second,
		shutdown:            make(chan struct{}),
	}

	// Start health monitoring
	registry.wg.Add(1)
	go registry.healthMonitor()

	return registry
}

// RegisterCollector registers a new collector
func (r *InMemoryCollectorRegistry) RegisterCollector(name string, info CollectorInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.collectors[name]; exists {
		return fmt.Errorf("collector %s already registered", name)
	}

	r.collectors[name] = &RegisteredCollector{
		Info:         info,
		RegisteredAt: time.Now(),
		HealthStatus: HealthStatus{
			Status:      pb.HealthStatus_STATUS_UNKNOWN,
			Message:     "Newly registered",
			LastHealthy: time.Now(),
			Metrics:     make(map[string]float64),
		},
		Metrics: make(map[string]float64),
	}

	r.registrationCount++
	r.logger.Info("Collector registered",
		zap.String("name", name),
		zap.String("type", info.Type),
		zap.String("version", info.Version),
	)

	return nil
}

// GetCollectors returns all registered collectors
func (r *InMemoryCollectorRegistry) GetCollectors() map[string]CollectorInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collectors := make(map[string]CollectorInfo)
	for name, registered := range r.collectors {
		collectors[name] = registered.Info
	}

	return collectors
}

// GetCollectorHealth returns health status for a specific collector
func (r *InMemoryCollectorRegistry) GetCollectorHealth(name string) (HealthStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collector, exists := r.collectors[name]
	if !exists {
		return HealthStatus{}, fmt.Errorf("collector %s not found", name)
	}

	collector.mu.RLock()
	defer collector.mu.RUnlock()

	return collector.HealthStatus, nil
}

// GetCollectorMetrics returns metrics for a specific collector
func (r *InMemoryCollectorRegistry) GetCollectorMetrics(name string) (map[string]float64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collector, exists := r.collectors[name]
	if !exists {
		return nil, fmt.Errorf("collector %s not found", name)
	}

	collector.mu.RLock()
	defer collector.mu.RUnlock()

	// Return a copy of metrics
	metrics := make(map[string]float64)
	for k, v := range collector.Metrics {
		metrics[k] = v
	}

	return metrics, nil
}

// UpdateCollectorHealth updates health status for a collector
func (r *InMemoryCollectorRegistry) UpdateCollectorHealth(name string, status HealthStatus) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collector, exists := r.collectors[name]
	if !exists {
		return fmt.Errorf("collector %s not found", name)
	}

	collector.mu.Lock()
	defer collector.mu.Unlock()

	collector.HealthStatus = status
	collector.LastHealthCheck = time.Now()
	collector.Info.LastSeen = time.Now()

	if status.Status == pb.HealthStatus_STATUS_HEALTHY {
		collector.HealthStatus.LastHealthy = time.Now()
	}

	return nil
}

// UpdateCollectorMetrics updates metrics for a collector
func (r *InMemoryCollectorRegistry) UpdateCollectorMetrics(name string, metrics map[string]float64) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collector, exists := r.collectors[name]
	if !exists {
		return fmt.Errorf("collector %s not found", name)
	}

	collector.mu.Lock()
	defer collector.mu.Unlock()

	// Update metrics
	for k, v := range metrics {
		collector.Metrics[k] = v
	}

	collector.Info.LastSeen = time.Now()

	return nil
}

// UnregisterCollector removes a collector from the registry
func (r *InMemoryCollectorRegistry) UnregisterCollector(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.collectors[name]; !exists {
		return fmt.Errorf("collector %s not found", name)
	}

	delete(r.collectors, name)
	r.logger.Info("Collector unregistered", zap.String("name", name))

	return nil
}

// Health returns registry health status
func (r *InMemoryCollectorRegistry) Health() HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	totalCollectors := len(r.collectors)
	healthyCollectors := 0
	degradedCollectors := 0
	unhealthyCollectors := 0

	for _, collector := range r.collectors {
		collector.mu.RLock()
		switch collector.HealthStatus.Status {
		case pb.HealthStatus_STATUS_HEALTHY:
			healthyCollectors++
		case pb.HealthStatus_STATUS_DEGRADED:
			degradedCollectors++
		case pb.HealthStatus_STATUS_UNHEALTHY:
			unhealthyCollectors++
		}
		collector.mu.RUnlock()
	}

	// Determine overall status
	status := pb.HealthStatus_STATUS_HEALTHY
	message := fmt.Sprintf("Registry healthy: %d collectors registered", totalCollectors)

	if unhealthyCollectors > 0 {
		status = pb.HealthStatus_STATUS_DEGRADED
		message = fmt.Sprintf("Registry degraded: %d unhealthy collectors", unhealthyCollectors)
	}

	if unhealthyCollectors > totalCollectors/2 {
		status = pb.HealthStatus_STATUS_UNHEALTHY
		message = fmt.Sprintf("Registry unhealthy: %d/%d collectors unhealthy", unhealthyCollectors, totalCollectors)
	}

	return HealthStatus{
		Status:      status,
		Message:     message,
		LastHealthy: time.Now(),
		Metrics: map[string]float64{
			"total_collectors":     float64(totalCollectors),
			"healthy_collectors":   float64(healthyCollectors),
			"degraded_collectors":  float64(degradedCollectors),
			"unhealthy_collectors": float64(unhealthyCollectors),
			"registration_count":   float64(r.registrationCount),
			"health_check_count":   float64(r.healthCheckCount),
		},
	}
}

// GetCollectorsByType returns collectors of a specific type
func (r *InMemoryCollectorRegistry) GetCollectorsByType(collectorType string) map[string]CollectorInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collectors := make(map[string]CollectorInfo)
	for name, registered := range r.collectors {
		if registered.Info.Type == collectorType {
			collectors[name] = registered.Info
		}
	}

	return collectors
}

// GetCollectorsByCapability returns collectors with a specific capability
func (r *InMemoryCollectorRegistry) GetCollectorsByCapability(capability string) map[string]CollectorInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collectors := make(map[string]CollectorInfo)
	for name, registered := range r.collectors {
		for _, cap := range registered.Info.Capabilities {
			if cap == capability {
				collectors[name] = registered.Info
				break
			}
		}
	}

	return collectors
}

// healthMonitor periodically checks collector health
func (r *InMemoryCollectorRegistry) healthMonitor() {
	defer r.wg.Done()

	ticker := time.NewTicker(r.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.shutdown:
			return
		case <-ticker.C:
			r.performHealthChecks()
		}
	}
}

// performHealthChecks checks health of all collectors
func (r *InMemoryCollectorRegistry) performHealthChecks() {
	r.mu.RLock()
	collectors := make([]string, 0, len(r.collectors))
	for name := range r.collectors {
		collectors = append(collectors, name)
	}
	r.mu.RUnlock()

	for _, name := range collectors {
		r.checkCollectorHealth(name)
		r.healthCheckCount++
	}
}

// checkCollectorHealth checks health of a single collector
func (r *InMemoryCollectorRegistry) checkCollectorHealth(name string) {
	r.mu.RLock()
	collector, exists := r.collectors[name]
	r.mu.RUnlock()

	if !exists {
		return
	}

	collector.mu.Lock()
	defer collector.mu.Unlock()

	// Check if collector has been seen recently
	timeSinceLastSeen := time.Since(collector.Info.LastSeen)

	if timeSinceLastSeen > 2*r.healthCheckInterval {
		// Collector hasn't reported in too long
		collector.HealthStatus.Status = pb.HealthStatus_STATUS_UNHEALTHY
		collector.HealthStatus.Message = fmt.Sprintf("No updates for %.0f seconds", timeSinceLastSeen.Seconds())
	} else if timeSinceLastSeen > r.healthCheckInterval {
		// Collector is late but not critically
		collector.HealthStatus.Status = pb.HealthStatus_STATUS_DEGRADED
		collector.HealthStatus.Message = fmt.Sprintf("Late update: %.0f seconds ago", timeSinceLastSeen.Seconds())
	}

	collector.LastHealthCheck = time.Now()
}

// Close shuts down the registry
func (r *InMemoryCollectorRegistry) Close() error {
	close(r.shutdown)
	r.wg.Wait()
	return nil
}

// GetStatistics returns registry statistics
func (r *InMemoryCollectorRegistry) GetStatistics() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := map[string]interface{}{
		"total_collectors":      len(r.collectors),
		"registration_count":    r.registrationCount,
		"health_check_count":    r.healthCheckCount,
		"health_check_interval": r.healthCheckInterval.String(),
	}

	// Count by type
	typeCount := make(map[string]int)
	for _, collector := range r.collectors {
		typeCount[collector.Info.Type]++
	}
	stats["collectors_by_type"] = typeCount

	// Count by status
	statusCount := make(map[string]int)
	for _, collector := range r.collectors {
		collector.mu.RLock()
		statusCount[collector.HealthStatus.Status.String()]++
		collector.mu.RUnlock()
	}
	stats["collectors_by_status"] = statusCount

	return stats
}

// CollectorStatistics represents detailed statistics for a collector
type CollectorStatistics struct {
	Name            string
	Type            string
	Status          string
	RegisteredAt    time.Time
	LastSeen        time.Time
	LastHealthCheck time.Time
	UptimeSeconds   float64
	EventTypes      []string
	Capabilities    []string
	Metrics         map[string]float64
}

// GetCollectorStatistics returns detailed statistics for a collector
func (r *InMemoryCollectorRegistry) GetCollectorStatistics(name string) (*CollectorStatistics, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collector, exists := r.collectors[name]
	if !exists {
		return nil, fmt.Errorf("collector %s not found", name)
	}

	collector.mu.RLock()
	defer collector.mu.RUnlock()

	uptime := time.Since(collector.RegisteredAt).Seconds()

	// Copy metrics
	metrics := make(map[string]float64)
	for k, v := range collector.Metrics {
		metrics[k] = v
	}

	return &CollectorStatistics{
		Name:            name,
		Type:            collector.Info.Type,
		Status:          collector.HealthStatus.Status.String(),
		RegisteredAt:    collector.RegisteredAt,
		LastSeen:        collector.Info.LastSeen,
		LastHealthCheck: collector.LastHealthCheck,
		UptimeSeconds:   uptime,
		EventTypes:      collector.Info.EventTypes,
		Capabilities:    collector.Info.Capabilities,
		Metrics:         metrics,
	}, nil
}
