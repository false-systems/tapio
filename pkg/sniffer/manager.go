package sniffer

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SimpleManager coordinates multiple sniffers and correlation
type SimpleManager struct {
	// Core components
	sniffers    map[string]Sniffer
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
	
	// Resource limits (for all sniffers combined)
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
		MaxMemoryMB:             256,  // Total for all sniffers
		MaxCPUMilli:             100,  // Total for all sniffers
		EventBufferSize:         50000,
	}
}

// NewSimpleManager creates a new sniffer manager
func NewSimpleManager(config ManagerConfig) *SimpleManager {
	correlation := NewCorrelationEngine(config.CorrelationBatchSize, config.CorrelationBatchTimeout)
	
	return &SimpleManager{
		sniffers:    make(map[string]Sniffer),
		correlation: correlation,
		eventChan:   make(chan Event, config.EventBufferSize),
		config:      config,
	}
}

// Register adds a new sniffer to the manager
func (m *SimpleManager) Register(sniffer Sniffer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.isRunning {
		return fmt.Errorf("cannot register sniffer while running")
	}
	
	name := sniffer.Name()
	if _, exists := m.sniffers[name]; exists {
		return fmt.Errorf("sniffer %s already registered", name)
	}
	
	m.sniffers[name] = sniffer
	
	// Also register with correlation engine
	return m.correlation.RegisterSniffer(sniffer)
}

// Start begins all registered sniffers
func (m *SimpleManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.isRunning {
		return fmt.Errorf("manager already running")
	}
	
	if len(m.sniffers) == 0 {
		return fmt.Errorf("no sniffers registered")
	}
	
	m.ctx, m.cancel = context.WithCancel(ctx)
	
	// Calculate resource limits per sniffer
	snifferConfig := m.calculateSnifferConfig()
	
	// Start all sniffers
	for name, sniffer := range m.sniffers {
		if err := sniffer.Start(m.ctx, snifferConfig); err != nil {
			// Stop any started sniffers
			m.stopSniffers()
			return fmt.Errorf("failed to start sniffer %s: %w", name, err)
		}
	}
	
	// Start correlation engine
	if err := m.correlation.Start(m.ctx); err != nil {
		m.stopSniffers()
		return fmt.Errorf("failed to start correlation engine: %w", err)
	}
	
	// Start event forwarding
	go m.forwardEvents()
	
	// Start health monitoring
	go m.monitorHealth()
	
	m.isRunning = true
	return nil
}

// calculateSnifferConfig calculates per-sniffer resource limits
func (m *SimpleManager) calculateSnifferConfig() Config {
	numSniffers := len(m.sniffers)
	if numSniffers == 0 {
		numSniffers = 1
	}
	
	return Config{
		Enabled:         true,
		SamplingRate:    1.0,
		EventBufferSize: m.config.EventBufferSize / numSniffers,
		MaxEventsPerSec: 100000,
		MaxMemoryMB:     m.config.MaxMemoryMB / numSniffers,
		MaxCPUMilli:     m.config.MaxCPUMilli / numSniffers,
		Extra:           make(map[string]interface{}),
	}
}

// forwardEvents merges events from all sniffers
func (m *SimpleManager) forwardEvents() {
	// Create a goroutine for each sniffer
	var wg sync.WaitGroup
	
	for name, sniffer := range m.sniffers {
		wg.Add(1)
		go func(name string, sniffer Sniffer) {
			defer wg.Done()
			events := sniffer.Events()
			
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
		}(name, sniffer)
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

// checkHealth checks the health of all sniffers
func (m *SimpleManager) checkHealth() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for name, sniffer := range m.sniffers {
		health := sniffer.Health()
		
		// Log unhealthy sniffers (in production, would alert)
		if health.Status == HealthStatusUnhealthy {
			fmt.Printf("Sniffer %s is unhealthy: %s\n", name, health.Message)
		}
	}
}

// Events returns a merged stream of events from all sniffers
func (m *SimpleManager) Events() <-chan Event {
	return m.eventChan
}

// Insights returns the insights channel from correlation engine
func (m *SimpleManager) Insights() <-chan Insight {
	return m.correlation.Insights()
}

// Health returns the health status of all sniffers
func (m *SimpleManager) Health() map[string]Health {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	health := make(map[string]Health)
	
	for name, sniffer := range m.sniffers {
		health[name] = sniffer.Health()
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

// GetSniffer returns a specific sniffer by name
func (m *SimpleManager) GetSniffer(name string) (Sniffer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	sniffer, exists := m.sniffers[name]
	return sniffer, exists
}

// GetStats returns manager statistics
func (m *SimpleManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	stats := map[string]interface{}{
		"sniffers_count":    len(m.sniffers),
		"is_running":        m.isRunning,
		"event_buffer_size": len(m.eventChan),
	}
	
	// Add correlation stats
	for k, v := range m.correlation.GetStats() {
		stats["correlation_"+k] = v
	}
	
	// Add per-sniffer stats
	snifferStats := make(map[string]interface{})
	for name, sniffer := range m.sniffers {
		health := sniffer.Health()
		snifferStats[name] = map[string]interface{}{
			"status":           health.Status,
			"events_processed": health.EventsProcessed,
			"events_dropped":   health.EventsDropped,
		}
	}
	stats["sniffers"] = snifferStats
	
	return stats
}

// Stop stops all sniffers and the manager
func (m *SimpleManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if !m.isRunning {
		return nil
	}
	
	// Stop correlation engine
	m.correlation.Stop()
	
	// Stop all sniffers
	m.stopSniffers()
	
	// Cancel context
	if m.cancel != nil {
		m.cancel()
	}
	
	m.isRunning = false
	return nil
}

// stopSniffers stops all registered sniffers
func (m *SimpleManager) stopSniffers() {
	for name, sniffer := range m.sniffers {
		if stopper, ok := sniffer.(interface{ Stop() error }); ok {
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
	
	// Create and register sniffers
	// ebpfMonitor := ebpf.NewMonitor(...)
	// k8sClient := kubernetes.NewForConfig(...)
	// translator := NewSimplePIDTranslator(k8sClient)
	
	// ebpfSniffer := NewEBPFSniffer(ebpfMonitor, translator)
	// k8sSniffer := NewK8sSniffer(k8sClient)
	
	// manager.Register(ebpfSniffer)
	// manager.Register(k8sSniffer)
	
	// Start everything
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		panic(err)
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