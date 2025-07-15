package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// MetricManager orchestrates graceful shutdown with proper resource cleanup and metric flushing
type MetricManager struct {
	// Dependencies
	logger  *slog.Logger
	factory MetricFactory

	// State management
	mu           sync.RWMutex
	running      int32
	shutdownOnce sync.Once
	shutdown     chan struct{}
	shutdownDone chan struct{}
	components   map[string]ShutdownComponent

	// Configuration
	config ShutdownConfig

	// Signal handling
	signalChannel  chan os.Signal
	signalHandlers map[os.Signal]SignalHandler

	// Cleanup coordination
	cleanupTasks   []CleanupTask
	flushTasks     []FlushTask
	shutdownPhases []ShutdownPhase

	// Performance tracking
	shutdownStats ShutdownStats

	// Resource management
	resourceManager *ResourceManager
	metricFlusher   *MetricFlusher
	healthChecker   *HealthChecker
}

// ShutdownComponent represents a component that needs graceful shutdown
type ShutdownComponent interface {
	// Shutdown gracefully shuts down the component
	Shutdown(ctx context.Context) error

	// GetShutdownPriority returns shutdown priority (higher numbers shut down first)
	GetShutdownPriority() int

	// GetShutdownTimeout returns maximum time allowed for shutdown
	GetShutdownTimeout() time.Duration

	// GetComponentName returns the component name for logging
	GetComponentName() string

	// IsHealthy returns current health status
	IsHealthy() bool
}

// ShutdownConfig configures graceful shutdown behavior
type ShutdownConfig struct {
	// Timeout configuration
	GlobalShutdownTimeout time.Duration
	ComponentTimeout      time.Duration
	FlushTimeout          time.Duration
	CleanupTimeout        time.Duration

	// Signal handling
	GracefulSignals      []os.Signal
	ForceShutdownSignals []os.Signal
	SignalTimeout        time.Duration

	// Shutdown phases
	EnablePhases      bool
	PhaseTimeout      time.Duration
	WaitBetweenPhases time.Duration

	// Resource management
	EnableResourceTracking bool
	ResourceCleanupTimeout time.Duration
	ForceCleanup           bool

	// Metrics flushing
	EnableMetricFlushing bool
	FlushInterval        time.Duration
	MaxFlushAttempts     int
	FlushRetryBackoff    time.Duration

	// Health checking
	EnableHealthChecks  bool
	HealthCheckInterval time.Duration
	HealthCheckTimeout  time.Duration
	UnhealthyThreshold  int

	// Error handling
	ContinueOnError bool
	MaxErrors       int
	ErrorTimeout    time.Duration

	// Monitoring
	EnableShutdownMetrics  bool
	MetricsFlushOnShutdown bool
}

// Supporting types
type (
	SignalHandler func(os.Signal) error
	CleanupTask   func(context.Context) error
	FlushTask     func(context.Context) error
	ShutdownPhase struct {
		Name       string
		Priority   int
		Components []string
		Timeout    time.Duration
		Required   bool
	}

	ShutdownStats struct {
		StartTime        time.Time
		EndTime          time.Time
		TotalDuration    time.Duration
		ComponentCount   int
		SuccessfulCount  int
		ErrorCount       int
		TimeoutCount     int
		FlushedMetrics   int64
		CleanedResources int64
		Phases           []PhaseStats
	}

	PhaseStats struct {
		Name      string
		StartTime time.Time
		Duration  time.Duration
		Success   bool
		Error     error
	}

	ComponentStats struct {
		Name         string
		StartTime    time.Time
		Duration     time.Duration
		Success      bool
		Error        error
		Priority     int
		Timeout      time.Duration
		FlushedItems int64
	}
)

// ResourceManager tracks and cleans up resources
type ResourceManager struct {
	mu           sync.RWMutex
	resources    map[string]ManagedResource
	cleanupFuncs map[string]func() error
	stats        ResourceStats
	logger       *slog.Logger
}

