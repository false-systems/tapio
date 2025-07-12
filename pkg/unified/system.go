package unified

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/ebpf"
	"github.com/yairfalse/tapio/pkg/performance"
	"github.com/yairfalse/tapio/pkg/resilience"
	"github.com/yairfalse/tapio/pkg/sources"
)

// UnifiedSystem represents the complete eBPF System Sniffer
type UnifiedSystem struct {
	// Core components
	ebpfCollector     *ebpf.EnhancedCollector
	correlationEngine *correlation.EnhancedEngine

	// Data sources
	ebpfSource     *sources.EBPFSource
	systemdSource  *sources.SystemdSource
	journaldSource *sources.JournaldSource

	// Performance components
	eventPipeline  *performance.EventPipeline
	objectPool     *performance.TypedPool[SystemEvent]
	perCPUBuffers  *performance.PerCPUBuffer
	batchProcessor *performance.AdaptiveBatchProcessor[SystemEvent]

	// Resilience components
	circuitBreaker *resilience.CircuitBreaker
	selfHealing    *resilience.SelfHealingManager
	loadShedder    *resilience.LoadShedder
	rateLimiter    *resilience.RateLimiter

	// Configuration
	config *SystemConfig

	// State management
	isRunning bool
	startTime time.Time

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mutex  sync.RWMutex
}

// SystemEvent represents a unified system event
type SystemEvent struct {
	Source    string
	Type      string
	Timestamp time.Time
	Data      interface{}
	Priority  int
}

// SystemConfig configures the unified system
type SystemConfig struct {
	// eBPF settings
	EnableNetworkMonitoring bool
	EnableDNSMonitoring     bool
	EnableProtocolAnalysis  bool

	// Source settings
	EnableSystemd  bool
	EnableJournald bool

	// Performance settings
	EventBufferSize    int
	MaxEventsPerSecond int
	BatchSize          int
	PerCPUBufferSize   int

	// Resilience settings
	EnableCircuitBreaker bool
	EnableSelfHealing    bool
	EnableLoadShedding   bool
	MaxFailures          uint32

	// Correlation settings
	CorrelationWindow     time.Duration
	EnablePatternAnalysis bool

	// Resource limits
	MaxMemoryMB   int
	MaxCPUPercent int
}

// DefaultSystemConfig returns the default configuration
func DefaultSystemConfig() *SystemConfig {
	return &SystemConfig{
		EnableNetworkMonitoring: true,
		EnableDNSMonitoring:     true,
		EnableProtocolAnalysis:  true,
		EnableSystemd:           true,
		EnableJournald:          true,
		EventBufferSize:         100000,
		MaxEventsPerSecond:      165000,
		BatchSize:               1000,
		PerCPUBufferSize:        64 * 1024,
		EnableCircuitBreaker:    true,
		EnableSelfHealing:       true,
		EnableLoadShedding:      true,
		MaxFailures:             5,
		CorrelationWindow:       5 * time.Minute,
		EnablePatternAnalysis:   true,
		MaxMemoryMB:             100,
		MaxCPUPercent:           50,
	}
}

