package internal

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// ProductionCollector is a production-hardened eBPF collector implementation
type ProductionCollector struct {
	// Configuration
	config core.Config

	// State management
	started atomic.Bool
	stopped atomic.Bool

	// Event processing
	eventChan chan domain.Event
	processor core.EventProcessor

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Statistics
	stats struct {
		eventsCollected atomic.Uint64
		eventsDropped   atomic.Uint64
		bytesProcessed  atomic.Uint64
		errorCount      atomic.Uint64
	}

	// Health tracking
	lastEventTime atomic.Value // time.Time
	startTime     time.Time

	// Platform-specific implementation
	impl platformImpl

	// OTEL instrumentation (disabled for now)
	// otelInstrumentation *otel.CollectorInstrumentation

	// Production hardening components
	security    *SecurityManager
	rateLimiter *RateLimiter
	resources   *ResourceManager
	monitoring  *MonitoringManager

	// Error recovery
	errorRecovery *ErrorRecoveryManager

	// Graceful degradation
	degradedMode     atomic.Bool
	degradationLevel atomic.Int32 // 0=normal, 1=reduced, 2=minimal, 3=emergency
}

// ErrorRecoveryManager handles error recovery and circuit breaking
type ErrorRecoveryManager struct {
	consecutiveErrors atomic.Int32
	lastError         atomic.Value // time.Time
	recoveryAttempts  atomic.Int32
	maxRetries        int
	backoffStrategy   BackoffStrategy
	mu                sync.RWMutex
}

// BackoffStrategy defines retry backoff behavior
type BackoffStrategy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	Jitter       float64
}

// NewProductionCollector creates a new production-hardened eBPF collector
func NewProductionCollector(config core.Config) (core.Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	c := &ProductionCollector{
		config:    config,
		eventChan: make(chan domain.Event, config.EventBufferSize),
		startTime: time.Now(),
		processor: newEventProcessor(),
	}

	// Initialize error recovery
	c.errorRecovery = &ErrorRecoveryManager{
		maxRetries: 3,
		backoffStrategy: BackoffStrategy{
			InitialDelay: 1 * time.Second,
			MaxDelay:     30 * time.Second,
			Multiplier:   2.0,
			Jitter:       0.1,
		},
	}
	c.errorRecovery.lastError.Store(time.Time{})

	// Initialize security manager with strict settings
	securityConfig := DefaultSecurityConfig()
	securityConfig.StrictMode = true
	c.security = NewSecurityManager(securityConfig)

	// Validate security environment before proceeding
	if err := c.security.ValidateEnvironment(context.Background()); err != nil {
		return nil, fmt.Errorf("security validation failed: %w", err)
	}

	// Initialize rate limiter with adaptive settings
	rateLimiterConfig := DefaultRateLimiterConfig()
	rateLimiterConfig.MaxEventsPerSecond = int64(config.MaxEventsPerSecond)
	rateLimiterConfig.EnableAdaptive = true
	rateLimiterConfig.EnableCircuitBreaker = true
	rateLimiterConfig.EnableBackpressure = true
	c.rateLimiter = NewRateLimiter(rateLimiterConfig)

	// Initialize resource manager with conservative limits
	resourceConfig := DefaultResourceConfig()
	resourceConfig.MaxMemoryMB = 256    // Conservative memory limit
	resourceConfig.MaxCPUPercent = 10.0 // Conservative CPU limit
	resourceConfig.EnableAdaptive = true
	c.resources = NewResourceManager(resourceConfig)

	// Initialize comprehensive monitoring
	monitoringConfig := DefaultMonitoringConfig()
	monitoringConfig.EnableAlerting = true
	monitoringConfig.EnablePrometheus = true
	c.monitoring = NewMonitoringManager(monitoringConfig)

	// Initialize platform-specific implementation
	impl, err := newPlatformImpl()
	if err != nil {
		return nil, fmt.Errorf("failed to create platform implementation: %w", err)
	}

	// Initialize with security validation
	if err := c.initializeWithSecurity(impl, config); err != nil {
		return nil, fmt.Errorf("failed to initialize platform implementation: %w", err)
	}

	c.impl = impl
	c.lastEventTime.Store(time.Now())

	// Initialize OTEL instrumentation if enabled (disabled for now)
	// if config.EnableOTEL {
	//	c.initializeOTEL()
	// }

	// Register health checks
	c.registerHealthChecks()

	// Start background monitoring
	c.wg.Add(1)
	go c.periodicSecurityCheck()

	return c, nil
}