// ManagedResource represents a resource that needs cleanup
type ManagedResource interface {
	GetResourceID() string
	GetResourceType() string
	GetSize() int64
	Cleanup(ctx context.Context) error
	IsActive() bool
}

// MetricFlusher handles final metric flushing
type MetricFlusher struct {
	mu             sync.RWMutex
	clients        []MetricClient[MetricType]
	pendingMetrics []MetricType
	flushStats     FlushStats
	logger         *slog.Logger
}

// HealthChecker monitors component health during shutdown
type HealthChecker struct {
	mu            sync.RWMutex
	components    map[string]ShutdownComponent
	healthStats   HealthStats
	alertHandlers []HealthAlertHandler
	logger        *slog.Logger
}

// Supporting stats types
type (
	ResourceStats struct {
		TotalResources   int64
		ActiveResources  int64
		CleanedResources int64
		MemoryFreed      int64
		ErrorCount       int64
	}

	FlushStats struct {
		TotalFlushes      int64
		SuccessfulFlushes int64
		FailedFlushes     int64
		MetricsFlushed    int64
		TotalDuration     time.Duration
	}

	HealthStats struct {
		ComponentsChecked   int64
		HealthyComponents   int64
		UnhealthyComponents int64
		CheckErrors         int64
		LastCheck           time.Time
	}

	HealthAlertHandler func(componentName string, healthy bool, error error)
)

// NewMetricManager creates a new metric manager with graceful shutdown capabilities
func NewMetricManager(factory MetricFactory, config ShutdownConfig, logger *slog.Logger) *MetricManager {
	// Apply defaults
	applyShutdownDefaults(&config)

	if logger == nil {
		logger = slog.Default().With("component", "metric-manager")
	}

	manager := &MetricManager{
		logger:         logger,
		factory:        factory,
		shutdown:       make(chan struct{}),
		shutdownDone:   make(chan struct{}),
		components:     make(map[string]ShutdownComponent),
		config:         config,
		signalChannel:  make(chan os.Signal, 1),
		signalHandlers: make(map[os.Signal]SignalHandler),
		shutdownStats: ShutdownStats{
			StartTime: time.Now(),
		},
	}

	// Initialize sub-components
	manager.resourceManager = NewResourceManager(logger.With("sub-component", "resource-manager"))
	manager.metricFlusher = NewMetricFlusher(logger.With("sub-component", "metric-flusher"))
	manager.healthChecker = NewHealthChecker(logger.With("sub-component", "health-checker"))

	// Set up signal handling
	manager.setupSignalHandling()

	// Start background tasks
	if config.EnableHealthChecks {
		go manager.runHealthChecks()
	}

	if config.EnableResourceTracking {
		go manager.runResourceTracking()
	}

	atomic.StoreInt32(&manager.running, 1)

	return manager
}

// RegisterComponent registers a component for graceful shutdown
func (m *MetricManager) RegisterComponent(name string, component ShutdownComponent) error {
	if atomic.LoadInt32(&m.running) == 0 {
		return fmt.Errorf("metric manager is not running")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.components[name]; exists {
		return fmt.Errorf("component %s is already registered", name)
	}

	m.components[name] = component
	m.shutdownStats.ComponentCount++

	// Register with health checker
	if m.config.EnableHealthChecks {
		m.healthChecker.RegisterComponent(name, component)
	}

	m.logger.Info("Component registered for shutdown",
		"component", name,
		"priority", component.GetShutdownPriority(),
		"timeout", component.GetShutdownTimeout())

	return nil
}

// UnregisterComponent removes a component from shutdown management
func (m *MetricManager) UnregisterComponent(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.components[name]; !exists {
		return fmt.Errorf("component %s is not registered", name)
	}

	delete(m.components, name)
	m.shutdownStats.ComponentCount--

	// Unregister from health checker
	if m.config.EnableHealthChecks {
		m.healthChecker.UnregisterComponent(name)
	}

	m.logger.Info("Component unregistered", "component", name)

	return nil
}

// AddCleanupTask adds a cleanup task to be executed during shutdown
func (m *MetricManager) AddCleanupTask(task CleanupTask) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cleanupTasks = append(m.cleanupTasks, task)
}