// NewUnifiedSystem creates a new unified system
func NewUnifiedSystem(config *SystemConfig) (*UnifiedSystem, error) {
	if config == nil {
		config = DefaultSystemConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	system := &UnifiedSystem{
		config:    config,
		ctx:       ctx,
		cancel:    cancel,
		startTime: time.Now(),
	}

	// Initialize components
	if err := system.initializeComponents(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	return system, nil
}

// initializeComponents initializes all system components
func (s *UnifiedSystem) initializeComponents() error {
	// Initialize eBPF collector
	if err := s.initializeEBPF(); err != nil {
		return fmt.Errorf("failed to initialize eBPF: %w", err)
	}

	// Initialize data sources
	if err := s.initializeSources(); err != nil {
		return fmt.Errorf("failed to initialize sources: %w", err)
	}

	// Initialize performance components
	if err := s.initializePerformance(); err != nil {
		return fmt.Errorf("failed to initialize performance: %w", err)
	}

	// Initialize resilience components
	if err := s.initializeResilience(); err != nil {
		return fmt.Errorf("failed to initialize resilience: %w", err)
	}

	// Initialize correlation engine
	if err := s.initializeCorrelation(); err != nil {
		return fmt.Errorf("failed to initialize correlation: %w", err)
	}

	return nil
}

// initializeEBPF initializes eBPF components
func (s *UnifiedSystem) initializeEBPF() error {
	var err error
	s.ebpfCollector, err = ebpf.NewEnhancedCollector()
	if err != nil {
		return err
	}

	// Configure which eBPF programs to load
	if s.config.EnableNetworkMonitoring {
		if err := s.ebpfCollector.EnableNetworkMonitoring(); err != nil {
			return fmt.Errorf("failed to enable network monitoring: %w", err)
		}
	}

	if s.config.EnableDNSMonitoring {
		if err := s.ebpfCollector.EnableDNSMonitoring(); err != nil {
			return fmt.Errorf("failed to enable DNS monitoring: %w", err)
		}
	}

	if s.config.EnableProtocolAnalysis {
		if err := s.ebpfCollector.EnableProtocolAnalysis(); err != nil {
			return fmt.Errorf("failed to enable protocol analysis: %w", err)
		}
	}

	return nil
}

// initializeSources initializes data sources
func (s *UnifiedSystem) initializeSources() error {
	var err error

	// eBPF source
	s.ebpfSource = sources.NewEBPFSource()

	// Systemd source
	if s.config.EnableSystemd {
		s.systemdSource, err = sources.NewSystemdSource(nil)
		if err != nil {
			return fmt.Errorf("failed to create systemd source: %w", err)
		}
	}

	// Journald source
	if s.config.EnableJournald {
		s.journaldSource, err = sources.NewJournaldSource(nil)
		if err != nil {
			return fmt.Errorf("failed to create journald source: %w", err)
		}
	}

	return nil
}

// initializePerformance initializes performance components
func (s *UnifiedSystem) initializePerformance() error {
	var err error

	// Create object pool
	s.objectPool, err = performance.NewTypedPool[SystemEvent](
		func() *SystemEvent { return &SystemEvent{} },
		func(e *SystemEvent) { *e = SystemEvent{} },
		1000,
		s.config.EventBufferSize,
	)
	if err != nil {
		return err
	}

	// Create per-CPU buffers
	s.perCPUBuffers, err = performance.NewPerCPUBuffer(performance.PerCPUBufferConfig{
		BufferSize:   s.config.PerCPUBufferSize,
		OverflowSize: uint64(s.config.EventBufferSize),
	})
	if err != nil {
		return err
	}

	// Create event pipeline
	stages := []performance.Stage{
		performance.NewFilterStage("priority_filter", func(e *performance.Event) bool {
			return e.Priority >= 2 // Filter low priority events
		}),
		performance.NewTransformStage("enrich", func(e *performance.Event) error {
			// Enrich event with additional metadata
			e.Metadata[0] = uint64(time.Now().UnixNano())
			return nil
		}),
	}

	s.eventPipeline, err = performance.NewEventPipeline(stages, performance.DefaultPipelineConfig())
	if err != nil {
		return err
	}

	// Create batch processor
	s.batchProcessor = performance.NewAdaptiveBatchProcessor[SystemEvent](
		100,
		s.config.BatchSize,
		50*time.Millisecond,
		func(ctx context.Context, batch []SystemEvent) error {
			// Convert to pointer slice
			ptrBatch := make([]*SystemEvent, len(batch))
			for i := range batch {
				ptrBatch[i] = &batch[i]
			}
			return s.processBatch(ctx, ptrBatch)
		},
	)

	return nil
}

// initializeResilience initializes resilience components
func (s *UnifiedSystem) initializeResilience() error {
	// Circuit breaker
	if s.config.EnableCircuitBreaker {
		s.circuitBreaker = resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
			Name:         "system_circuit_breaker",
			MaxFailures:  s.config.MaxFailures,
			ResetTimeout: 60 * time.Second,
		})
	}

	// Self-healing manager
	if s.config.EnableSelfHealing {
		s.selfHealing = resilience.NewSelfHealingManager(nil)

		// Register components
		s.selfHealing.RegisterComponent(&resilience.MonitoredComponent{
			Name:   "ebpf_collector",
			Type:   resilience.ComponentTypeProcess,
			Status: resilience.StatusHealthy,
		})

		if s.config.EnableSystemd {
			s.selfHealing.RegisterComponent(&resilience.MonitoredComponent{
				Name:   "systemd_source",
				Type:   resilience.ComponentTypeService,
				Status: resilience.StatusHealthy,
			})
		}

		if s.config.EnableJournald {
			s.selfHealing.RegisterComponent(&resilience.MonitoredComponent{
				Name:   "journald_source",
				Type:   resilience.ComponentTypeService,
				Status: resilience.StatusHealthy,
			})
		}

		// Register health checker
		s.selfHealing.RegisterHealthChecker(&systemHealthChecker{system: s})

		// Register healer
		s.selfHealing.RegisterHealer(&systemHealer{system: s})
	}

	// Load shedder
	if s.config.EnableLoadShedding {
		s.loadShedder = resilience.NewLoadShedder("system_load_shedder", nil)
	}

	// Rate limiter
	s.rateLimiter = resilience.NewRateLimiter(
		"system_rate_limiter",
		s.config.MaxEventsPerSecond,
		s.config.MaxEventsPerSecond/10, // 10% burst
	)

	return nil
}

