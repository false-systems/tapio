package collectors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Registry manages multiple collectors and provides centralized control
type Registry struct {
	collectors map[string]Collector
	mu         sync.RWMutex

	// Event aggregation
	events chan RawEvent
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// State
	started bool

	// OTEL instrumentation
	tracer                   trace.Tracer
	activeCollectorsGauge    metric.Int64ObservableGauge
	eventsProcessedCounter   metric.Int64Counter
	eventQueueDepthGauge     metric.Int64ObservableGauge
	processingLatencyHist    metric.Float64Histogram
	collectorHealthGauge     metric.Int64ObservableGauge
	registrationCounter      metric.Int64Counter
	unregistrationCounter    metric.Int64Counter
	startStopDurationHist    metric.Float64Histogram

	// Metrics state for observables
	eventsProcessedByCollector map[string]int64
	eventProcessingTimes       []float64
	metricsLock                sync.RWMutex
}

// NewRegistry creates a new collector registry
func NewRegistry() *Registry {
	r := &Registry{
		collectors:                  make(map[string]Collector),
		events:                      make(chan RawEvent, 10000), // Large buffer for aggregated events
		eventsProcessedByCollector:  make(map[string]int64),
		eventProcessingTimes:        make([]float64, 0, 1000),
	}

	// Initialize OTEL instrumentation
	r.initializeOTEL()

	return r
}

// Register adds a collector to the registry
func (r *Registry) Register(name string, collector Collector) error {
	ctx := context.Background()
	spanCtx, span := r.tracer.Start(ctx, "Registry.Register",
		trace.WithAttributes(
			attribute.String("collector.name", name),
		))
	defer span.End()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		span.RecordError(fmt.Errorf("cannot register collectors after registry is started"))
		return fmt.Errorf("cannot register collectors after registry is started")
	}

	if _, exists := r.collectors[name]; exists {
		span.RecordError(fmt.Errorf("collector '%s' already registered", name))
		return fmt.Errorf("collector '%s' already registered", name)
	}

	r.collectors[name] = collector
	r.metricsLock.Lock()
	r.eventsProcessedByCollector[name] = 0
	r.metricsLock.Unlock()

	// Record registration metric
	if r.registrationCounter != nil {
		r.registrationCounter.Add(spanCtx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", name),
			))
	}

	span.SetAttributes(attribute.Bool("registration.success", true))
	return nil
}

// Unregister removes a collector from the registry
func (r *Registry) Unregister(name string) error {
	ctx := context.Background()
	spanCtx, span := r.tracer.Start(ctx, "Registry.Unregister",
		trace.WithAttributes(
			attribute.String("collector.name", name),
		))
	defer span.End()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		span.RecordError(fmt.Errorf("cannot unregister collectors after registry is started"))
		return fmt.Errorf("cannot unregister collectors after registry is started")
	}

	if _, exists := r.collectors[name]; !exists {
		span.RecordError(fmt.Errorf("collector '%s' not found", name))
		return fmt.Errorf("collector '%s' not found", name)
	}

	delete(r.collectors, name)
	r.metricsLock.Lock()
	delete(r.eventsProcessedByCollector, name)
	r.metricsLock.Unlock()

	// Record unregistration metric
	if r.unregistrationCounter != nil {
		r.unregistrationCounter.Add(spanCtx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", name),
			))
	}

	span.SetAttributes(attribute.Bool("unregistration.success", true))
	return nil
}