// AddFlushTask adds a metric flush task to be executed during shutdown
func (m *MetricManager) AddFlushTask(task FlushTask) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.flushTasks = append(m.flushTasks, task)
}

// RegisterSignalHandler registers a custom signal handler
func (m *MetricManager) RegisterSignalHandler(sig os.Signal, handler SignalHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.signalHandlers[sig] = handler
}

// Shutdown initiates graceful shutdown
func (m *MetricManager) Shutdown(ctx context.Context) error {
	var shutdownError error

	m.shutdownOnce.Do(func() {
		m.logger.Info("Starting graceful shutdown")
		m.shutdownStats.StartTime = time.Now()

		// Stop accepting new work
		atomic.StoreInt32(&m.running, 0)

		// Signal shutdown to all goroutines
		close(m.shutdown)

		// Execute shutdown phases
		shutdownError = m.executeShutdownPhases(ctx)

		// Final cleanup
		m.performFinalCleanup(ctx)

		// Record completion
		m.shutdownStats.EndTime = time.Now()
		m.shutdownStats.TotalDuration = m.shutdownStats.EndTime.Sub(m.shutdownStats.StartTime)

		// Signal shutdown completion
		close(m.shutdownDone)

		m.logger.Info("Graceful shutdown completed",
			"duration", m.shutdownStats.TotalDuration,
			"components", m.shutdownStats.ComponentCount,
			"successful", m.shutdownStats.SuccessfulCount,
			"errors", m.shutdownStats.ErrorCount,
			"timeouts", m.shutdownStats.TimeoutCount)
	})

	return shutdownError
}

// WaitForShutdown waits for shutdown to complete
func (m *MetricManager) WaitForShutdown() {
	<-m.shutdownDone
}

// IsRunning returns whether the manager is running
func (m *MetricManager) IsRunning() bool {
	return atomic.LoadInt32(&m.running) == 1
}

// GetShutdownStats returns shutdown statistics
func (m *MetricManager) GetShutdownStats() ShutdownStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.shutdownStats
}

// Private methods

func (m *MetricManager) setupSignalHandling() {
	// Register for graceful shutdown signals
	gracefulSignals := m.config.GracefulSignals
	if len(gracefulSignals) == 0 {
		gracefulSignals = []os.Signal{syscall.SIGTERM, syscall.SIGINT}
	}

	signal.Notify(m.signalChannel, gracefulSignals...)

	// Register for force shutdown signals
	forceSignals := m.config.ForceShutdownSignals
	if len(forceSignals) == 0 {
		forceSignals = []os.Signal{syscall.SIGKILL, syscall.SIGQUIT}
	}

	signal.Notify(m.signalChannel, forceSignals...)

	// Start signal handler
	go m.handleSignals()
}

func (m *MetricManager) handleSignals() {
	for {
		select {
		case <-m.shutdown:
			return
		case sig := <-m.signalChannel:
			m.logger.Info("Received signal", "signal", sig)

			// Check for custom handler
			if handler, exists := m.signalHandlers[sig]; exists {
				if err := handler(sig); err != nil {
					m.logger.Error("Custom signal handler failed", "signal", sig, "error", err)
				}
				continue
			}

			// Handle graceful shutdown signals
			if m.isGracefulSignal(sig) {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), m.config.GlobalShutdownTimeout)
					defer cancel()

					if err := m.Shutdown(ctx); err != nil {
						m.logger.Error("Graceful shutdown failed", "error", err)
						os.Exit(1)
					}
					os.Exit(0)
				}()
			} else {
				// Force shutdown
				m.logger.Warn("Force shutdown signal received", "signal", sig)
				os.Exit(1)
			}
		}
	}
}