// initializeCorrelation initializes the correlation engine
func (s *UnifiedSystem) initializeCorrelation() error {
	engineConfig := correlation.DefaultEnhancedEngineConfig()
	engineConfig.CorrelationWindow = s.config.CorrelationWindow
	engineConfig.EnablePatternAnalysis = s.config.EnablePatternAnalysis

	s.correlationEngine = correlation.NewEnhancedEngine(engineConfig)

	// Add sources
	s.correlationEngine.AddSource(correlation.SourceEBPF, s.ebpfSource)

	if s.config.EnableSystemd {
		s.correlationEngine.AddSource(correlation.SourceSystemd, s.systemdSource)
	}

	if s.config.EnableJournald {
		s.correlationEngine.AddSource(correlation.SourceJournald, s.journaldSource)
	}

	return nil
}

// Start starts the unified system
func (s *UnifiedSystem) Start() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.isRunning {
		return fmt.Errorf("system already running")
	}

	// Start eBPF collector
	if err := s.ebpfCollector.Start(); err != nil {
		return fmt.Errorf("failed to start eBPF collector: %w", err)
	}

	// Start sources
	if s.config.EnableSystemd {
		if err := s.systemdSource.Start(s.ctx); err != nil {
			return fmt.Errorf("failed to start systemd source: %w", err)
		}
	}

	if s.config.EnableJournald {
		if err := s.journaldSource.Start(s.ctx); err != nil {
			return fmt.Errorf("failed to start journald source: %w", err)
		}
	}

	// Start performance components
	if err := s.eventPipeline.Start(); err != nil {
		return fmt.Errorf("failed to start event pipeline: %w", err)
	}

	if err := s.batchProcessor.Start(); err != nil {
		return fmt.Errorf("failed to start batch processor: %w", err)
	}

	// Start resilience components
	if s.config.EnableSelfHealing {
		if err := s.selfHealing.Start(); err != nil {
			return fmt.Errorf("failed to start self-healing: %w", err)
		}
	}

	// Start correlation engine
	if err := s.correlationEngine.Start(s.ctx); err != nil {
		return fmt.Errorf("failed to start correlation engine: %w", err)
	}

	// Start main processing loop
	s.wg.Add(1)
	go s.processEvents()

	// Start metrics collection
	s.wg.Add(1)
	go s.collectMetrics()

	s.isRunning = true
	return nil
}

