package integration

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collector"
	"github.com/yairfalse/tapio/pkg/events_correlation"
	"github.com/yairfalse/tapio/pkg/events_correlation/bridge"
	"github.com/yairfalse/tapio/pkg/events_correlation/rules"
	"github.com/yairfalse/tapio/pkg/events_correlation/store"
)

// IntegratedManager combines Tapio collectors with the events correlation engine
type IntegratedManager struct {
	// Core components
	tapioManager      *collector.SimpleManager
	correlationEngine events_correlation.Engine
	eventStore        events_correlation.EventStore
	eventBridge       *bridge.TapioEventBridge

	// Channels
	resultsChan chan events_correlation.Result
	ctx         context.Context
	cancel      context.CancelFunc

	// State
	mu        sync.RWMutex
	isRunning bool

	// Configuration
	config IntegrationConfig
}

// IntegrationConfig configures the integrated system
type IntegrationConfig struct {
	// Event processing
	EventBufferSize   int
	CorrelationWindow time.Duration
	ResultBufferSize  int

	// Performance tuning
	MaxConcurrency    int
	ProcessingTimeout time.Duration

	// Rule configuration
	EnableMemoryRules  bool
	EnableCPURules     bool
	EnableNetworkRules bool

	// Tapio manager config
	TapioConfig collector.ManagerConfig
}

// DefaultIntegrationConfig returns default configuration
func DefaultIntegrationConfig() IntegrationConfig {
	return IntegrationConfig{
		EventBufferSize:    10000,
		CorrelationWindow:  5 * time.Minute,
		ResultBufferSize:   1000,
		MaxConcurrency:     10,
		ProcessingTimeout:  30 * time.Second,
		EnableMemoryRules:  true,
		EnableCPURules:     true,
		EnableNetworkRules: true,
		TapioConfig:        collector.DefaultManagerConfig(),
	}
}

// NewIntegratedManager creates a new integrated manager
func NewIntegratedManager(config IntegrationConfig) *IntegratedManager {
	// Create Tapio manager
	tapioManager := collector.NewSimpleManager(config.TapioConfig)

	// Create event store
	eventStore := store.NewMemoryEventStore(10000, 24*time.Hour)

	// Create correlation engine
	correlationEngine := events_correlation.NewEngine(eventStore,
		events_correlation.WithWindowSize(config.CorrelationWindow),
		events_correlation.WithMaxConcurrentRules(config.MaxConcurrency),
	)

	// Create event bridge
	eventBridge := bridge.NewTapioEventBridge(tapioManager)

	return &IntegratedManager{
		tapioManager:      tapioManager,
		correlationEngine: correlationEngine,
		eventStore:        eventStore,
		eventBridge:       eventBridge,
		resultsChan:       make(chan events_correlation.Result, config.ResultBufferSize),
		config:            config,
	}
}

// RegisterCollectors registers Tapio collectors with the manager
func (m *IntegratedManager) RegisterCollectors(collectors ...collector.Collector) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return fmt.Errorf("cannot register collectors while running")
	}

	for _, c := range collectors {
		if err := m.tapioManager.Register(c); err != nil {
			return fmt.Errorf("failed to register collector %s: %w", c.Name(), err)
		}
	}

	return nil
}

// Start begins the integrated system
func (m *IntegratedManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return fmt.Errorf("manager already running")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start Tapio manager first
	if err := m.tapioManager.Start(m.ctx); err != nil {
		return fmt.Errorf("failed to start Tapio manager: %w", err)
	}

	// Start correlation engine
	if err := m.correlationEngine.Start(m.ctx); err != nil {
		m.tapioManager.Stop()
		return fmt.Errorf("failed to start correlation engine: %w", err)
	}

	// Register correlation rules
	if err := m.registerRules(); err != nil {
		m.correlationEngine.Stop()
		m.tapioManager.Stop()
		return fmt.Errorf("failed to register correlation rules: %w", err)
	}

	// Start event processing
	go m.processEvents()

	// Start result forwarding
	go m.forwardResults()

	// Start cleanup routine
	go m.startCleanupRoutine()

	m.isRunning = true
	return nil
}