func (m *MetricManager) isGracefulSignal(sig os.Signal) bool {
	for _, graceful := range m.config.GracefulSignals {
		if sig == graceful {
			return true
		}
	}
	return false
}

func (m *MetricManager) executeShutdownPhases(ctx context.Context) error {
	if !m.config.EnablePhases {
		return m.shutdownAllComponents(ctx)
	}

	// Execute phases in order
	for _, phase := range m.shutdownPhases {
		phaseStats := PhaseStats{
			Name:      phase.Name,
			StartTime: time.Now(),
		}

		m.logger.Info("Starting shutdown phase", "phase", phase.Name)

		// Create phase context with timeout
		phaseCtx, cancel := context.WithTimeout(ctx, phase.Timeout)

		err := m.executeShutdownPhase(phaseCtx, phase)
		cancel()

		phaseStats.Duration = time.Since(phaseStats.StartTime)
		phaseStats.Success = err == nil
		phaseStats.Error = err

		m.shutdownStats.Phases = append(m.shutdownStats.Phases, phaseStats)

		if err != nil {
			m.logger.Error("Shutdown phase failed", "phase", phase.Name, "error", err)
			if phase.Required {
				return fmt.Errorf("required phase %s failed: %w", phase.Name, err)
			}
		}

		// Wait between phases if configured
		if m.config.WaitBetweenPhases > 0 {
			time.Sleep(m.config.WaitBetweenPhases)
		}
	}

	return nil
}

func (m *MetricManager) executeShutdownPhase(ctx context.Context, phase ShutdownPhase) error {
	var wg sync.WaitGroup
	errorChan := make(chan error, len(phase.Components))

	// Shutdown components in this phase concurrently
	for _, componentName := range phase.Components {
		m.mu.RLock()
		component, exists := m.components[componentName]
		m.mu.RUnlock()

		if !exists {
			m.logger.Warn("Component not found for phase", "component", componentName, "phase", phase.Name)
			continue
		}

		wg.Add(1)
		go func(name string, comp ShutdownComponent) {
			defer wg.Done()

			if err := m.shutdownComponent(ctx, name, comp); err != nil {
				errorChan <- fmt.Errorf("component %s: %w", name, err)
			}
		}(componentName, component)
	}

	// Wait for all components in this phase
	wg.Wait()
	close(errorChan)

	// Collect errors
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("phase errors: %v", errors)
	}

	return nil
}

func (m *MetricManager) shutdownAllComponents(ctx context.Context) error {
	m.mu.RLock()
	components := make([]componentWithName, 0, len(m.components))
	for name, component := range m.components {
		components = append(components, componentWithName{
			name:      name,
			component: component,
		})
	}
	m.mu.RUnlock()

	// Sort by priority (higher priority shuts down first)
	m.sortComponentsByPriority(components)

	// Shutdown components
	for _, comp := range components {
		if err := m.shutdownComponent(ctx, comp.name, comp.component); err != nil {
			m.logger.Error("Component shutdown failed", "component", comp.name, "error", err)
			m.shutdownStats.ErrorCount++

			if !m.config.ContinueOnError {
				return fmt.Errorf("component %s shutdown failed: %w", comp.name, err)
			}
		} else {
			m.shutdownStats.SuccessfulCount++
		}
	}

	return nil
}

func (m *MetricManager) shutdownComponent(ctx context.Context, name string, component ShutdownComponent) error {
	start := time.Now()

	m.logger.Info("Shutting down component", "component", name)

	// Create component-specific timeout
	timeout := component.GetShutdownTimeout()
	if timeout == 0 {
		timeout = m.config.ComponentTimeout
	}

	componentCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Flush metrics for this component if it's a metric client
	if client, ok := component.(MetricClient[MetricType]); ok {
		m.flushComponentMetrics(componentCtx, name, client)
	}

	// Shutdown the component
	done := make(chan error, 1)
	go func() {
		done <- component.Shutdown(componentCtx)
	}()

	select {
	case err := <-done:
		duration := time.Since(start)
		if err != nil {
			m.logger.Error("Component shutdown failed", "component", name, "duration", duration, "error", err)
			return err
		}

		m.logger.Info("Component shutdown completed", "component", name, "duration", duration)
		return nil

	case <-componentCtx.Done():
		m.shutdownStats.TimeoutCount++
		return fmt.Errorf("component %s shutdown timeout after %v", name, timeout)
	}
}