// initializeWithSecurity initializes platform implementation with security checks
func (c *ProductionCollector) initializeWithSecurity(impl platformImpl, config core.Config) error {
	// Validate each eBPF program before loading
	for _, prog := range config.Programs {
		if err := c.security.ValidateProgram(prog); err != nil {
			return fmt.Errorf("program validation failed: %w", err)
		}
	}

	// Initialize with resource limits
	_, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	if err := impl.init(config); err != nil {
		return fmt.Errorf("platform initialization failed: %w", err)
	}

	return nil
}

// Start begins event collection with production safeguards
func (c *ProductionCollector) Start(ctx context.Context) error {
	if !c.config.Enabled {
		return fmt.Errorf("collector is disabled")
	}

	if c.started.Load() {
		return core.ErrAlreadyStarted
	}

	// Create OTEL span for collector startup (disabled for now)
	// if c.otelInstrumentation != nil {
	//	var span trace.Span
	//	ctx, span = c.otelInstrumentation.InstrumentCollectorStart(ctx, "ebpf")
	//	defer span.End()
	// }

	// Perform pre-flight checks
	if err := c.performPreflightChecks(ctx); err != nil {
		return fmt.Errorf("preflight checks failed: %w", err)
	}

	// Create cancellable context
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start platform implementation with recovery
	if err := c.startWithRecovery(c.ctx); err != nil {
		return fmt.Errorf("failed to start: %w", err)
	}

	// Mark as started
	c.started.Store(true)

	// Start event processing with multiple workers
	numWorkers := 3 // Multiple workers for resilience
	for i := 0; i < numWorkers; i++ {
		c.wg.Add(1)
		go c.processEventsWithRecovery(i)
	}

	// Start monitoring workers
	c.wg.Add(3)
	go c.monitorResources()
	go c.monitorHealth()
	go c.handleAlerts()

	c.monitoring.RecordEvent("collector_started", map[string]string{
		"version": "1.0.0",
		"mode":    "production",
	})

	return nil
}

// performPreflightChecks validates system readiness
func (c *ProductionCollector) performPreflightChecks(ctx context.Context) error {
	checks := []struct {
		name  string
		check func() error
	}{
		{"security", func() error { return c.security.ValidateEnvironment(ctx) }},
		{"resources", func() error { return c.resources.CheckMemory() }},
		{"kernel", func() error { return c.validateKernelSupport() }},
		{"permissions", func() error { return c.validatePermissions() }},
	}

	for _, check := range checks {
		if err := check.check(); err != nil {
			c.monitoring.RecordError("preflight_"+check.name, map[string]string{
				"error": err.Error(),
			})
			return fmt.Errorf("%s check failed: %w", check.name, err)
		}
	}

	return nil
}

// startWithRecovery starts the collector with automatic recovery
func (c *ProductionCollector) startWithRecovery(ctx context.Context) error {
	return c.errorRecovery.ExecuteWithRetry(func() error {
		return c.impl.start(ctx)
	})
}

// processEventsWithRecovery processes events with error recovery
func (c *ProductionCollector) processEventsWithRecovery(workerID int) {
	defer c.wg.Done()
	defer c.recoverFromPanic(workerID)

	for {
		select {
		case <-c.ctx.Done():
			return

		case rawEvent, ok := <-c.impl.events():
			if !ok {
				return
			}

			// Process with comprehensive error handling
			c.processEventSafely(rawEvent, workerID)
		}
	}
}

