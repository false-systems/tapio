package collectors

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/monitoring"
	"github.com/yairfalse/tapio/pkg/grpc"
)

// manager implements the Manager interface for lightweight collector lifecycle management
type manager struct {
	config     *Config
	grpcClient *GRPCStreamingClient
	
	// Collector management
	collectors    map[string]Collector
	collectorsMu  sync.RWMutex
	
	// Event processing
	eventChan     chan *Event
	eventHandler  EventHandler
	pipeline      Pipeline
	
	// Lifecycle management
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	started       atomic.Bool
	stopped       atomic.Bool
	
	// Resource monitoring
	monitor       *monitoring.ResourceMonitor
	
	// Statistics
	totalEvents   uint64
	droppedEvents uint64
	errors        uint64
	
	// Health tracking
	lastHealthCheck time.Time
	healthCache     map[string]*Health
	healthMu        sync.RWMutex
}

// Config provides configuration for the collector manager
type Config struct {
	// Enabled collectors
	EnabledCollectors []string `json:"enabled_collectors"`
	
	// Collection settings
	SamplingRate    float64 `json:"sampling_rate"`
	MaxEventsPerSec int     `json:"max_events_per_sec"`
	BufferSize      int     `json:"buffer_size"`
	
	// gRPC configuration
	GRPC GRPCConfig `json:"grpc"`
	
	// Resource limits
	Resources ResourceConfig `json:"resources"`
	
	// Pipeline configuration
	Pipeline PipelineConfig `json:"pipeline"`
}

// GRPCConfig contains gRPC client configuration
type GRPCConfig struct {
	ServerEndpoints      []string      `json:"server_endpoints"`
	TLSEnabled          bool          `json:"tls_enabled"`
	MaxBatchSize        int           `json:"max_batch_size"`
	BatchTimeout        time.Duration `json:"batch_timeout"`
	ReconnectEnabled    bool          `json:"reconnect_enabled"`
	MaxReconnectAttempts int          `json:"max_reconnect_attempts"`
}

// ResourceConfig contains resource limit configuration
type ResourceConfig struct {
	MaxMemoryMB int `json:"max_memory_mb"`
	MaxCPUMilli int `json:"max_cpu_milli"`
}

// PipelineConfig contains event processing pipeline configuration
type PipelineConfig struct {
	EnableFiltering    bool                   `json:"enable_filtering"`
	EnableTransformation bool                 `json:"enable_transformation"`
	FilterConfig       map[string]interface{} `json:"filter_config"`
	TransformerConfig  map[string]interface{} `json:"transformer_config"`
}

// NewManager creates a new collector manager
func NewManager(config *Config, grpcClient *GRPCStreamingClient) Manager {
	ctx, cancel := context.WithCancel(context.Background())
	
	m := &manager{
		config:      config,
		grpcClient:  grpcClient,
		collectors:  make(map[string]Collector),
		eventChan:   make(chan *Event, config.BufferSize),
		ctx:         ctx,
		cancel:      cancel,
		healthCache: make(map[string]*Health),
	}
	
	// Initialize resource monitoring
	m.monitor = monitoring.NewResourceMonitor(monitoring.ResourceLimits{
		MaxMemoryMB: config.Resources.MaxMemoryMB,
		MaxCPUMilli: config.Resources.MaxCPUMilli,
	})
	
	// Initialize event handler
	m.eventHandler = NewEventHandler(grpcClient)
	
	// Initialize processing pipeline
	m.pipeline = NewPipeline(config.Pipeline)
	
	return m
}

// Register adds a new collector to the manager
func (m *manager) Register(collector Collector) error {
	m.collectorsMu.Lock()
	defer m.collectorsMu.Unlock()
	
	name := collector.Name()
	if _, exists := m.collectors[name]; exists {
		return fmt.Errorf("collector %s already registered", name)
	}
	
	m.collectors[name] = collector
	
	// Initialize health cache entry
	m.healthMu.Lock()
	m.healthCache[name] = &Health{
		Status:  HealthStatusStopped,
		Message: "Registered but not started",
	}
	m.healthMu.Unlock()
	
	return nil
}

