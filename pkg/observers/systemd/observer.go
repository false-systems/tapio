package systemd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/observers/base"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Observer monitors systemd service state changes and failures
type Observer struct {
	*base.BaseObserver
	*base.EventChannelManager
	*base.LifecycleManager

	// Core configuration
	config *Config
	logger *zap.Logger

	// Service state tracking
	mu       sync.RWMutex
	services map[string]*ServiceState

	// Rate limiting
	rateLimiter *rate.Limiter

	// eBPF components (platform-specific)
	ebpfState interface{}

	// Journal reader (platform-specific)
	journalReader interface{}

	// Systemd-specific OTEL components
	tracer            trace.Tracer
	eventsProcessed   metric.Int64Counter
	errorsTotal       metric.Int64Counter
	processingTime    metric.Float64Histogram
	servicesMonitored metric.Int64Gauge
	serviceStarts     metric.Int64Counter
	serviceStops      metric.Int64Counter
	serviceFailures   metric.Int64Counter
}

// NewObserver creates a new systemd observer
func NewObserver(name string, config *Config) (*Observer, error) {
	// Use provided config or defaults
	if config == nil {
		config = DefaultConfig()
	}
	config.SetDefaults()

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Set up logger if not provided
	if config.Logger == nil {
		var err error
		config.Logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}
	logger := config.Logger.Named(name)

	// Initialize base components
	baseObserver := base.NewBaseObserver(name, config.HealthCheckInterval)
	eventManager := base.NewEventChannelManager(config.BufferSize, name, logger)
	lifecycleManager := base.NewLifecycleManager(context.Background(), logger)

	// Initialize systemd-specific OTEL components
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	servicesMonitored, err := meter.Int64Gauge(
		fmt.Sprintf("%s_services_monitored", name),
		metric.WithDescription("Number of services being monitored"),
	)
	if err != nil {
		logger.Warn("Failed to create services gauge", zap.Error(err))
	}

	serviceStarts, err := meter.Int64Counter(
		fmt.Sprintf("%s_service_starts_total", name),
		metric.WithDescription("Total service start events"),
	)
	if err != nil {
		logger.Warn("Failed to create service starts counter", zap.Error(err))
	}

	serviceStops, err := meter.Int64Counter(
		fmt.Sprintf("%s_service_stops_total", name),
		metric.WithDescription("Total service stop events"),
	)
	if err != nil {
		logger.Warn("Failed to create service stops counter", zap.Error(err))
	}

	serviceFailures, err := meter.Int64Counter(
		fmt.Sprintf("%s_service_failures_total", name),
		metric.WithDescription("Total service failure events"),
	)
	if err != nil {
		logger.Warn("Failed to create service failures counter", zap.Error(err))
	}

	o := &Observer{
		BaseObserver:        baseObserver,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycleManager,
		config:              config,
		logger:              logger,
		services:            make(map[string]*ServiceState),
		rateLimiter:         rate.NewLimiter(rate.Limit(config.RateLimitPerSecond), config.RateLimitPerSecond),
		tracer:              tracer,
		eventsProcessed:     eventsProcessed,
		errorsTotal:         errorsTotal,
		processingTime:      processingTime,
		servicesMonitored:   servicesMonitored,
		serviceStarts:       serviceStarts,
		serviceStops:        serviceStops,
		serviceFailures:     serviceFailures,
	}

	logger.Info("Systemd observer created",
		zap.String("name", name),
		zap.Int("buffer_size", config.BufferSize),
		zap.Bool("enable_ebpf", config.EnableEBPF),
		zap.Bool("enable_journal", config.EnableJournal),
	)

	return o, nil
}

// Start starts the systemd monitoring
func (o *Observer) Start(ctx context.Context) error {
	ctx, span := o.tracer.Start(ctx, "systemd.observer.start")
	defer span.End()

	o.logger.Info("Starting systemd observer")

	// Start platform-specific monitoring
	if o.config.EnableEBPF {
		if err := o.startEBPFMonitoring(ctx); err != nil {
			span.SetAttributes(attribute.String("error", err.Error()))
			return fmt.Errorf("failed to start eBPF monitoring: %w", err)
		}
	}

	if o.config.EnableJournal {
		if err := o.startJournalMonitoring(ctx); err != nil {
			span.SetAttributes(attribute.String("error", err.Error()))
			// Journal failures are not fatal, just log
			o.logger.Warn("Failed to start journal monitoring", zap.Error(err))
		}
	}

	// Start health check routine
	o.LifecycleManager.Start("health-check", func() {
		o.runHealthCheck(o.LifecycleManager.Context())
	})

	o.SetHealthy(true)
	o.logger.Info("Systemd observer started successfully")
	span.SetAttributes(attribute.Bool("success", true))

	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping systemd observer")

	// Stop lifecycle manager with timeout
	o.LifecycleManager.Stop(30 * time.Second)

	// Stop platform-specific monitoring
	o.stopEBPFMonitoring()
	o.stopJournalMonitoring()

	// Close event channel
	o.EventChannelManager.Close()

	o.SetHealthy(false)
	o.logger.Info("Systemd observer stopped successfully")

	return nil
}

// runHealthCheck performs periodic health checks
func (o *Observer) runHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(o.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.performHealthCheck(ctx)
		}
	}
}

// performHealthCheck checks observer health
func (o *Observer) performHealthCheck(ctx context.Context) {
	o.mu.RLock()
	serviceCount := len(o.services)
	o.mu.RUnlock()

	// Update metrics
	if o.servicesMonitored != nil {
		o.servicesMonitored.Record(ctx, int64(serviceCount))
	}

	// Check for stale data
	if o.GetStatistics().LastEventTime.Before(time.Now().Add(-5 * time.Minute)) {
		o.logger.Warn("No events received in last 5 minutes")
	}
}