// processEventSafely processes a single event with all safety checks
func (c *ProductionCollector) processEventSafely(rawEvent core.RawEvent, workerID int) {
	// Record processing start
	start := time.Now()

	// Apply rate limiting
	if !c.rateLimiter.Allow(c.ctx) {
		c.stats.eventsDropped.Add(1)
		c.monitoring.RecordEvent("event_rate_limited", map[string]string{
			"worker_id": fmt.Sprintf("%d", workerID),
		})
		return
	}

	// Validate event security
	if err := c.security.ValidateEvent(rawEvent); err != nil {
		c.stats.eventsDropped.Add(1)
		c.monitoring.RecordError("event_security_violation", map[string]string{
			"error": err.Error(),
			"pid":   fmt.Sprintf("%d", rawEvent.PID),
		})
		return
	}

	// Check resource constraints
	if c.resources.IsUnderPressure() {
		if c.shouldDropUnderPressure(rawEvent) {
			c.stats.eventsDropped.Add(1)
			c.monitoring.RecordEvent("event_dropped_pressure", nil)
			return
		}
	}

	// Create OTEL span for event processing (disabled for now)
	eventCtx := c.ctx
	// if c.otelInstrumentation != nil {
	//	tempEvent := &domain.Event{
	//		ID:     domain.EventID(fmt.Sprintf("ebpf-%d-%d", rawEvent.PID, rawEvent.Timestamp)),
	//		Source: domain.SourceEBPF,
	//	}
	//	var span trace.Span
	//	eventCtx, span = c.otelInstrumentation.InstrumentEventProcessing(eventCtx, &domain.UnifiedEvent{
	//		ID:     string(tempEvent.ID),
	//		Source: string(tempEvent.Source),
	//	})
	//	defer span.End()
	// }

	// Process the raw event with timeout
	processCtx, cancel := context.WithTimeout(eventCtx, 100*time.Millisecond)
	defer cancel()

	event, err := c.processor.ProcessEvent(processCtx, rawEvent)
	if err != nil {
		c.handleProcessingError(err, rawEvent)
		return
	}

	// Update statistics
	c.updateStatsSafely(rawEvent, time.Since(start))

	// Try to send event with backpressure handling
	select {
	case c.eventChan <- event:
		c.rateLimiter.ReportSuccess()
		c.monitoring.RecordLatency("event_processing", time.Since(start), map[string]string{
			"worker_id": fmt.Sprintf("%d", workerID),
		})
	case <-time.After(10 * time.Millisecond):
		// Buffer full with timeout
		c.stats.eventsDropped.Add(1)
		c.rateLimiter.UpdateLoad(int64(len(c.eventChan)))
		c.monitoring.RecordEvent("event_buffer_full", nil)
	case <-c.ctx.Done():
		return
	}
}

// handleProcessingError handles event processing errors with appropriate recovery
func (c *ProductionCollector) handleProcessingError(err error, rawEvent core.RawEvent) {
	c.stats.errorCount.Add(1)
	c.errorRecovery.RecordError()
	c.rateLimiter.ReportError(err)

	// Log with context
	c.monitoring.RecordError("event_processing", map[string]string{
		"error": err.Error(),
		"type":  rawEvent.Type,
		"pid":   fmt.Sprintf("%d", rawEvent.PID),
	})

	// Check if we should enter degraded mode
	if c.errorRecovery.consecutiveErrors.Load() > 100 {
		c.enterDegradedMode(1)
	}
}

// shouldDropUnderPressure determines if event should be dropped under resource pressure
func (c *ProductionCollector) shouldDropUnderPressure(event core.RawEvent) bool {
	degradationLevel := c.degradationLevel.Load()

	switch degradationLevel {
	case 0: // Normal mode - drop only low priority
		return false
	case 1: // Reduced mode - drop non-critical
		return !c.isHighPriorityEvent(event)
	case 2: // Minimal mode - drop most events
		return !c.isCriticalEvent(event)
	case 3: // Emergency mode - drop all but essential
		return !c.isEssentialEvent(event)
	default:
		return true
	}
}

