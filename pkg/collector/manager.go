package collector

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SimpleManager coordinates multiple collectors and correlation
type SimpleManager struct {
	// Core components
	collectors  map[string]Collector
	correlation *CorrelationEngine
	eventChan   chan Event
	ctx         context.Context
	cancel      context.CancelFunc

	// State
	mu        sync.RWMutex
	isRunning bool

	// Configuration
	config ManagerConfig
}

// ManagerConfig configures the sniffer manager
type ManagerConfig struct {
	// Correlation settings
	CorrelationBatchSize    int
	CorrelationBatchTimeout time.Duration

	// Resource limits (for all collectors combined)
	MaxMemoryMB int
	MaxCPUMilli int

	// Event processing
	EventBufferSize int
}

// DefaultManagerConfig returns default configuration
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		CorrelationBatchSize:    100,
		CorrelationBatchTimeout: 100 * time.Millisecond,
		MaxMemoryMB:             256, // Total for all collectors
		MaxCPUMilli:             100, // Total for all collectors
		EventBufferSize:         50000,
	}
}

// NewSimpleManager creates a new sniffer manager
func NewSimpleManager(config ManagerConfig) *SimpleManager {
	correlation := NewCorrelationEngine(config.CorrelationBatchSize, config.CorrelationBatchTimeout)

	return &SimpleManager{
		collectors:  make(map[string]Collector),
		correlation: correlation,
		eventChan:   make(chan Event, config.EventBufferSize),
		config:      config,
	}
}

// Register adds a new sniffer to the manager
func (m *SimpleManager) Register(c Collector) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return fmt.Errorf("cannot register collector while running")
	}

	name := c.Name()
	if _, exists := m.collectors[name]; exists {
		return fmt.Errorf("collector %s already registered", name)
	}

	m.collectors[name] = c

	// Also register with correlation engine
	return m.correlation.RegisterCollector(c)
}

// Start begins all registered collectors
func (m *SimpleManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return fmt.Errorf("manager already running")
	}

	if len(m.collectors) == 0 {
		return fmt.Errorf("no collectors registered")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)

	// Calculate resource limits per sniffer
	snifferConfig := m.calculateCollectorConfig()

	// Start all collectors
	for name, c := range m.collectors {
		if err := c.Start(m.ctx, snifferConfig); err != nil {
			// Stop any started collectors
			m.stopCollectors()
			return fmt.Errorf("failed to start sniffer %s: %w", name, err)
		}
	}

	// Start correlation engine
	if err := m.correlation.Start(m.ctx); err != nil {
		m.stopCollectors()
		return fmt.Errorf("failed to start correlation engine: %w", err)
	}

	// Start event forwarding
	go m.forwardEvents()

	// Start health monitoring
	go m.monitorHealth()

	m.isRunning = true
	return nil
}

// calculateCollectorConfig calculates per-sniffer resource limits
func (m *SimpleManager) calculateCollectorConfig() Config {
	numCollectors := len(m.collectors)
	if numCollectors == 0 {
		numCollectors = 1
	}

	return Config{
		Enabled:         true,
		SamplingRate:    1.0,
		EventBufferSize: m.config.EventBufferSize / numCollectors,
		MaxEventsPerSec: 100000,
		MaxMemoryMB:     m.config.MaxMemoryMB / numCollectors,
		MaxCPUMilli:     m.config.MaxCPUMilli / numCollectors,
		Extra:           make(map[string]interface{}),
	}
}

// forwardEvents merges events from all collectors
func (m *SimpleManager) forwardEvents() {
	// Create a goroutine for each sniffer
	var wg sync.WaitGroup

	for name, c := range m.collectors {
		wg.Add(1)
		go func(name string, c Collector) {
			defer wg.Done()
			events := c.Events()

			for {
				select {
				case <-m.ctx.Done():
					return
				case event, ok := <-events:
					if !ok {
						return
					}

					// Forward to merged channel
					select {
					case m.eventChan <- event:
					default:
						// Drop if buffer full
					}
				}
			}
		}(name, c)
	}

	// Wait for all forwarders to finish
	wg.Wait()
	close(m.eventChan)
}