// registerRules registers correlation rules with the engine
func (m *IntegratedManager) registerRules() error {
	if m.config.EnableMemoryRules {
		// Memory rules
		if err := m.correlationEngine.RegisterRule(rules.MemoryPressureCascade()); err != nil {
			return fmt.Errorf("failed to register memory pressure rule: %w", err)
		}
		if err := m.correlationEngine.RegisterRule(rules.MemoryLeakDetection()); err != nil {
			return fmt.Errorf("failed to register memory leak rule: %w", err)
		}
		if err := m.correlationEngine.RegisterRule(rules.ContainerOOMPrediction()); err != nil {
			return fmt.Errorf("failed to register OOM prediction rule: %w", err)
		}
	}

	if m.config.EnableCPURules {
		// CPU rules
		if err := m.correlationEngine.RegisterRule(rules.CPUThrottleDetection()); err != nil {
			return fmt.Errorf("failed to register CPU throttle rule: %w", err)
		}
		if err := m.correlationEngine.RegisterRule(rules.CPUContentionDetection()); err != nil {
			return fmt.Errorf("failed to register CPU contention rule: %w", err)
		}
		if err := m.correlationEngine.RegisterRule(rules.HighCPUUtilizationPattern()); err != nil {
			return fmt.Errorf("failed to register high CPU rule: %w", err)
		}
	}

	return nil
}

// processEvents processes events from Tapio through the correlation engine
func (m *IntegratedManager) processEvents() {
	// Get event stream from bridge
	events := m.eventBridge.StreamEvents()

	// Collect events in batches for processing
	eventBatch := make([]events_correlation.Event, 0, 100)
	batchTimer := time.NewTicker(time.Second)
	defer batchTimer.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}

			// Store event in the event store
			if err := m.eventStore.StoreEvent(m.ctx, event); err != nil {
				// Log error but continue processing
				fmt.Printf("Failed to store event: %v\n", err)
			}

			// Add to batch
			eventBatch = append(eventBatch, event)

			// Process batch if it's full
			if len(eventBatch) >= 100 {
				m.processBatch(eventBatch)
				eventBatch = eventBatch[:0]
			}

		case <-batchTimer.C:
			// Process any remaining events in batch
			if len(eventBatch) > 0 {
				m.processBatch(eventBatch)
				eventBatch = eventBatch[:0]
			}
		}
	}
}

// processBatch processes a batch of events through the correlation engine
func (m *IntegratedManager) processBatch(events []events_correlation.Event) {
	results, err := m.correlationEngine.ProcessEvents(m.ctx, events)
	if err != nil {
		fmt.Printf("Failed to process events: %v\n", err)
		return
	}

	// Forward results
	for _, result := range results {
		select {
		case m.resultsChan <- *result:
		default:
			// Drop if buffer full
		}
	}
}

// cleanOldEvents removes old events from the event store
func (m *IntegratedManager) cleanOldEvents() {
	cutoff := time.Now().Add(-m.config.CorrelationWindow * 2) // Keep 2x window for safety
	if err := m.eventStore.Cleanup(m.ctx, cutoff); err != nil {
		fmt.Printf("Failed to cleanup old events: %v\n", err)
	}
}

// startCleanupRoutine periodically cleans up old events
func (m *IntegratedManager) startCleanupRoutine() {
	ticker := time.NewTicker(m.config.CorrelationWindow)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cleanOldEvents()
		}
	}
}

// forwardResults forwards results from both correlation engines
func (m *IntegratedManager) forwardResults() {
	// Also forward Tapio insights through the bridge
	tapioInsights := m.eventBridge.GetInsights()

	for {
		select {
		case <-m.ctx.Done():
			return
		case insight, ok := <-tapioInsights:
			if !ok {
				continue
			}

			// Forward Tapio insights as correlation results
			select {
			case m.resultsChan <- insight:
			default:
				// Drop if buffer full
			}
		}
	}
}

// Results returns the unified results channel
func (m *IntegratedManager) Results() <-chan events_correlation.Result {
	return m.resultsChan
}