// Start starts all registered collectors
func (r *Registry) Start(ctx context.Context) error {
	startTime := time.Now()
	spanCtx, span := r.tracer.Start(ctx, "Registry.Start",
		trace.WithAttributes(
			attribute.Int("collector.count", len(r.collectors)),
		))
	defer func() {
		duration := time.Since(startTime).Seconds() * 1000 // Convert to ms
		if r.startStopDurationHist != nil {
			r.startStopDurationHist.Record(spanCtx, duration,
				metric.WithAttributes(
					attribute.String("operation", "start"),
				))
		}
		span.End()
	}()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		span.RecordError(fmt.Errorf("registry already started"))
		return fmt.Errorf("registry already started")
	}

	if len(r.collectors) == 0 {
		span.RecordError(fmt.Errorf("no collectors registered"))
		return fmt.Errorf("no collectors registered")
	}

	r.ctx, r.cancel = context.WithCancel(ctx)

	// Start all collectors
	for name, collector := range r.collectors {
		collectorSpan := trace.SpanFromContext(spanCtx)
		collectorSpan.AddEvent("Starting collector",
			trace.WithAttributes(attribute.String("collector.name", name)))

		if err := collector.Start(r.ctx); err != nil {
			collectorSpan.RecordError(err)
			// Stop already started collectors
			r.stopCollectors()
			return fmt.Errorf("failed to start collector '%s': %w", name, err)
		}

		// Start event forwarder for this collector
		r.wg.Add(1)
		go r.forwardEvents(name, collector)
	}

	r.started = true
	span.SetAttributes(attribute.Bool("start.success", true))
	return nil
}

// Stop stops all collectors
func (r *Registry) Stop() error {
	startTime := time.Now()
	ctx := context.Background()
	spanCtx, span := r.tracer.Start(ctx, "Registry.Stop")
	defer func() {
		duration := time.Since(startTime).Seconds() * 1000 // Convert to ms
		if r.startStopDurationHist != nil {
			r.startStopDurationHist.Record(spanCtx, duration,
				metric.WithAttributes(
					attribute.String("operation", "stop"),
				))
		}
		span.End()
	}()

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		span.SetAttributes(attribute.Bool("was_running", false))
		return nil
	}

	// Cancel context to stop forwarders
	r.cancel()

	// Stop all collectors
	r.stopCollectors()

	// Wait for forwarders to finish
	r.wg.Wait()

	// Close aggregated events channel
	close(r.events)

	r.started = false
	span.SetAttributes(
		attribute.Bool("stop.success", true),
		attribute.Bool("was_running", true),
	)
	return nil
}

// Events returns the aggregated event channel from all collectors
func (r *Registry) Events() <-chan RawEvent {
	return r.events
}

// List returns the names of all registered collectors
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.collectors))
	for name := range r.collectors {
		names = append(names, name)
	}
	return names
}

// Get returns a specific collector by name
func (r *Registry) Get(name string) (Collector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collector, exists := r.collectors[name]
	return collector, exists
}

// Health returns the health status of all collectors
func (r *Registry) Health() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	health := make(map[string]bool)
	for name, collector := range r.collectors {
		health[name] = collector.IsHealthy()
	}
	return health
}

// IsHealthy returns true if all collectors are healthy
func (r *Registry) IsHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, collector := range r.collectors {
		if !collector.IsHealthy() {
			return false
		}
	}
	return true
}

// stopCollectors stops all collectors (must be called with lock held)
func (r *Registry) stopCollectors() {
	var wg sync.WaitGroup

	for name, collector := range r.collectors {
		wg.Add(1)
		go func(n string, c Collector) {
			defer wg.Done()
			if err := c.Stop(); err != nil {
				// Log error but continue
				// In production, this would use proper logging
			}
		}(name, collector)
	}

	wg.Wait()
}

// forwardEvents forwards events from a collector to the aggregated channel
func (r *Registry) forwardEvents(name string, collector Collector) {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			return
		case event, ok := <-collector.Events():
			if !ok {
				return
			}

			processStart := time.Now()

			// Add collector name to metadata
			if event.Metadata == nil {
				event.Metadata = make(map[string]string)
			}
			event.Metadata["collector"] = name

			select {
			case r.events <- event:
				// Record metrics
				processingTime := time.Since(processStart).Seconds() * 1000 // Convert to ms
				r.recordEventProcessed(name, processingTime)
			case <-r.ctx.Done():
				return
			}
		}
	}
}