// monitorHealth periodically checks sniffer health
func (m *SimpleManager) monitorHealth() {
	ticker := time.NewTicker(30 * time.Second)
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
func (m *SimpleManager) checkHealth() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, c := range m.collectors {
		health := c.Health()

		// Log unhealthy collectors (in production, would alert)
		if health.Status == HealthStatusUnhealthy {
			fmt.Printf("Collector %s is unhealthy: %s\n", name, health.Message)
		}
	}
}

// Events returns a merged stream of events from all collectors
func (m *SimpleManager) Events() <-chan Event {
	return m.eventChan
}

// Insights returns the insights channel from correlation engine
func (m *SimpleManager) Insights() <-chan Insight {
	return m.correlation.Insights()
}

// Health returns the health status of all collectors
func (m *SimpleManager) Health() map[string]Health {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := make(map[string]Health)

	for name, c := range m.collectors {
		health[name] = c.Health()
	}

	// Add correlation engine health
	correlationHealth := Health{
		Status:  HealthStatusHealthy,
		Message: "Correlation engine running",
		Metrics: m.correlation.GetStats(),
	}
	health["correlation"] = correlationHealth

	return health
}

// GetCollector returns a specific sniffer by name
func (m *SimpleManager) GetCollector(name string) (Collector, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sniffer, exists := m.collectors[name]
	return sniffer, exists
}

// GetStats returns manager statistics
func (m *SimpleManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := map[string]interface{}{
		"collectors_count":  len(m.collectors),
		"is_running":        m.isRunning,
		"event_buffer_size": len(m.eventChan),
	}

	// Add correlation stats
	for k, v := range m.correlation.GetStats() {
		stats["correlation_"+k] = v
	}

	// Add per-sniffer stats
	snifferStats := make(map[string]interface{})
	for name, c := range m.collectors {
		health := c.Health()
		snifferStats[name] = map[string]interface{}{
			"status":           health.Status,
			"events_processed": health.EventsProcessed,
			"events_dropped":   health.EventsDropped,
		}
	}
	stats["collectors"] = snifferStats

	return stats
}

// Stop stops all collectors and the manager
func (m *SimpleManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return nil
	}

	// Stop correlation engine
	m.correlation.Stop()

	// Stop all collectors
	m.stopCollectors()

	// Cancel context
	if m.cancel != nil {
		m.cancel()
	}

	m.isRunning = false
	return nil
}

// stopCollectors stops all registered collectors
func (m *SimpleManager) stopCollectors() {
	for name, c := range m.collectors {
		if stopper, ok := c.(interface{ Stop() error }); ok {
			if err := stopper.Stop(); err != nil {
				fmt.Printf("Error stopping sniffer %s: %v\n", name, err)
			}
		}
	}
}

// Example usage function
func ExampleUsage() {
	// Create manager with default config
	config := DefaultManagerConfig()
	manager := NewSimpleManager(config)

	// Create and register collectors
	// ebpfMonitor := ebpf.NewMonitor(...)
	// k8sClient := kubernetes.NewForConfig(...)
	// translator := NewSimplePIDTranslator(k8sClient)

	// ebpfCollector := NewEBPFCollector(ebpfMonitor, translator)
	// k8sCollector := NewK8sCollector(k8sClient)

	// manager.Register(ebpfCollector)
	// manager.Register(k8sCollector)

	// Start everything
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		fmt.Printf("Failed to start manager: %v\n", err)
		return
	}

	// Process insights
	go func() {
		insights := manager.Insights()
		for insight := range insights {
			fmt.Printf("INSIGHT: %s - %s\n", insight.Title, insight.Description)
			for _, action := range insight.Actions {
				fmt.Printf("  ACTION: %s\n", action.Title)
				for _, cmd := range action.Commands {
					fmt.Printf("    $ %s\n", cmd)
				}
			}
		}
	}()

	// Monitor health
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			health := manager.Health()
			for name, h := range health {
				fmt.Printf("%s: %s - %s\n", name, h.Status, h.Message)
			}
		}
	}()
}