func (m *MetricManager) flushComponentMetrics(ctx context.Context, name string, client MetricClient[MetricType]) {
	if !m.config.EnableMetricFlushing {
		return
	}

	flushCtx, cancel := context.WithTimeout(ctx, m.config.FlushTimeout)
	defer cancel()

	start := time.Now()

	// Attempt to flush metrics with retries
	for attempt := 1; attempt <= m.config.MaxFlushAttempts; attempt++ {
		if err := m.attemptMetricFlush(flushCtx, name, client); err != nil {
			m.logger.Warn("Metric flush attempt failed",
				"component", name,
				"attempt", attempt,
				"error", err)

			if attempt < m.config.MaxFlushAttempts {
				time.Sleep(m.config.FlushRetryBackoff)
				continue
			}

			m.logger.Error("All metric flush attempts failed", "component", name)
			return
		}

		m.logger.Info("Metrics flushed successfully",
			"component", name,
			"duration", time.Since(start),
			"attempts", attempt)
		return
	}
}

func (m *MetricManager) attemptMetricFlush(ctx context.Context, name string, client MetricClient[MetricType]) error {
	// This would implement actual metric flushing
	// For now, just simulate the flush
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
		m.shutdownStats.FlushedMetrics++
		return nil
	}
}

func (m *MetricManager) performFinalCleanup(ctx context.Context) {
	cleanupCtx, cancel := context.WithTimeout(ctx, m.config.CleanupTimeout)
	defer cancel()

	// Execute cleanup tasks
	for i, task := range m.cleanupTasks {
		if err := task(cleanupCtx); err != nil {
			m.logger.Error("Cleanup task failed", "task_index", i, "error", err)
		}
	}

	// Execute flush tasks
	for i, task := range m.flushTasks {
		if err := task(cleanupCtx); err != nil {
			m.logger.Error("Flush task failed", "task_index", i, "error", err)
		}
	}

	// Cleanup resources
	if m.config.EnableResourceTracking {
		m.resourceManager.CleanupAll(cleanupCtx)
	}

	// Final metric flush
	if m.config.EnableMetricFlushing {
		m.metricFlusher.FinalFlush(cleanupCtx)
	}

	// Shutdown factory
	if err := m.factory.Shutdown(cleanupCtx); err != nil {
		m.logger.Error("Factory shutdown failed", "error", err)
	}
}

func (m *MetricManager) runHealthChecks() {
	ticker := time.NewTicker(m.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.shutdown:
			return
		case <-ticker.C:
			m.healthChecker.CheckAllComponents()
		}
	}
}

func (m *MetricManager) runResourceTracking() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.shutdown:
			return
		case <-ticker.C:
			m.resourceManager.UpdateStats()
		}
	}
}

func (m *MetricManager) sortComponentsByPriority(components []componentWithName) {
	// Sort by priority (higher first), then by name for deterministic ordering
	for i := 0; i < len(components)-1; i++ {
		for j := i + 1; j < len(components); j++ {
			iPriority := components[i].component.GetShutdownPriority()
			jPriority := components[j].component.GetShutdownPriority()

			if jPriority > iPriority || (jPriority == iPriority && components[j].name < components[i].name) {
				components[i], components[j] = components[j], components[i]
			}
		}
	}
}

type componentWithName struct {
	name      string
	component ShutdownComponent
}

// Utility functions