// Unregister removes a collector from the manager
func (m *manager) Unregister(name string) error {
	m.collectorsMu.Lock()
	defer m.collectorsMu.Unlock()
	
	collector, exists := m.collectors[name]
	if !exists {
		return fmt.Errorf("collector %s not found", name)
	}
	
	// Stop the collector if it's running
	if m.started.Load() {
		if err := collector.Stop(); err != nil {
			return fmt.Errorf("failed to stop collector %s: %w", name, err)
		}
	}
	
	delete(m.collectors, name)
	
	// Remove from health cache
	m.healthMu.Lock()
	delete(m.healthCache, name)
	m.healthMu.Unlock()
	
	return nil
}

// Start begins all registered collectors
func (m *manager) Start(ctx context.Context) error {
	if !m.started.CompareAndSwap(false, true) {
		return fmt.Errorf("manager already started")
	}
	
	// Start resource monitoring
	if err := m.monitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start resource monitor: %w", err)
	}
	
	// Start event processing
	m.wg.Add(1)
	go m.processEvents()
	
	// Start health monitoring
	m.wg.Add(1)
	go m.monitorHealth()
	
	// Start all registered collectors
	m.collectorsMu.RLock()
	collectors := make([]Collector, 0, len(m.collectors))
	for _, collector := range m.collectors {
		collectors = append(collectors, collector)
	}
	m.collectorsMu.RUnlock()
	
	for _, collector := range collectors {
		if !collector.IsEnabled() {
			continue
		}
		
		if err := collector.Start(m.ctx); err != nil {
			return fmt.Errorf("failed to start collector %s: %w", collector.Name(), err)
		}
		
		// Start event forwarding for this collector
		m.wg.Add(1)
		go m.forwardEvents(collector)
		
		// Update health status
		m.healthMu.Lock()
		m.healthCache[collector.Name()] = &Health{
			Status:  HealthStatusHealthy,
			Message: "Started successfully",
		}
		m.healthMu.Unlock()
	}
	
	return nil
}

// Stop gracefully stops all collectors
func (m *manager) Stop() error {
	if !m.stopped.CompareAndSwap(false, true) {
		return nil // Already stopped
	}
	
	// Cancel context to signal shutdown
	m.cancel()
	
	// Stop all collectors
	m.collectorsMu.RLock()
	var stopErrors []error
	for name, collector := range m.collectors {
		if err := collector.Stop(); err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("failed to stop collector %s: %w", name, err))
		}
		
		// Update health status
		m.healthMu.Lock()
		m.healthCache[name] = &Health{
			Status:  HealthStatusStopped,
			Message: "Stopped",
		}
		m.healthMu.Unlock()
	}
	m.collectorsMu.RUnlock()
	
	// Wait for all goroutines to finish
	m.wg.Wait()
	
	// Stop resource monitoring
	if err := m.monitor.Shutdown(); err != nil {
		stopErrors = append(stopErrors, fmt.Errorf("failed to stop resource monitor: %w", err))
	}
	
	// Close event channel
	close(m.eventChan)
	
	if len(stopErrors) > 0 {
		return fmt.Errorf("errors during shutdown: %v", stopErrors)
	}
	
	return nil
}

// Events returns a merged stream of events from all collectors
func (m *manager) Events() <-chan *Event {
	return m.eventChan
}

// Health returns the health status of all collectors
func (m *manager) Health() map[string]*Health {
	m.healthMu.RLock()
	defer m.healthMu.RUnlock()
	
	health := make(map[string]*Health)
	for name, h := range m.healthCache {
		// Create a copy to avoid race conditions
		health[name] = &Health{
			Status:           h.Status,
			Message:          h.Message,
			LastEventTime:    h.LastEventTime,
			EventsProcessed:  h.EventsProcessed,
			EventsDropped:    h.EventsDropped,
			ErrorCount:       h.ErrorCount,
			LastError:        h.LastError,
			LastErrorTime:    h.LastErrorTime,
			Metrics:          h.Metrics,
		}
	}
	
	return health
}

// GetStats returns statistics for all collectors
func (m *manager) GetStats() map[string]*Stats {
	m.collectorsMu.RLock()
	defer m.collectorsMu.RUnlock()
	
	stats := make(map[string]*Stats)
	for name, collector := range m.collectors {
		stats[name] = collector.GetStats()
	}
	
	return stats
}

// GetCollector returns a specific collector by name
func (m *manager) GetCollector(name string) (Collector, bool) {
	m.collectorsMu.RLock()
	defer m.collectorsMu.RUnlock()
	
	collector, exists := m.collectors[name]
	return collector, exists
}