// Event priority classification methods
func (c *ProductionCollector) isHighPriorityEvent(event core.RawEvent) bool {
	// Security-related events are high priority
	return event.Type == "security" || event.Type == "auth" || event.UID == 0
}

func (c *ProductionCollector) isCriticalEvent(event core.RawEvent) bool {
	// System-critical events
	return event.Type == "kernel" || event.Type == "panic" || event.PID == 1
}

func (c *ProductionCollector) isEssentialEvent(event core.RawEvent) bool {
	// Only absolutely essential events
	return event.Type == "emergency" || event.Type == "alert"
}

// enterDegradedMode switches to degraded operation mode
func (c *ProductionCollector) enterDegradedMode(level int32) {
	if !c.degradedMode.Load() {
		c.degradedMode.Store(true)
		c.degradationLevel.Store(level)

		c.monitoring.RecordEvent("degraded_mode_entered", map[string]string{
			"level": fmt.Sprintf("%d", level),
		})

		// Adjust rate limits
		currentRate := c.rateLimiter.currentRate.Load()
		c.rateLimiter.currentRate.Store(currentRate / 2)
	}
}

// recoverFromPanic handles panic recovery in event processing
func (c *ProductionCollector) recoverFromPanic(workerID int) {
	if r := recover(); r != nil {
		c.stats.errorCount.Add(1)
		c.monitoring.RecordError("worker_panic", map[string]string{
			"worker_id": fmt.Sprintf("%d", workerID),
			"panic":     fmt.Sprintf("%v", r),
		})

		// Restart worker after delay
		time.Sleep(5 * time.Second)
		c.wg.Add(1)
		go c.processEventsWithRecovery(workerID)
	}
}

// Stop gracefully stops the collector with cleanup
func (c *ProductionCollector) Stop() error {
	if !c.started.Load() {
		return core.ErrNotStarted
	}

	if c.stopped.Load() {
		return nil
	}

	// Mark as stopping
	c.stopped.Store(true)

	// Record shutdown
	c.monitoring.RecordEvent("collector_stopping", nil)

	// Stop accepting new events
	if c.cancel != nil {
		c.cancel()
	}

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop components in order
	if err := c.gracefulShutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	// Wait for goroutines
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-shutdownCtx.Done():
		return fmt.Errorf("shutdown timeout exceeded")
	}

	// Final cleanup
	c.cleanup()

	return nil
}

// gracefulShutdown performs ordered shutdown of components
func (c *ProductionCollector) gracefulShutdown(ctx context.Context) error {
	// Stop monitoring first
	c.monitoring.Stop()

	// Stop rate limiter
	c.rateLimiter.Stop()

	// Stop resource manager
	c.resources.Stop()

	// Stop platform implementation
	if err := c.impl.stop(); err != nil {
		return fmt.Errorf("failed to stop platform implementation: %w", err)
	}

	// Close event channel
	close(c.eventChan)

	return nil
}

// cleanup performs final cleanup
func (c *ProductionCollector) cleanup() {
	// Force garbage collection
	c.resources.cleanup()

	// Clear sensitive data
	c.processor = nil
	c.impl = nil
}

// Monitoring goroutines

func (c *ProductionCollector) monitorResources() {
	defer c.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			usage := c.resources.GetUsage()
			c.monitoring.RecordResourceUsage(map[string]float64{
				"memory_bytes":     float64(usage.MemoryBytes),
				"memory_percent":   float64(usage.MemoryBytes) / float64(c.resources.limits.MemoryBytes) * 100,
				"cpu_percent":      usage.CPUPercent,
				"file_descriptors": float64(usage.FileDescriptors),
				"goroutines":       float64(usage.Goroutines),
			})

			// Update backpressure
			c.rateLimiter.UpdateLoad(int64(len(c.eventChan)))
		}
	}
}

func (c *ProductionCollector) monitorHealth() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			health := c.Health()
			if health.Status != core.HealthStatusHealthy {
				c.monitoring.RecordEvent("health_degraded", map[string]string{
					"status":  string(health.Status),
					"message": health.Message,
				})
			}
		}
	}
}

