package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	factoryregistry "github.com/yairfalse/tapio/pkg/collectors/factory"
	"github.com/yairfalse/tapio/pkg/config"
)

// CollectorManager manages the lifecycle of multiple collectors
type CollectorManager struct {
	config *config.Config

	// Collectors
	collectors map[string]collectors.Collector
	mu         sync.RWMutex

	// Event aggregation
	eventsChan chan collectors.RawEvent

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Health tracking
	health map[string]*CollectorHealth
}

// CollectorHealth tracks health status of a collector
type CollectorHealth struct {
	Name          string
	Healthy       bool
	LastHealthy   time.Time
	EventsEmitted int64
	LastError     error
	StartTime     time.Time
}

// NewManager creates a new collector manager
func NewManager(cfg *config.Config) *CollectorManager {
	return &CollectorManager{
		config:     cfg,
		collectors: make(map[string]collectors.Collector),
		eventsChan: make(chan collectors.RawEvent, cfg.Collectors.BufferSize*len(cfg.Collectors.Enabled)),
		health:     make(map[string]*CollectorHealth),
	}
}

// Start starts all configured collectors
func (m *CollectorManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ctx != nil {
		return fmt.Errorf("manager already started")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start each configured collector
	collectorConfig := m.config.Collectors.ToCollectorConfig()

	for _, name := range m.config.Collectors.Enabled {
		collector, err := factoryregistry.CreateCollector(name, collectorConfig)
		if err != nil {
			// Log error but continue with other collectors
			continue
		}

		// Initialize health tracking
		m.health[name] = &CollectorHealth{
			Name:      name,
			Healthy:   true,
			StartTime: time.Now(),
		}

		// Start collector
		if err := collector.Start(m.ctx); err != nil {
			m.health[name].Healthy = false
			m.health[name].LastError = err
			continue
		}

		m.collectors[name] = collector

		// Start event processor for this collector
		m.wg.Add(1)
		go m.processCollectorEvents(name, collector)
	}

	// Start health monitor
	m.wg.Add(1)
	go m.monitorHealth()

	return nil
}

// Stop gracefully stops all collectors
func (m *CollectorManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel == nil {
		return fmt.Errorf("manager not started")
	}

	// Cancel context to signal shutdown
	m.cancel()

	// Stop all collectors
	var errors []error
	for name, collector := range m.collectors {
		if err := collector.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop %s: %w", name, err))
		}
	}

	// Wait for goroutines to finish
	m.wg.Wait()

	// Close event channel
	close(m.eventsChan)

	// Reset state
	m.collectors = make(map[string]collectors.Collector)
	m.ctx = nil
	m.cancel = nil

	if len(errors) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errors)
	}

	return nil
}

// Events returns the aggregated event channel
func (m *CollectorManager) Events() <-chan collectors.RawEvent {
	return m.eventsChan
}

// GetHealth returns health status for all collectors
func (m *CollectorManager) GetHealth() map[string]*CollectorHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	health := make(map[string]*CollectorHealth)
	for k, v := range m.health {
		healthCopy := *v
		health[k] = &healthCopy
	}

	return health
}

// IsHealthy returns true if all collectors are healthy
func (m *CollectorManager) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, h := range m.health {
		if !h.Healthy {
			return false
		}
	}

	return true
}

// RestartCollector restarts a specific collector
func (m *CollectorManager) RestartCollector(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing collector
	if collector, exists := m.collectors[name]; exists {
		if err := collector.Stop(); err != nil {
			return fmt.Errorf("failed to stop collector: %w", err)
		}
		delete(m.collectors, name)
	}

	// Create and start new instance
	collectorConfig := m.config.Collectors.ToCollectorConfig()
	collector, err := factoryregistry.CreateCollector(name, collectorConfig)
	if err != nil {
		return fmt.Errorf("failed to create collector: %w", err)
	}

	if err := collector.Start(m.ctx); err != nil {
		return fmt.Errorf("failed to start collector: %w", err)
	}

	m.collectors[name] = collector

	// Reset health
	m.health[name] = &CollectorHealth{
		Name:      name,
		Healthy:   true,
		StartTime: time.Now(),
	}

	// Start event processor
	m.wg.Add(1)
	go m.processCollectorEvents(name, collector)

	return nil
}

// processCollectorEvents processes events from a single collector
func (m *CollectorManager) processCollectorEvents(name string, collector collectors.Collector) {
	defer m.wg.Done()

	events := collector.Events()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}

			// Update health stats
			m.mu.Lock()
			if h, exists := m.health[name]; exists {
				h.EventsEmitted++
				h.LastHealthy = time.Now()
			}
			m.mu.Unlock()

			// Forward event
			select {
			case m.eventsChan <- event:
			case <-m.ctx.Done():
				return
			default:
				// Buffer full, drop event
			}

		case <-m.ctx.Done():
			return
		}
	}
}

// monitorHealth periodically checks collector health
func (m *CollectorManager) monitorHealth() {
	defer m.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkHealth()
		case <-m.ctx.Done():
			return
		}
	}
}

// checkHealth checks the health of all collectors
func (m *CollectorManager) checkHealth() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, collector := range m.collectors {
		healthy := collector.IsHealthy()

		if h, exists := m.health[name]; exists {
			h.Healthy = healthy

			// Check if collector hasn't emitted events in a while
			if time.Since(h.LastHealthy) > 5*time.Minute && h.EventsEmitted > 0 {
				h.Healthy = false
			}
		}
	}
}
