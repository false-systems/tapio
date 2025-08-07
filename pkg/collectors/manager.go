package collectors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CollectorInterface defines the common interface all collectors must implement
type CollectorInterface interface {
	// Lifecycle management
	Start(ctx context.Context) error
	Stop() error

	// Event streaming
	Events() <-chan domain.UnifiedEvent

	// Health and monitoring
	Health() CollectorHealth
	Statistics() CollectorStatistics

	// Metadata
	Name() string
	Type() string
}

// CollectorHealth represents health status for any collector
type CollectorHealth struct {
	Status          domain.HealthStatusValue `json:"status"`
	Message         string                   `json:"message"`
	LastEventTime   time.Time                `json:"last_event_time"`
	EventsProcessed uint64                   `json:"events_processed"`
	EventsDropped   uint64                   `json:"events_dropped"`
	ErrorCount      uint64                   `json:"error_count"`
	Metrics         map[string]float64       `json:"metrics"`
}

// CollectorStatistics represents runtime statistics
type CollectorStatistics struct {
	StartTime       time.Time              `json:"start_time"`
	EventsCollected uint64                 `json:"events_collected"`
	EventsDropped   uint64                 `json:"events_dropped"`
	Custom          map[string]interface{} `json:"custom"`
}

// Manager coordinates multiple collectors
type Manager struct {
	// Core components
	collectors map[string]CollectorInterface
	eventChan  chan domain.UnifiedEvent
	ctx        context.Context
	cancel     context.CancelFunc

	// State
	mu        sync.RWMutex
	isRunning bool
	startTime time.Time

	// Configuration
	config ManagerConfig

	// Metrics
	totalEvents   uint64
	droppedEvents uint64
}

// ManagerConfig configures the collector manager
type ManagerConfig struct {
	// Event processing
	EventBufferSize int

	// Health monitoring
	HealthCheckInterval time.Duration

	// Resource limits
	MaxMemoryMB int
	MaxCPUMilli int
}

// DefaultManagerConfig returns default configuration
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		EventBufferSize:     100000,
		HealthCheckInterval: 30 * time.Second,
		MaxMemoryMB:         512,
		MaxCPUMilli:         200,
	}
}

// NewManager creates a new collector manager
func NewManager(config ManagerConfig) *Manager {
	return &Manager{
		collectors: make(map[string]CollectorInterface),
		eventChan:  make(chan domain.UnifiedEvent, config.EventBufferSize),
		config:     config,
	}
}

// Register adds a new collector to the manager
func (m *Manager) Register(name string, collector CollectorInterface) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return fmt.Errorf("cannot register collector while running")
	}

	if _, exists := m.collectors[name]; exists {
		return fmt.Errorf("collector %s already registered", name)
	}

	m.collectors[name] = collector
	return nil
}

// Start begins all registered collectors
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return fmt.Errorf("manager already running")
	}

	if len(m.collectors) == 0 {
		return fmt.Errorf("no collectors registered")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.startTime = time.Now()

	// Start all collectors
	for name, collector := range m.collectors {
		if err := collector.Start(m.ctx); err != nil {
			// Stop any started collectors
			m.stopCollectors()
			return fmt.Errorf("failed to start collector %s: %w", name, err)
		}
	}

	// Start event forwarding
	m.startEventForwarding()

	// Start health monitoring
	go m.monitorHealth()

	m.isRunning = true
	return nil
}

// startEventForwarding merges events from all collectors
func (m *Manager) startEventForwarding() {
	for name, collector := range m.collectors {
		go func(name string, c CollectorInterface) {
			events := c.Events()
			for {
				select {
				case <-m.ctx.Done():
					return
				case event, ok := <-events:
					if !ok {
						return
					}

					// Add collector metadata
					if event.Source == "" {
						event.Source = name
					}

					// Forward to merged channel
					select {
					case m.eventChan <- event:
						m.mu.Lock()
						m.totalEvents++
						m.mu.Unlock()
					default:
						// Drop if buffer full
						m.mu.Lock()
						m.droppedEvents++
						m.mu.Unlock()
					}
				}
			}
		}(name, collector)
	}
}