// GetStats returns comprehensive statistics
func (m *IntegratedManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})

	// Tapio manager stats
	stats["tapio"] = m.tapioManager.GetStats()

	// Correlation engine stats
	stats["correlation"] = m.correlationEngine.GetStats()

	// Bridge health
	stats["bridge"] = m.eventBridge.GetHealthStatus()

	// Integration stats
	stats["integration"] = map[string]interface{}{
		"is_running":         m.isRunning,
		"results_buffer":     len(m.resultsChan),
		"correlation_window": m.config.CorrelationWindow.String(),
		"rules_enabled": map[string]bool{
			"memory":  m.config.EnableMemoryRules,
			"cpu":     m.config.EnableCPURules,
			"network": m.config.EnableNetworkRules,
		},
	}

	return stats
}

// Health returns the health status of all components
func (m *IntegratedManager) Health() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := make(map[string]interface{})

	// Tapio manager health
	health["tapio_collectors"] = m.tapioManager.Health()

	// Bridge health
	health["event_bridge"] = m.eventBridge.GetHealthStatus()

	// Integration health
	integrationHealth := map[string]interface{}{
		"status":     "healthy",
		"is_running": m.isRunning,
		"context":    "correlation_engine_integrated",
	}

	// Check if system is overloaded
	if len(m.resultsChan) > int(float64(m.config.ResultBufferSize)*0.8) {
		integrationHealth["status"] = "warning"
		integrationHealth["message"] = "Results buffer nearly full"
	}

	health["integration"] = integrationHealth

	return health
}

// Stop stops all components
func (m *IntegratedManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return nil
	}

	// Stop correlation engine
	if err := m.correlationEngine.Stop(); err != nil {
		fmt.Printf("Error stopping correlation engine: %v\n", err)
	}

	// Stop Tapio manager
	if err := m.tapioManager.Stop(); err != nil {
		// Log error but continue cleanup
		fmt.Printf("Error stopping Tapio manager: %v\n", err)
	}

	// Cancel context
	if m.cancel != nil {
		m.cancel()
	}

	// Close results channel
	close(m.resultsChan)

	m.isRunning = false
	return nil
}

// CreateExampleSetup creates an example setup with common collectors
func CreateExampleSetup() (*IntegratedManager, error) {
	config := DefaultIntegrationConfig()
	manager := NewIntegratedManager(config)

	// Note: In a real setup, you would create and register actual collectors:
	//
	// // Create eBPF collector
	// ebpfMonitor := ebpf.NewMonitor(...)
	// pidTranslator := collector.NewSimplePIDTranslator(...)
	// ebpfCollector := collector.NewEBPFCollector(ebpfMonitor, pidTranslator)
	//
	// // Create Kubernetes collector
	// k8sClient := kubernetes.NewForConfig(...)
	// k8sCollector := collector.NewK8sCollector(k8sClient)
	//
	// // Register collectors
	// if err := manager.RegisterCollectors(ebpfCollector, k8sCollector); err != nil {
	//     return nil, err
	// }

	return manager, nil
}

// ProcessResults shows an example of how to process correlation results
func ProcessResults(manager *IntegratedManager) {
	results := manager.Results()

	for result := range results {
		fmt.Printf("CORRELATION RESULT: %s\n", result.Title)
		fmt.Printf("  Severity: %s | Confidence: %.2f\n", result.Severity, result.Confidence)
		fmt.Printf("  Category: %s | Description: %s\n", result.Category, result.Description)

		// Print evidence
		if len(result.Evidence.Events) > 0 {
			fmt.Printf("  Evidence: %d events\n", len(result.Evidence.Events))
		}
		if len(result.Evidence.Entities) > 0 {
			fmt.Printf("  Affected entities: %d\n", len(result.Evidence.Entities))
			for _, entity := range result.Evidence.Entities {
				fmt.Printf("    - %s: %s\n", entity.Type, entity.String())
			}
		}

		// Print recommendations
		if len(result.Recommendations) > 0 {
			fmt.Printf("  Recommendations:\n")
			for _, rec := range result.Recommendations {
				fmt.Printf("    - %s\n", rec)
			}
		}

		// Print actions
		if len(result.Actions) > 0 {
			fmt.Printf("  Actions:\n")
			for _, action := range result.Actions {
				fmt.Printf("    - %s (%s priority)\n", action.Target, action.Priority)
				if len(action.Parameters) > 0 {
					for k, v := range action.Parameters {
						fmt.Printf("      %s: %s\n", k, v)
					}
				}
			}
		}

		fmt.Println("---")
	}
}