// ListCollectors returns all registered collector names
func (m *manager) ListCollectors() []string {
	m.collectorsMu.RLock()
	defer m.collectorsMu.RUnlock()
	
	names := make([]string, 0, len(m.collectors))
	for name := range m.collectors {
		names = append(names, name)
	}
	
	return names
}

// Configure updates the configuration for a specific collector
func (m *manager) Configure(name string, config CollectorConfig) error {
	m.collectorsMu.RLock()
	collector, exists := m.collectors[name]
	m.collectorsMu.RUnlock()
	
	if !exists {
		return fmt.Errorf("collector %s not found", name)
	}
	
	return collector.Configure(config)
}

// Reload reloads configuration for all collectors
func (m *manager) Reload() error {
	// This would reload configuration from external source
	// For now, it's a placeholder that could trigger reconfiguration
	return nil
}

// forwardEvents forwards events from a collector to the event channel
func (m *manager) forwardEvents(collector Collector) {
	defer m.wg.Done()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case event, ok := <-collector.Events():
			if !ok {
				return // Channel closed
			}
			
			// Apply sampling if configured
			if m.config.SamplingRate < 1.0 {
				if !m.shouldSample() {
					atomic.AddUint64(&m.droppedEvents, 1)
					continue
				}
			}
			
			// Process through pipeline
			processedEvent, err := m.pipeline.Process(m.ctx, event)
			if err != nil {
				atomic.AddUint64(&m.errors, 1)
				continue
			}
			
			if processedEvent == nil {
				// Event was filtered out
				continue
			}
			
			// Try to send to event channel
			select {
			case m.eventChan <- processedEvent:
				atomic.AddUint64(&m.totalEvents, 1)
			default:
				// Channel is full, drop event
				atomic.AddUint64(&m.droppedEvents, 1)
			}
		}
	}
}

// processEvents handles events from the event channel
func (m *manager) processEvents() {
	defer m.wg.Done()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case event, ok := <-m.eventChan:
			if !ok {
				return // Channel closed
			}
			
			// Handle the event
			if err := m.eventHandler.HandleEvent(m.ctx, event); err != nil {
				atomic.AddUint64(&m.errors, 1)
			}
		}
	}
}

// monitorHealth periodically checks collector health
func (m *manager) monitorHealth() {
	defer m.wg.Done()
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.updateHealthStatus()
		}
	}
}

// updateHealthStatus updates the health status of all collectors
func (m *manager) updateHealthStatus() {
	m.collectorsMu.RLock()
	collectors := make(map[string]Collector)
	for name, collector := range m.collectors {
		collectors[name] = collector
	}
	m.collectorsMu.RUnlock()
	
	m.healthMu.Lock()
	defer m.healthMu.Unlock()
	
	for name, collector := range collectors {
		health := collector.Health()
		if health != nil {
			m.healthCache[name] = health
		}
	}
	
	m.lastHealthCheck = time.Now()
}

// shouldSample returns true if the event should be sampled
func (m *manager) shouldSample() bool {
	// Simple sampling implementation
	// In production, this could be more sophisticated
	return true // Placeholder - would implement proper sampling logic
}

// GetManagerStats returns overall manager statistics
func (m *manager) GetManagerStats() ManagerStats {
	usage := m.monitor.GetUsage()
	
	return ManagerStats{
		TotalEvents:     atomic.LoadUint64(&m.totalEvents),
		DroppedEvents:   atomic.LoadUint64(&m.droppedEvents),
		ErrorCount:      atomic.LoadUint64(&m.errors),
		ActiveCollectors: len(m.collectors),
		MemoryUsageMB:   usage.MemoryMB,
		CPUUsagePercent: usage.CPUPercent,
		Uptime:         time.Since(m.lastHealthCheck),
	}
}

// ManagerStats provides overall manager statistics
type ManagerStats struct {
	TotalEvents      uint64        `json:"total_events"`
	DroppedEvents    uint64        `json:"dropped_events"`
	ErrorCount       uint64        `json:"error_count"`
	ActiveCollectors int           `json:"active_collectors"`
	MemoryUsageMB    float64       `json:"memory_usage_mb"`
	CPUUsagePercent  float64       `json:"cpu_usage_percent"`
	Uptime          time.Duration `json:"uptime"`
}