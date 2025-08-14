package factory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// TypedCollectorFactory creates collectors from typed configurations
type TypedCollectorFactory = config.CollectorFactory

// registry holds all registered collector factories
var (
	mu             sync.RWMutex
	typedFactories = make(map[string]TypedCollectorFactory)
	configParser   = config.NewConfigParser()

	// OTEL instrumentation
	tracer                    trace.Tracer
	instantiationCounter      metric.Int64Counter
	registrationEventsCounter metric.Int64Counter
	creationFailuresCounter   metric.Int64Counter
	creationSuccessCounter    metric.Int64Counter
	configParsingDurationHist metric.Float64Histogram
	creationDurationHist      metric.Float64Histogram
	validationDurationHist    metric.Float64Histogram
	lookupDurationHist        metric.Float64Histogram
	registeredFactoriesGauge  metric.Int64ObservableGauge

	// Metrics state
	creationStats      = make(map[string]int64) // Success count by type
	failureStats       = make(map[string]int64) // Failure count by type
	metricsInitialized = false
	metricsLock        sync.RWMutex
)

// RegisterTypedFactory registers a typed collector factory
func RegisterTypedFactory(name string, factory TypedCollectorFactory) error {
	ensureOTELInitialized()
	ctx := context.Background()
	spanCtx, span := tracer.Start(ctx, "factory.RegisterTypedFactory",
		trace.WithAttributes(
			attribute.String("collector.type", name),
			attribute.String("factory.type", "typed"),
		))
	defer span.End()

	if name == "" {
		span.RecordError(fmt.Errorf("collector name cannot be empty"))
		return fmt.Errorf("collector name cannot be empty")
	}
	if factory == nil {
		span.RecordError(fmt.Errorf("factory cannot be nil"))
		return fmt.Errorf("factory cannot be nil")
	}

	mu.Lock()
	defer mu.Unlock()

	// Check if already registered
	if _, exists := typedFactories[name]; exists {
		span.RecordError(fmt.Errorf("collector %s already registered", name))
		return fmt.Errorf("collector %s already registered", name)
	}

	typedFactories[name] = factory

	// Record registration metric
	if registrationEventsCounter != nil {
		registrationEventsCounter.Add(spanCtx, 1,
			metric.WithAttributes(
				attribute.String("collector.type", name),
				attribute.String("factory.type", "typed"),
				attribute.String("operation", "register"),
			))
	}

	span.SetAttributes(attribute.Bool("registration.success", true))
	return nil
}

// CreateCollector creates a collector instance by name with map[string]interface{} configuration
// This method parses the map into typed configurations and uses typed factories
func CreateCollector(name string, config map[string]interface{}) (collectors.Collector, error) {
	ensureOTELInitialized()
	ctx := context.Background()
	spanCtx, span := tracer.Start(ctx, "factory.CreateCollector",
		trace.WithAttributes(
			attribute.String("collector.type", name),
			attribute.String("factory.type", "typed"),
		))
	defer span.End()

	if name == "" {
		span.RecordError(fmt.Errorf("collector name cannot be empty"))
		return nil, fmt.Errorf("collector name cannot be empty")
	}
	if config == nil {
		config = make(map[string]interface{})
	}

	lookupStart := time.Now()
	mu.RLock()
	typedFactory, exists := typedFactories[name]
	mu.RUnlock()

	// Record lookup duration
	if lookupDurationHist != nil {
		lookupDuration := time.Since(lookupStart).Seconds() * 1000
		lookupDurationHist.Record(spanCtx, lookupDuration,
			metric.WithAttributes(
				attribute.String("collector.type", name),
			))
	}

	if !exists {
		unknownErr := fmt.Errorf("unknown collector type: %s", name)
		span.RecordError(unknownErr)
		recordFailure(name, "typed")
		return nil, unknownErr
	}

	creationStart := time.Now()

	// Parse map config to typed config
	parseStart := time.Now()
	typedConfig, parseErr := configParser.ParseFromMap(name, config)
	parseTime := time.Since(parseStart).Seconds() * 1000

	if configParsingDurationHist != nil {
		configParsingDurationHist.Record(spanCtx, parseTime,
			metric.WithAttributes(
				attribute.String("collector.type", name),
			))
	}

	if parseErr != nil {
		span.RecordError(parseErr)
		recordFailure(name, "typed")
		return nil, fmt.Errorf("failed to parse config for collector %s: %w", name, parseErr)
	}

	// Validate the parsed config
	validationStart := time.Now()
	if validationErr := typedFactory.ValidateConfig(typedConfig); validationErr != nil {
		validationTime := time.Since(validationStart).Seconds() * 1000
		if validationDurationHist != nil {
			validationDurationHist.Record(spanCtx, validationTime,
				metric.WithAttributes(
					attribute.String("collector.type", name),
					attribute.Bool("validation.success", false),
				))
		}
		span.RecordError(validationErr)
		recordFailure(name, "typed")
		return nil, fmt.Errorf("config validation failed for collector %s: %w", name, validationErr)
	}
	validationTime := time.Since(validationStart).Seconds() * 1000
	if validationDurationHist != nil {
		validationDurationHist.Record(spanCtx, validationTime,
			metric.WithAttributes(
				attribute.String("collector.type", name),
				attribute.Bool("validation.success", true),
			))
	}

	// Create collector and convert to proper type
	collectorInterface, createErr := typedFactory.CreateCollector(context.Background(), typedConfig)
	if createErr != nil {
		span.RecordError(createErr)
		recordFailure(name, "typed")
		return nil, createErr
	}

	// Type assert to collectors.Collector
	collector, ok := collectorInterface.(collectors.Collector)
	if !ok {
		typeErr := fmt.Errorf("factory returned invalid collector type for %s", name)
		span.RecordError(typeErr)
		recordFailure(name, "typed")
		return nil, typeErr
	}

	// Record creation duration and success
	creationTime := time.Since(creationStart).Seconds() * 1000
	if creationDurationHist != nil {
		creationDurationHist.Record(spanCtx, creationTime,
			metric.WithAttributes(
				attribute.String("collector.type", name),
				attribute.String("factory.type", "typed"),
			))
	}

	recordSuccess(name, "typed")
	span.SetAttributes(
		attribute.Bool("creation.success", true),
		attribute.String("factory.type", "typed"),
	)

	return collector, nil
}