// monitorHealth periodically checks collector health
func (m *Manager) monitorHealth() {
	ticker := time.NewTicker(m.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkHealth()
		}
	}
}

// checkHealth checks the health of all collectors
func (m *Manager) checkHealth() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, collector := range m.collectors {
		health := collector.Health()

		// Log unhealthy collectors
		if health.Status == domain.HealthUnhealthy {
			// In production, this would trigger alerts
			fmt.Printf("[WARN] Collector %s is unhealthy: %s\n", name, health.Message)
		}
	}
}

// Events returns a merged stream of events from all collectors
func (m *Manager) Events() <-chan domain.UnifiedEvent {
	return m.eventChan
}

// Health returns the health status of all collectors
func (m *Manager) Health() map[string]CollectorHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := make(map[string]CollectorHealth)

	for name, collector := range m.collectors {
		health[name] = collector.Health()
	}

	// Add manager's own health
	managerHealth := CollectorHealth{
		Status:          m.getManagerHealth(),
		Message:         "Collector manager",
		LastEventTime:   time.Now(),
		EventsProcessed: m.totalEvents,
		EventsDropped:   m.droppedEvents,
		ErrorCount:      0,
		Metrics: map[string]float64{
			"collectors_count":   float64(len(m.collectors)),
			"event_buffer_usage": float64(len(m.eventChan)) / float64(m.config.EventBufferSize),
		},
	}
	health["manager"] = managerHealth

	return health
}

// getManagerHealth determines manager's health status
func (m *Manager) getManagerHealth() domain.HealthStatusValue {
	if !m.isRunning {
		return domain.HealthUnknown
	}

	// Check if any collector is unhealthy
	unhealthyCount := 0
	for _, collector := range m.collectors {
		if collector.Health().Status == domain.HealthUnhealthy {
			unhealthyCount++
		}
	}

	if unhealthyCount == len(m.collectors) {
		return domain.HealthUnhealthy
	} else if unhealthyCount > 0 {
		return domain.HealthDegraded
	}

	// Check event drop rate
	dropRate := float64(m.droppedEvents) / float64(m.totalEvents+1)
	if dropRate > 0.1 { // More than 10% drops
		return domain.HealthDegraded
	}

	return domain.HealthHealthy
}

// Statistics returns aggregated statistics from all collectors
func (m *Manager) Statistics() map[string]CollectorStatistics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]CollectorStatistics)

	for name, collector := range m.collectors {
		stats[name] = collector.Statistics()
	}

	// Add manager statistics
	managerStats := CollectorStatistics{
		StartTime:       m.startTime,
		EventsCollected: m.totalEvents,
		EventsDropped:   m.droppedEvents,
		Custom: map[string]interface{}{
			"collectors_count":  len(m.collectors),
			"uptime_seconds":    time.Since(m.startTime).Seconds(),
			"event_buffer_size": m.config.EventBufferSize,
		},
	}
	stats["manager"] = managerStats

	return stats
}

// GetCollector returns a specific collector by name
func (m *Manager) GetCollector(name string) (CollectorInterface, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	collector, exists := m.collectors[name]
	return collector, exists
}

// Stop stops all collectors and the manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return nil
	}

	// Stop all collectors
	m.stopCollectors()

	// Cancel context
	if m.cancel != nil {
		m.cancel()
	}

	// Close event channel
	close(m.eventChan)

	m.isRunning = false
	return nil
}

// stopCollectors stops all registered collectors
func (m *Manager) stopCollectors() {
	var wg sync.WaitGroup

	for name, collector := range m.collectors {
		wg.Add(1)
		go func(name string, c CollectorInterface) {
			defer wg.Done()
			if err := c.Stop(); err != nil {
				fmt.Printf("[ERROR] Failed to stop collector %s: %v\n", name, err)
			}
		}(name, collector)
	}

	// Wait for all collectors to stop
	wg.Wait()
}

// IsRunning returns whether the manager is running
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isRunning
}

// CollectorCount returns the number of registered collectors
func (m *Manager) CollectorCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.collectors)
}