// initializeOTEL sets up OpenTelemetry instrumentation
func (r *Registry) initializeOTEL() {
	// Initialize tracer
	r.tracer = otel.Tracer("collector-registry")

	// Initialize meter
	meter := otel.Meter("collector-registry")

	// Create metrics with graceful error handling
	var err error

	// Active collectors gauge (observable)
	r.activeCollectorsGauge, err = meter.Int64ObservableGauge(
		"registry.active_collectors",
		metric.WithDescription("Number of active collectors"),
		metric.WithInt64Callback(r.observeActiveCollectors),
	)
	if err != nil {
		// Continue without metric if OTEL unavailable
	}

	// Events processed counter
	r.eventsProcessedCounter, err = meter.Int64Counter(
		"registry.events_processed_total",
		metric.WithDescription("Total number of events processed"),
	)
	if err != nil {
		// Continue without metric
	}

	// Event queue depth gauge (observable)
	r.eventQueueDepthGauge, err = meter.Int64ObservableGauge(
		"registry.event_queue_depth",
		metric.WithDescription("Current depth of event queue"),
		metric.WithInt64Callback(r.observeQueueDepth),
	)
	if err != nil {
		// Continue without metric
	}

	// Processing latency histogram
	r.processingLatencyHist, err = meter.Float64Histogram(
		"registry.processing_latency_ms",
		metric.WithDescription("Event processing latency in milliseconds"),
	)
	if err != nil {
		// Continue without metric
	}

	// Collector health gauge (observable)
	r.collectorHealthGauge, err = meter.Int64ObservableGauge(
		"registry.collector_health",
		metric.WithDescription("Health status of collectors (1=healthy, 0=unhealthy)"),
		metric.WithInt64Callback(r.observeCollectorHealth),
	)
	if err != nil {
		// Continue without metric
	}

	// Registration counter
	r.registrationCounter, err = meter.Int64Counter(
		"registry.registrations_total",
		metric.WithDescription("Total number of collector registrations"),
	)
	if err != nil {
		// Continue without metric
	}

	// Unregistration counter
	r.unregistrationCounter, err = meter.Int64Counter(
		"registry.unregistrations_total",
		metric.WithDescription("Total number of collector unregistrations"),
	)
	if err != nil {
		// Continue without metric
	}

	// Start/stop duration histogram
	r.startStopDurationHist, err = meter.Float64Histogram(
		"registry.operation_duration_ms",
		metric.WithDescription("Duration of start/stop operations in milliseconds"),
	)
	if err != nil {
		// Continue without metric
	}
}

// recordEventProcessed records metrics for a processed event
func (r *Registry) recordEventProcessed(collectorName string, latencyMs float64) {
	ctx := context.Background()

	// Update counters
	if r.eventsProcessedCounter != nil {
		r.eventsProcessedCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector", collectorName),
			))
	}

	// Record latency
	if r.processingLatencyHist != nil {
		r.processingLatencyHist.Record(ctx, latencyMs,
			metric.WithAttributes(
				attribute.String("collector", collectorName),
			))
	}

	// Update per-collector counter
	r.metricsLock.Lock()
	r.eventsProcessedByCollector[collectorName]++
	// Keep a sliding window of recent processing times
	r.eventProcessingTimes = append(r.eventProcessingTimes, latencyMs)
	if len(r.eventProcessingTimes) > 1000 {
		r.eventProcessingTimes = r.eventProcessingTimes[1:]
	}
	r.metricsLock.Unlock()
}

// observeActiveCollectors callback for active collectors gauge
func (r *Registry) observeActiveCollectors(_ context.Context, o metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.started {
		o.Observe(int64(len(r.collectors)))
	} else {
		o.Observe(0)
	}
	return nil
}

// observeQueueDepth callback for queue depth gauge
func (r *Registry) observeQueueDepth(_ context.Context, o metric.Int64Observer) error {
	o.Observe(int64(len(r.events)))
	return nil
}

// observeCollectorHealth callback for collector health gauge
func (r *Registry) observeCollectorHealth(_ context.Context, o metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, collector := range r.collectors {
		healthValue := int64(0)
		if collector.IsHealthy() {
			healthValue = 1
		}
		o.Observe(healthValue,
			metric.WithAttributes(
				attribute.String("collector", name),
			))
	}
	return nil
}