// CreateTypedCollector creates a collector instance using typed configuration
func CreateTypedCollector(ctx context.Context, collectorType string, config config.CollectorConfig) (collectors.Collector, error) {
	ensureOTELInitialized()
	spanCtx, span := tracer.Start(ctx, "factory.CreateTypedCollector",
		trace.WithAttributes(
			attribute.String("collector.type", collectorType),
			attribute.String("factory.type", "typed"),
		))
	defer span.End()

	if collectorType == "" {
		span.RecordError(fmt.Errorf("collector type cannot be empty"))
		return nil, fmt.Errorf("collector type cannot be empty")
	}
	if config == nil {
		span.RecordError(fmt.Errorf("config cannot be nil"))
		return nil, fmt.Errorf("config cannot be nil")
	}

	lookupStart := time.Now()
	mu.RLock()
	factory, exists := typedFactories[collectorType]
	mu.RUnlock()

	// Record lookup duration
	if lookupDurationHist != nil {
		lookupDuration := time.Since(lookupStart).Seconds() * 1000
		lookupDurationHist.Record(spanCtx, lookupDuration,
			metric.WithAttributes(
				attribute.String("collector.type", collectorType),
			))
	}

	if !exists {
		notFoundErr := fmt.Errorf("no typed factory registered for collector type: %s", collectorType)
		span.RecordError(notFoundErr)
		recordFailure(collectorType, "typed")
		return nil, notFoundErr
	}

	creationStart := time.Now()

	// Validate config
	validationStart := time.Now()
	if err := factory.ValidateConfig(config); err != nil {
		validationTime := time.Since(validationStart).Seconds() * 1000
		if validationDurationHist != nil {
			validationDurationHist.Record(spanCtx, validationTime,
				metric.WithAttributes(
					attribute.String("collector.type", collectorType),
					attribute.Bool("validation.success", false),
				))
		}
		span.RecordError(err)
		recordFailure(collectorType, "typed")
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	validationTime := time.Since(validationStart).Seconds() * 1000
	if validationDurationHist != nil {
		validationDurationHist.Record(spanCtx, validationTime,
			metric.WithAttributes(
				attribute.String("collector.type", collectorType),
				attribute.Bool("validation.success", true),
			))
	}

	// Create collector and convert to proper type
	collectorInterface, err := factory.CreateCollector(ctx, config)
	if err != nil {
		span.RecordError(err)
		recordFailure(collectorType, "typed")
		return nil, err
	}

	// Type assert to collectors.Collector
	collector, ok := collectorInterface.(collectors.Collector)
	if !ok {
		typeErr := fmt.Errorf("factory returned invalid collector type")
		span.RecordError(typeErr)
		recordFailure(collectorType, "typed")
		return nil, typeErr
	}

	// Record creation duration and success
	creationTime := time.Since(creationStart).Seconds() * 1000
	if creationDurationHist != nil {
		creationDurationHist.Record(spanCtx, creationTime,
			metric.WithAttributes(
				attribute.String("collector.type", collectorType),
				attribute.String("factory.type", "typed"),
			))
	}

	recordSuccess(collectorType, "typed")
	span.SetAttributes(
		attribute.Bool("creation.success", true),
		attribute.String("factory.type", "typed"),
	)

	return collector, nil
}

// ListCollectors returns a sorted list of registered collector names
func ListCollectors() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(typedFactories))
	for name := range typedFactories {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// ListTypedCollectors returns a sorted list of collectors that support typed configuration
func ListTypedCollectors() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(typedFactories))
	for name := range typedFactories {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// IsRegistered checks if a collector type is registered
func IsRegistered(name string) bool {
	mu.RLock()
	defer mu.RUnlock()

	_, exists := typedFactories[name]
	return exists
}

// IsTypedRegistered checks if a collector type is registered with typed factory
func IsTypedRegistered(name string) bool {
	mu.RLock()
	defer mu.RUnlock()

	_, exists := typedFactories[name]
	return exists
}

// GetTypedFactory returns the typed factory for a collector type
func GetTypedFactory(name string) (TypedCollectorFactory, error) {
	mu.RLock()
	defer mu.RUnlock()

	factory, exists := typedFactories[name]
	if !exists {
		return nil, fmt.Errorf("no typed factory registered for collector type: %s", name)
	}

	return factory, nil
}

// ensureOTELInitialized initializes OTEL instrumentation if not already done
func ensureOTELInitialized() {
	metricsLock.Lock()
	defer metricsLock.Unlock()

	if metricsInitialized {
		return
	}

	// Initialize tracer
	tracer = otel.Tracer("collector-factory")

	// Initialize meter
	meter := otel.Meter("collector-factory")

	var err error

	// Collector instantiation counter
	instantiationCounter, err = meter.Int64Counter(
		"factory.instantiations_total",
		metric.WithDescription("Total number of collector instantiations by type"),
	)
	if err != nil {
		// Continue without metric if OTEL unavailable
	}

	// Registration events counter
	registrationEventsCounter, err = meter.Int64Counter(
		"factory.registration_events_total",
		metric.WithDescription("Total number of factory registration events"),
	)
	if err != nil {
		// Continue without metric
	}

	// Creation failures counter
	creationFailuresCounter, err = meter.Int64Counter(
		"factory.creation_failures_total",
		metric.WithDescription("Total number of collector creation failures"),
	)
	if err != nil {
		// Continue without metric
	}

	// Creation success counter
	creationSuccessCounter, err = meter.Int64Counter(
		"factory.creation_success_total",
		metric.WithDescription("Total number of successful collector creations"),
	)
	if err != nil {
		// Continue without metric
	}

	// Configuration parsing duration histogram
	configParsingDurationHist, err = meter.Float64Histogram(
		"factory.config_parsing_duration_ms",
		metric.WithDescription("Configuration parsing time in milliseconds"),
	)
	if err != nil {
		// Continue without metric
	}

	// Creation duration histogram
	creationDurationHist, err = meter.Float64Histogram(
		"factory.creation_duration_ms",
		metric.WithDescription("Collector creation duration in milliseconds"),
	)
	if err != nil {
		// Continue without metric
	}

	// Validation duration histogram
	validationDurationHist, err = meter.Float64Histogram(
		"factory.validation_duration_ms",
		metric.WithDescription("Configuration validation time in milliseconds"),
	)
	if err != nil {
		// Continue without metric
	}

	// Factory lookup duration histogram
	lookupDurationHist, err = meter.Float64Histogram(
		"factory.lookup_duration_ms",
		metric.WithDescription("Factory lookup time in milliseconds"),
	)
	if err != nil {
		// Continue without metric
	}

	// Registered factories gauge (observable)
	registeredFactoriesGauge, err = meter.Int64ObservableGauge(
		"factory.registered_factories",
		metric.WithDescription("Number of registered factories by type"),
		metric.WithInt64Callback(observeRegisteredFactories),
	)
	if err != nil {
		// Continue without metric
	}

	metricsInitialized = true
}

// recordSuccess records successful collector creation metrics
func recordSuccess(collectorType, factoryType string) {
	ctx := context.Background()

	if instantiationCounter != nil {
		instantiationCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.type", collectorType),
				attribute.String("factory.type", factoryType),
			))
	}

	if creationSuccessCounter != nil {
		creationSuccessCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.type", collectorType),
				attribute.String("factory.type", factoryType),
			))
	}

	// Update internal stats
	metricsLock.Lock()
	creationStats[collectorType]++
	metricsLock.Unlock()
}

// recordFailure records failed collector creation metrics
func recordFailure(collectorType, factoryType string) {
	ctx := context.Background()

	if creationFailuresCounter != nil {
		creationFailuresCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.type", collectorType),
				attribute.String("factory.type", factoryType),
			))
	}

	// Update internal stats
	metricsLock.Lock()
	failureStats[collectorType]++
	metricsLock.Unlock()
}

// observeRegisteredFactories callback for registered factories gauge
func observeRegisteredFactories(_ context.Context, o metric.Int64Observer) error {
	mu.RLock()
	defer mu.RUnlock()

	// Observe typed factory count
	o.Observe(int64(len(typedFactories)),
		metric.WithAttributes(
			attribute.String("factory.type", "typed"),
		))

	return nil
}