// Stop stops the unified system
func (s *UnifiedSystem) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.isRunning {
		return nil
	}

	// Signal shutdown
	s.cancel()

	// Wait for goroutines
	s.wg.Wait()

	// Stop components in reverse order
	s.correlationEngine.Stop()

	if s.config.EnableSelfHealing {
		s.selfHealing.Stop()
	}

	s.batchProcessor.Stop()
	s.eventPipeline.Stop()

	if s.config.EnableJournald {
		s.journaldSource.Stop()
	}

	if s.config.EnableSystemd {
		s.systemdSource.Stop()
	}

	s.ebpfCollector.Stop()

	s.isRunning = false
	return nil
}

// processEvents is the main event processing loop
func (s *UnifiedSystem) processEvents() {
	defer s.wg.Done()

	// Create unified event channel
	eventChan := s.ebpfCollector.GetEventChannel()

	for {
		select {
		case <-s.ctx.Done():
			return

		case event := <-eventChan:
			// Check rate limit
			if !s.rateLimiter.Allow() {
				continue
			}

			// Check load shedding
			if s.config.EnableLoadShedding {
				req := &resilience.Request{
					Priority: resilience.Priority(s.mapEventPriority(event)),
				}
				if !s.loadShedder.ShouldAccept(s.ctx, req) {
					continue
				}
			}

			// Get event from pool
			sysEvent := s.objectPool.Get()
			sysEvent.Source = "ebpf"
			sysEvent.Type = event.Type
			sysEvent.Timestamp = event.Timestamp
			sysEvent.Data = event
			sysEvent.Priority = s.mapEventPriority(event)

			// Submit to batch processor
			if err := s.batchProcessor.Submit(*sysEvent); err != nil {
				s.objectPool.Put(sysEvent)
			}
		}
	}
}

// processBatch processes a batch of events
func (s *UnifiedSystem) processBatch(ctx context.Context, batch []*SystemEvent) error {
	// Convert to pipeline events
	pipelineEvents := make([]*performance.Event, len(batch))
	for i, sysEvent := range batch {
		pEvent := s.eventPipeline.GetEvent()
		pEvent.Type = sysEvent.Type
		pEvent.Timestamp = sysEvent.Timestamp.UnixNano()
		pEvent.Priority = uint8(sysEvent.Priority)
		pipelineEvents[i] = pEvent
	}

	// Submit to pipeline
	if err := s.eventPipeline.SubmitBatch(pipelineEvents); err != nil {
		return err
	}

	// Return events to pool
	for _, sysEvent := range batch {
		s.objectPool.Put(sysEvent)
	}

	return nil
}

// mapEventPriority maps event to priority
func (s *UnifiedSystem) mapEventPriority(event ebpf.SystemEvent) int {
	switch event.Type {
	case "error", "oom", "crash":
		return 5 // Critical
	case "warning", "throttle":
		return 3 // High
	default:
		return 1 // Normal
	}
}

// collectMetrics collects system metrics
func (s *UnifiedSystem) collectMetrics() {
	defer s.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// Update load shedder with current metrics
			if s.config.EnableLoadShedding {
				metrics := s.GetMetrics()
				s.loadShedder.UpdateMetrics(resilience.SystemMetrics{
					CurrentLoad: uint64(metrics.EventsProcessed),
					CPUUsage:    metrics.CPUUsage,
					MemoryUsage: metrics.MemoryUsage,
					ErrorRate:   metrics.ErrorRate,
				})
			}
		}
	}
}