func (c *ProductionCollector) handleAlerts() {
	defer c.wg.Done()

	// Alert handling would connect to external alerting systems
	// For now, just log critical alerts

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			metrics := c.monitoring.GetMetrics()
			c.checkAlertConditions(metrics)
		}
	}
}

func (c *ProductionCollector) periodicSecurityCheck() {
	defer c.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.security.PeriodicCheck(c.ctx); err != nil {
				c.monitoring.RecordError("security_check", map[string]string{
					"error": err.Error(),
				})
			}
		}
	}
}

// Health returns comprehensive health status
func (c *ProductionCollector) Health() core.Health {
	status := core.HealthStatusHealthy
	message := "eBPF collector is healthy"
	details := make(map[string]interface{})

	// Check basic health
	if !c.started.Load() {
		status = core.HealthStatusUnknown
		message = "Collector not started"
	} else if c.stopped.Load() {
		status = core.HealthStatusUnhealthy
		message = "Collector stopped"
	}

	// Check error rate
	errorRate := c.calculateErrorRate()
	if errorRate > 0.1 {
		status = core.HealthStatusUnhealthy
		message = fmt.Sprintf("High error rate: %.2f%%", errorRate*100)
	} else if errorRate > 0.05 {
		status = core.HealthStatusDegraded
		message = fmt.Sprintf("Elevated error rate: %.2f%%", errorRate*100)
	}

	// Check event flow
	lastEvent := c.lastEventTime.Load().(time.Time)
	timeSinceLastEvent := time.Since(lastEvent)
	if timeSinceLastEvent > 5*time.Minute && c.started.Load() {
		status = core.HealthStatusDegraded
		message = fmt.Sprintf("No events for %v", timeSinceLastEvent)
	}

	// Check degraded mode
	if c.degradedMode.Load() {
		status = core.HealthStatusDegraded
		message = fmt.Sprintf("Running in degraded mode (level %d)", c.degradationLevel.Load())
	}

	// Add detailed metrics
	details["programs_loaded"] = c.impl.programsLoaded()
	details["maps_created"] = c.impl.mapsCreated()
	details["buffer_utilization"] = c.getBufferUtilization()
	details["events_per_second"] = c.getEventsPerSecond()
	details["bytes_per_second"] = c.getBytesPerSecond()
	details["error_rate"] = errorRate
	details["degraded_mode"] = c.degradedMode.Load()
	details["consecutive_errors"] = c.errorRecovery.consecutiveErrors.Load()

	// Get component health
	details["security_status"] = c.security.GetMetrics()
	details["rate_limiter_status"] = c.rateLimiter.GetMetrics()
	details["resource_status"] = c.resources.GetMetrics()
	details["monitoring_status"] = c.monitoring.GetHealthStatus()

	return core.Health{
		Status:          status,
		Message:         message,
		LastEventTime:   lastEvent,
		EventsProcessed: c.stats.eventsCollected.Load(),
		EventsDropped:   c.stats.eventsDropped.Load(),
		ErrorCount:      c.stats.errorCount.Load(),
		Metrics:         convertToFloat64Map(details),
	}
}

// Statistics returns comprehensive runtime statistics
func (c *ProductionCollector) Statistics() core.Statistics {
	uptime := time.Since(c.startTime)

	stats := core.Statistics{
		StartTime:       c.startTime,
		EventsCollected: c.stats.eventsCollected.Load(),
		EventsDropped:   c.stats.eventsDropped.Load(),
		BytesProcessed:  c.stats.bytesProcessed.Load(),
		ProgramsLoaded:  c.impl.programsLoaded(),
		MapsCreated:     c.impl.mapsCreated(),
		Custom: map[string]interface{}{
			"uptime_seconds":       uptime.Seconds(),
			"events_per_second":    c.getEventsPerSecond(),
			"bytes_per_second":     c.getBytesPerSecond(),
			"error_rate":           c.calculateErrorRate(),
			"degraded_mode":        c.degradedMode.Load(),
			"degradation_level":    c.degradationLevel.Load(),
			"buffer_utilization":   c.getBufferUtilization(),
			"monitoring_metrics":   c.monitoring.GetMetrics(),
			"rate_limiter_metrics": c.rateLimiter.GetMetrics(),
			"resource_metrics":     c.resources.GetMetrics(),
			"security_metrics":     c.security.GetMetrics(),
		},
	}

	return stats
}