func applyShutdownDefaults(config *ShutdownConfig) {
	if config.GlobalShutdownTimeout == 0 {
		config.GlobalShutdownTimeout = 30 * time.Second
	}
	if config.ComponentTimeout == 0 {
		config.ComponentTimeout = 10 * time.Second
	}
	if config.FlushTimeout == 0 {
		config.FlushTimeout = 5 * time.Second
	}
	if config.CleanupTimeout == 0 {
		config.CleanupTimeout = 5 * time.Second
	}
	if config.SignalTimeout == 0 {
		config.SignalTimeout = 15 * time.Second
	}
	if config.PhaseTimeout == 0 {
		config.PhaseTimeout = 10 * time.Second
	}
	if config.WaitBetweenPhases == 0 {
		config.WaitBetweenPhases = time.Second
	}
	if config.ResourceCleanupTimeout == 0 {
		config.ResourceCleanupTimeout = 5 * time.Second
	}
	if config.FlushInterval == 0 {
		config.FlushInterval = time.Second
	}
	if config.MaxFlushAttempts == 0 {
		config.MaxFlushAttempts = 3
	}
	if config.FlushRetryBackoff == 0 {
		config.FlushRetryBackoff = 500 * time.Millisecond
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 10 * time.Second
	}
	if config.HealthCheckTimeout == 0 {
		config.HealthCheckTimeout = 5 * time.Second
	}
	if config.UnhealthyThreshold == 0 {
		config.UnhealthyThreshold = 3
	}
	if config.MaxErrors == 0 {
		config.MaxErrors = 5
	}
	if config.ErrorTimeout == 0 {
		config.ErrorTimeout = time.Second
	}
}

// Sub-component implementations (simplified)

func NewResourceManager(logger *slog.Logger) *ResourceManager {
	return &ResourceManager{
		resources:    make(map[string]ManagedResource),
		cleanupFuncs: make(map[string]func() error),
		logger:       logger,
	}
}

func (rm *ResourceManager) CleanupAll(ctx context.Context) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for id, resource := range rm.resources {
		if err := resource.Cleanup(ctx); err != nil {
			rm.logger.Error("Resource cleanup failed", "resource_id", id, "error", err)
		} else {
			rm.stats.CleanedResources++
		}
	}
}

func (rm *ResourceManager) UpdateStats() {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	rm.stats.TotalResources = int64(len(rm.resources))

	var activeCount int64
	for _, resource := range rm.resources {
		if resource.IsActive() {
			activeCount++
		}
	}
	rm.stats.ActiveResources = activeCount
}

func NewMetricFlusher(logger *slog.Logger) *MetricFlusher {
	return &MetricFlusher{
		logger: logger,
	}
}

func (mf *MetricFlusher) FinalFlush(ctx context.Context) {
	mf.mu.Lock()
	defer mf.mu.Unlock()

	start := time.Now()

	for _, client := range mf.clients {
		// Flush remaining metrics
		// This would be implemented based on the client type
	}

	mf.flushStats.TotalDuration += time.Since(start)
	mf.flushStats.TotalFlushes++
}

func NewHealthChecker(logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		components: make(map[string]ShutdownComponent),
		logger:     logger,
	}
}

func (hc *HealthChecker) RegisterComponent(name string, component ShutdownComponent) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.components[name] = component
}

func (hc *HealthChecker) UnregisterComponent(name string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	delete(hc.components, name)
}

func (hc *HealthChecker) CheckAllComponents() {
	hc.mu.RLock()
	components := make(map[string]ShutdownComponent)
	for name, comp := range hc.components {
		components[name] = comp
	}
	hc.mu.RUnlock()

	for name, component := range components {
		healthy := component.IsHealthy()

		hc.mu.Lock()
		hc.stats.ComponentsChecked++
		if healthy {
			hc.stats.HealthyComponents++
		} else {
			hc.stats.UnhealthyComponents++
		}
		hc.stats.LastCheck = time.Now()
		hc.mu.Unlock()

		// Notify alert handlers
		for _, handler := range hc.alertHandlers {
			handler(name, healthy, nil)
		}
	}
}