// GetMetrics returns system metrics
func (s *UnifiedSystem) GetMetrics() SystemMetrics {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	metrics := SystemMetrics{
		Uptime:          time.Since(s.startTime),
		IsRunning:       s.isRunning,
		EventsProcessed: s.correlationEngine.GetStatistics()["timeline_events"].(int),
	}

	// eBPF metrics
	if s.ebpfCollector != nil {
		ebpfStats := s.ebpfCollector.GetStatistics()
		metrics.EBPFEvents = ebpfStats["total_events"].(uint64)
	}

	// Performance metrics
	if s.eventPipeline != nil {
		pipelineMetrics := s.eventPipeline.GetMetrics()
		metrics.PipelineThroughput = pipelineMetrics.Throughput
		metrics.PipelineLatency = pipelineMetrics.AvgLatency
	}

	if s.objectPool != nil {
		poolMetrics := s.objectPool.GetMetrics()
		metrics.MemoryAllocations = poolMetrics.Allocations
		metrics.MemoryRecycled = poolMetrics.Recycled
	}

	// Resilience metrics
	if s.circuitBreaker != nil {
		cbMetrics := s.circuitBreaker.GetMetrics()
		metrics.CircuitBreakerState = cbMetrics.State
	}

	if s.rateLimiter != nil {
		rlMetrics := s.rateLimiter.GetMetrics()
		metrics.RateLimitedEvents = rlMetrics.DeniedRequests
	}

	// Calculate resource usage (simplified)
	metrics.CPUUsage = 25.0    // Would use actual CPU metrics
	metrics.MemoryUsage = 45.0 // Would use actual memory metrics
	metrics.ErrorRate = 0.001  // Would calculate from actual errors

	return metrics
}

// SystemMetrics contains system-wide metrics
type SystemMetrics struct {
	// General
	Uptime          time.Duration
	IsRunning       bool
	EventsProcessed int

	// eBPF
	EBPFEvents uint64

	// Performance
	PipelineThroughput uint64
	PipelineLatency    time.Duration
	MemoryAllocations  uint64
	MemoryRecycled     uint64

	// Resilience
	CircuitBreakerState string
	RateLimitedEvents   uint64

	// Resources
	CPUUsage    float64
	MemoryUsage float64
	ErrorRate   float64
}

// systemHealthChecker checks system health
type systemHealthChecker struct {
	system *UnifiedSystem
}

func (c *systemHealthChecker) Name() string {
	return "system_health_checker"
}

func (c *systemHealthChecker) Check(ctx context.Context, component *resilience.MonitoredComponent) (resilience.ComponentStatus, error) {
	switch component.Name {
	case "ebpf_collector":
		if c.system.ebpfCollector == nil {
			return resilience.StatusUnhealthy, fmt.Errorf("eBPF collector not initialized")
		}
		stats := c.system.ebpfCollector.GetStatistics()
		if stats["errors"].(uint64) > 100 {
			return resilience.StatusDegraded, nil
		}
		return resilience.StatusHealthy, nil

	case "systemd_source":
		if c.system.systemdSource == nil || !c.system.systemdSource.IsAvailable() {
			return resilience.StatusUnhealthy, fmt.Errorf("systemd source not available")
		}
		return resilience.StatusHealthy, nil

	case "journald_source":
		if c.system.journaldSource == nil || !c.system.journaldSource.IsAvailable() {
			return resilience.StatusUnhealthy, fmt.Errorf("journald source not available")
		}
		return resilience.StatusHealthy, nil

	default:
		return resilience.StatusUnknown, nil
	}
}

func (c *systemHealthChecker) GetDetails() map[string]interface{} {
	return map[string]interface{}{
		"checker": "system",
	}
}

// systemHealer heals system components
type systemHealer struct {
	system *UnifiedSystem
}

func (h *systemHealer) Name() string {
	return "system_healer"
}

func (h *systemHealer) CanHeal(component *resilience.MonitoredComponent) bool {
	return component.Type == resilience.ComponentTypeService
}

func (h *systemHealer) Heal(ctx context.Context, component *resilience.MonitoredComponent) error {
	switch component.Name {
	case "systemd_source":
		// Restart systemd source
		if h.system.systemdSource != nil {
			h.system.systemdSource.Stop()
			time.Sleep(1 * time.Second)
			return h.system.systemdSource.Start(ctx)
		}

	case "journald_source":
		// Restart journald source
		if h.system.journaldSource != nil {
			h.system.journaldSource.Stop()
			time.Sleep(1 * time.Second)
			return h.system.journaldSource.Start(ctx)
		}
	}

	return nil
}

func (h *systemHealer) GetActions() []resilience.HealingAction {
	return []resilience.HealingAction{
		{
			Name:        "restart_service",
			Description: "Restart the service",
			Risk:        resilience.RiskLow,
			Duration:    2 * time.Second,
		},
	}
}