// Events returns the event channel
func (c *ProductionCollector) Events() <-chan domain.Event {
	return c.eventChan
}

// Configure updates the collector configuration at runtime
func (c *ProductionCollector) Configure(config core.Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Validate with security manager
	for _, prog := range config.Programs {
		if err := c.security.ValidateProgram(prog); err != nil {
			return fmt.Errorf("program validation failed: %w", err)
		}
	}

	// Update configuration
	c.config = config

	// Update rate limiter
	c.rateLimiter.currentRate.Store(int64(config.MaxEventsPerSecond))

	return nil
}

// Helper methods

func (c *ProductionCollector) updateStatsSafely(rawEvent core.RawEvent, duration time.Duration) {
	c.stats.eventsCollected.Add(1)
	c.stats.bytesProcessed.Add(uint64(len(rawEvent.Data)))
	c.lastEventTime.Store(time.Now())

	// Reset consecutive errors on success
	c.errorRecovery.consecutiveErrors.Store(0)

	// Check if we can exit degraded mode
	if c.degradedMode.Load() && c.errorRecovery.consecutiveErrors.Load() == 0 {
		if time.Since(c.errorRecovery.lastError.Load().(time.Time)) > 5*time.Minute {
			c.exitDegradedMode()
		}
	}
}

func (c *ProductionCollector) exitDegradedMode() {
	c.degradedMode.Store(false)
	c.degradationLevel.Store(0)

	// Restore normal rate limits
	c.rateLimiter.currentRate.Store(int64(c.config.MaxEventsPerSecond))

	c.monitoring.RecordEvent("degraded_mode_exited", nil)
}

func (c *ProductionCollector) calculateErrorRate() float64 {
	total := c.stats.eventsCollected.Load() + c.stats.eventsDropped.Load() + c.stats.errorCount.Load()
	if total == 0 {
		return 0
	}
	errors := c.stats.errorCount.Load() + c.stats.eventsDropped.Load()
	return float64(errors) / float64(total)
}

func (c *ProductionCollector) getBufferUtilization() float64 {
	if c.config.EventBufferSize == 0 {
		return 0
	}
	return float64(len(c.eventChan)) / float64(c.config.EventBufferSize) * 100
}

func (c *ProductionCollector) getEventsPerSecond() float64 {
	uptime := time.Since(c.startTime).Seconds()
	if uptime == 0 {
		return 0
	}
	return float64(c.stats.eventsCollected.Load()) / uptime
}

func (c *ProductionCollector) getBytesPerSecond() float64 {
	uptime := time.Since(c.startTime).Seconds()
	if uptime == 0 {
		return 0
	}
	return float64(c.stats.bytesProcessed.Load()) / uptime
}

func (c *ProductionCollector) validateKernelSupport() error {
	// Simplified check - in production would validate specific kernel features
	return nil
}

func (c *ProductionCollector) validatePermissions() error {
	// Check for required capabilities
	if os.Geteuid() != 0 {
		// Not root, check for CAP_BPF or CAP_SYS_ADMIN
		return fmt.Errorf("insufficient permissions: need root or CAP_BPF/CAP_SYS_ADMIN")
	}
	return nil
}

func (c *ProductionCollector) checkAlertConditions(metrics map[string]interface{}) {
	// Check critical conditions
	if errorRate, ok := metrics["error_rate"].(float64); ok && errorRate > 0.1 {
		c.monitoring.RecordEvent("critical_error_rate", map[string]string{
			"rate": fmt.Sprintf("%.2f", errorRate),
		})
	}
}