// processSystemdEvent processes a systemd event from eBPF
func (o *Observer) processSystemdEvent(ctx context.Context, event *SystemdEvent) {
	if !o.rateLimiter.Allow() {
		o.IncrementDropped()
		return
	}

	start := time.Now()
	defer func() {
		if o.processingTime != nil {
			duration := time.Since(start).Milliseconds()
			o.processingTime.Record(ctx, float64(duration))
		}
	}()

	// Convert to string
	serviceName := string(event.ServiceName[:])
	serviceName = cleanString(serviceName)

	// Update service state
	o.updateServiceState(serviceName, event)

	// Create domain event
	domainEvent := o.createDomainEvent(event, serviceName)

	// Send event
	if err := o.SendEvent(domainEvent); err != nil {
		o.logger.Error("Failed to send event",
			zap.String("service", serviceName),
			zap.Error(err))
		if o.errorsTotal != nil {
			o.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "send_failed"),
			))
		}
		return
	}

	o.IncrementProcessed()
	if o.eventsProcessed != nil {
		o.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", getEventTypeName(event.EventType)),
			attribute.String("service", serviceName),
		))
	}

	// Update specific counters
	o.updateEventCounters(ctx, event.EventType)
}

// updateServiceState updates the internal service state tracking
func (o *Observer) updateServiceState(serviceName string, event *SystemdEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()

	state, exists := o.services[serviceName]
	if !exists {
		state = &ServiceState{
			Name:        serviceName,
			LastChanged: time.Now(),
		}
		o.services[serviceName] = state
	}

	// Update state based on event type
	switch event.EventType {
	case EventTypeServiceStart:
		state.State = StateActive
		state.SubState = "running"
		state.PID = event.PID
		state.ExitCode = 0
	case EventTypeServiceStop:
		state.State = StateInactive
		state.SubState = "exited"
		state.PID = 0
		state.ExitCode = int32(event.ExitCode)
	case EventTypeServiceFailed:
		state.State = StateFailed
		state.SubState = "failed"
		state.ExitCode = int32(event.ExitCode)
	case EventTypeServiceRestart:
		state.RestartCount++
		state.State = StateActivating
		state.SubState = "restarting"
	}

	state.LastChanged = time.Now()
}

// updateEventCounters updates specific event counters
func (o *Observer) updateEventCounters(ctx context.Context, eventType uint8) {
	switch eventType {
	case EventTypeServiceStart:
		if o.serviceStarts != nil {
			o.serviceStarts.Add(ctx, 1)
		}
	case EventTypeServiceStop:
		if o.serviceStops != nil {
			o.serviceStops.Add(ctx, 1)
		}
	case EventTypeServiceFailed:
		if o.serviceFailures != nil {
			o.serviceFailures.Add(ctx, 1)
		}
	}
}

// createDomainEvent creates a domain event from systemd event
func (o *Observer) createDomainEvent(event *SystemdEvent, serviceName string) *domain.CollectorEvent {
	eventType := domain.EventTypeSystemdService
	severity := domain.SeverityInfo

	// Determine severity based on event type
	switch event.EventType {
	case EventTypeServiceFailed:
		severity = domain.SeverityError
	case EventTypeServiceRestart:
		severity = domain.SeverityWarning
	}

	return &domain.CollectorEvent{
		Type:      eventType,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Source:    o.Name(),
		EventData: domain.SystemdServiceEvent{
			ServiceName: serviceName,
			EventType:   getEventTypeName(event.EventType),
			PID:         event.PID,
			UID:         event.UID,
			GID:         event.GID,
			ExitCode:    int32(event.ExitCode),
			Signal:      int32(event.Signal),
			CgroupID:    event.CgroupID,
			CgroupPath:  cleanString(string(event.CgroupPath[:])),
			Severity:    severity,
			Comm:        cleanString(string(event.Comm[:])),
		},
	}
}

// GetServiceStates returns current service states
func (o *Observer) GetServiceStates() map[string]*ServiceState {
	o.mu.RLock()
	defer o.mu.RUnlock()

	states := make(map[string]*ServiceState, len(o.services))
	for k, v := range o.services {
		// Create a copy to avoid race conditions
		stateCopy := *v
		states[k] = &stateCopy
	}
	return states
}

// GetServiceState returns the state of a specific service
func (o *Observer) GetServiceState(serviceName string) (*ServiceState, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	state, exists := o.services[serviceName]
	if !exists {
		return nil, false
	}

	// Return a copy
	stateCopy := *state
	return &stateCopy, true
}

// Statistics returns observer statistics
func (o *Observer) Statistics() *ObserverStats {
	baseStats := o.BaseObserver.GetStatistics()

	o.mu.RLock()
	serviceCount := len(o.services)
	o.mu.RUnlock()

	return &ObserverStats{
		EventsGenerated:   baseStats.EventsProcessed,
		EventsDropped:     baseStats.EventsDropped,
		LastEventTime:     baseStats.LastEventTime,
		ServicesMonitored: serviceCount,
	}
}

// Helper functions

func cleanString(s string) string {
	// Remove null bytes and trim
	for i, b := range []byte(s) {
		if b == 0 {
			return s[:i]
		}
	}
	return s
}

func getEventTypeName(eventType uint8) string {
	switch eventType {
	case EventTypeServiceStart:
		return "service_start"
	case EventTypeServiceStop:
		return "service_stop"
	case EventTypeServiceRestart:
		return "service_restart"
	case EventTypeServiceReload:
		return "service_reload"
	case EventTypeServiceFailed:
		return "service_failed"
	case EventTypeCgroupCreated:
		return "cgroup_created"
	case EventTypeCgroupDestroyed:
		return "cgroup_destroyed"
	default:
		return "unknown"
	}
}