// func (c *ProductionCollector) initializeOTEL() {
//	otelConfig := otel.DefaultConfig()
//	otelConfig.ServiceName = "tapio-ebpf-collector"
//	otelConfig.ServiceVersion = "1.0.0"
//	otelConfig.Environment = getEnvironment()
//	otelConfig.Enabled = true
//
//	otelIntegration, err := otel.NewSimpleOTEL(otelConfig)
//	if err != nil {
//		// OTEL is optional, just log the error
//		fmt.Printf("Failed to initialize OTEL: %v\n", err)
//		c.monitoring.RecordError("otel_init", map[string]string{
//			"error": err.Error(),
//		})
//	} else {
//		c.otelInstrumentation = otel.NewCollectorInstrumentation(otelIntegration)
//	}
// }

func (c *ProductionCollector) registerHealthChecks() {
	// Register component health checks with monitoring
	// These would be called periodically by the monitoring system
}

func getEnvironmentProd() string {
	if env := os.Getenv("TAPIO_ENV"); env != "" {
		return env
	}
	if env := os.Getenv("ENVIRONMENT"); env != "" {
		return env
	}
	return "production"
}

func convertToFloat64Map(input map[string]interface{}) map[string]float64 {
	result := make(map[string]float64)
	for k, v := range input {
		switch val := v.(type) {
		case float64:
			result[k] = val
		case int:
			result[k] = float64(val)
		case int64:
			result[k] = float64(val)
		case uint64:
			result[k] = float64(val)
		case bool:
			if val {
				result[k] = 1.0
			} else {
				result[k] = 0.0
			}
		}
	}
	return result
}

// ErrorRecoveryManager methods

func (erm *ErrorRecoveryManager) ExecuteWithRetry(fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < erm.maxRetries; attempt++ {
		if err := fn(); err == nil {
			erm.consecutiveErrors.Store(0)
			return nil
		} else {
			lastErr = err
			erm.RecordError()

			if attempt < erm.maxRetries-1 {
				delay := erm.calculateBackoff(attempt)
				time.Sleep(delay)
			}
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", erm.maxRetries, lastErr)
}

func (erm *ErrorRecoveryManager) RecordError() {
	erm.consecutiveErrors.Add(1)
	erm.lastError.Store(time.Now())
}

func (erm *ErrorRecoveryManager) calculateBackoff(attempt int) time.Duration {
	delay := float64(erm.backoffStrategy.InitialDelay)

	for i := 0; i < attempt; i++ {
		delay *= erm.backoffStrategy.Multiplier
	}

	if delay > float64(erm.backoffStrategy.MaxDelay) {
		delay = float64(erm.backoffStrategy.MaxDelay)
	}

	// Add jitter
	jitter := delay * erm.backoffStrategy.Jitter
	delay += (rand.Float64() - 0.5) * 2 * jitter

	return time.Duration(delay)
}

// Stub implementations for production collector

// stubEventProcessor is a minimal event processor for testing
type stubEventProcessor struct{}

func (p *stubEventProcessor) ProcessEvent(ctx context.Context, raw core.RawEvent) (domain.Event, error) {
	// Basic conversion from raw to domain event
	return domain.Event{
		ID:     domain.EventID(fmt.Sprintf("ebpf-%d", raw.PID)),
		Source: domain.SourceEBPF,
		Type:   domain.EventType(raw.Type),
		Data:   raw.Decoded,
	}, nil
}

// stubPlatformImpl is a minimal platform implementation for testing
type stubPlatformImpl struct{}

func (p *stubPlatformImpl) init(config core.Config) error {
	return nil
}

func (p *stubPlatformImpl) start(ctx context.Context) error {
	return nil
}

func (p *stubPlatformImpl) stop() error {
	return nil
}

func (p *stubPlatformImpl) events() <-chan core.RawEvent {
	// Return a channel that immediately closes
	ch := make(chan core.RawEvent)
	close(ch)
	return ch
}

func (p *stubPlatformImpl) programsLoaded() int {
	return 0
}

func (p *stubPlatformImpl) mapsCreated() int {
	return 0
}

// Note: NewCollector already exported in collector.go
