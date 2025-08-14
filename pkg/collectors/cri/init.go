package cri

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"google.golang.org/grpc/credentials/insecure"
)

// init registers the CRI collector with the global registry
func init() {
	// Register the CRI collector factory with error handling
	if err := registry.Register(CollectorName, NewCRICollector); err != nil {
		// Log error but don't panic - this allows the application to continue
		fmt.Printf("WARNING: failed to register CRI collector: %v\n", err)
		fmt.Printf("CRI collector will not be available\n")
	}
}

// NewCRICollector creates a new CRI collector instance
// This function matches the CollectorFactory signature required by the registry
func NewCRICollector(config map[string]interface{}) (collectors.Collector, error) {
	// Parse configuration
	criConfig, err := parseConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CRI collector config: %w", err)
	}

	// Validate configuration
	if err := criConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid CRI collector config: %w", err)
	}

	// Use configured name or default
	name := criConfig.Name
	if name == "" {
		name = CollectorName
	}

	// Create collector based on eBPF availability
	if criConfig.EnableEBPF {
		// Try to create eBPF-enhanced collector
		collector, err := NewEBPFCollector(name, criConfig)
		if err != nil {
			// Fall back to regular collector if eBPF fails
			// collector.logger.Warn("Failed to create eBPF collector, falling back to regular CRI collector",
			//	zap.Error(err))
			return NewCollector(name, criConfig)
		}
		return collector, nil
	}

	// Create regular CRI collector
	return NewCollector(name, criConfig)
}

// parseConfig converts map[string]interface{} to Config struct
func parseConfig(configMap map[string]interface{}) (Config, error) {
	// Start with default configuration
	config := DefaultConfig()

	if configMap == nil {
		return config, nil
	}

	// Parse configuration fields
	if name, ok := configMap["name"].(string); ok {
		config.Name = name
	}

	// OTEL configuration parsing
	if tracingEnabled, ok := configMap["tracing_enabled"].(bool); ok {
		config.TracingEnabled = tracingEnabled
	}

	if sampleRate, ok := configMap["tracing_sample_rate"].(float64); ok {
		config.TracingSampleRate = sampleRate
	}

	if metricsEnabled, ok := configMap["metrics_enabled"].(bool); ok {
		config.MetricsEnabled = metricsEnabled
	}

	if metricsInterval, ok := configMap["metrics_interval"].(string); ok {
		if interval, err := time.ParseDuration(metricsInterval); err == nil {
			config.MetricsInterval = interval
		}
	}

	if otlpEndpoint, ok := configMap["otlp_endpoint"].(string); ok {
		config.OTLPEndpoint = otlpEndpoint
	}

	if otlpInsecure, ok := configMap["otlp_insecure"].(bool); ok {
		config.OTLPInsecure = otlpInsecure
	}

	if spanBufferSize, ok := configMap["span_buffer_size"].(int); ok {
		config.SpanBufferSize = spanBufferSize
	} else if spanBufferSize, ok := configMap["span_buffer_size"].(float64); ok {
		config.SpanBufferSize = int(spanBufferSize)
	}

	if spanBatchTimeout, ok := configMap["span_batch_timeout"].(string); ok {
		if timeout, err := time.ParseDuration(spanBatchTimeout); err == nil {
			config.SpanBatchTimeout = timeout
		}
	}

	if serviceName, ok := configMap["service_name"].(string); ok {
		config.ServiceName = serviceName
	}

	if serviceVersion, ok := configMap["service_version"].(string); ok {
		config.ServiceVersion = serviceVersion
	}

	if deployEnv, ok := configMap["deployment_environment"].(string); ok {
		config.DeploymentEnvironment = deployEnv
	}

	if socketPath, ok := configMap["socket_path"].(string); ok {
		config.SocketPath = socketPath
	}

	if bufferSize, ok := configMap["event_buffer_size"].(int); ok {
		config.EventBufferSize = bufferSize
	} else if bufferSize, ok := configMap["event_buffer_size"].(float64); ok {
		config.EventBufferSize = int(bufferSize)
	}

	if pollInterval, ok := configMap["poll_interval"].(string); ok {
		if interval, err := time.ParseDuration(pollInterval); err == nil {
			config.PollInterval = interval
		}
	}

	if batchSize, ok := configMap["batch_size"].(int); ok {
		config.BatchSize = batchSize
	} else if batchSize, ok := configMap["batch_size"].(float64); ok {
		config.BatchSize = int(batchSize)
	}

	if flushInterval, ok := configMap["flush_interval"].(string); ok {
		if interval, err := time.ParseDuration(flushInterval); err == nil {
			config.FlushInterval = interval
		}
	}

	if ringBufferSize, ok := configMap["ring_buffer_size"].(int); ok {
		config.RingBufferSize = ringBufferSize
	} else if ringBufferSize, ok := configMap["ring_buffer_size"].(float64); ok {
		config.RingBufferSize = int(ringBufferSize)
	}

	// Feature flags
	if enableMetrics, ok := configMap["enable_metrics"].(bool); ok {
		config.EnableMetrics = enableMetrics
	}

	if enableTracing, ok := configMap["enable_tracing"].(bool); ok {
		config.EnableTracing = enableTracing
	}

	if enableEBPF, ok := configMap["enable_ebpf"].(bool); ok {
		config.EnableEBPF = enableEBPF
	}

	// Filtering options
	if kubernetesOnly, ok := configMap["kubernetes_only"].(bool); ok {
		config.KubernetesOnly = kubernetesOnly
	}

	if excludeSystem, ok := configMap["exclude_system_containers"].(bool); ok {
		config.ExcludeSystemContainers = excludeSystem
	}

	if includeNS, ok := configMap["include_namespaces"].([]interface{}); ok {
		config.IncludeNamespaces = convertToStringSlice(includeNS)
	}

	if excludeNS, ok := configMap["exclude_namespaces"].([]interface{}); ok {
		config.ExcludeNamespaces = convertToStringSlice(excludeNS)
	}

	// Resource limits
	if maxMemory, ok := configMap["max_memory_mb"].(int); ok {
		config.MaxMemoryMB = maxMemory
	} else if maxMemory, ok := configMap["max_memory_mb"].(float64); ok {
		config.MaxMemoryMB = int(maxMemory)
	}

	if maxCPU, ok := configMap["max_cpu_percent"].(int); ok {
		config.MaxCPUPercent = maxCPU
	} else if maxCPU, ok := configMap["max_cpu_percent"].(float64); ok {
		config.MaxCPUPercent = int(maxCPU)
	}

	// Health check settings
	if healthInterval, ok := configMap["health_check_interval"].(string); ok {
		if interval, err := time.ParseDuration(healthInterval); err == nil {
			config.HealthCheckInterval = interval
		}
	}

	if healthTimeout, ok := configMap["health_check_timeout"].(string); ok {
		if timeout, err := time.ParseDuration(healthTimeout); err == nil {
			config.HealthCheckTimeout = timeout
		}
	}

	return config, nil
}

// convertToStringSlice converts []interface{} to []string
func convertToStringSlice(slice []interface{}) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if str, ok := item.(string); ok {
			result = append(result, str)
		}
	}
	return result
}

// InitializeOTEL sets up global OTEL providers for CRI collector
func InitializeOTEL(ctx context.Context, config *Config) (func(), error) {
	if !config.TracingEnabled && !config.MetricsEnabled {
		return func() {}, nil
	}

	// Validate OTEL configuration
	if err := config.ValidateOTELEndpoint(); err != nil {
		return nil, fmt.Errorf("invalid OTEL configuration: %w", err)
	}

	// Create resource with semantic conventions
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceVersion(config.ServiceVersion),
			semconv.DeploymentEnvironment(config.DeploymentEnvironment),
		),
		resource.WithHost(),
		resource.WithContainer(),
		resource.WithOS(),
		resource.WithProcess(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTEL resource: %w", err)
	}

	var shutdownFuncs []func(context.Context) error

	// Setup tracing provider
	if config.TracingEnabled {
		tracerProvider, shutdown, err := setupTracing(ctx, res, config)
		if err != nil {
			return nil, fmt.Errorf("failed to setup tracing: %w", err)
		}
		shutdownFuncs = append(shutdownFuncs, shutdown)
		otel.SetTracerProvider(tracerProvider)
	}

	// Setup metrics provider
	if config.MetricsEnabled {
		meterProvider, shutdown, err := setupMetrics(ctx, res, config)
		if err != nil {
			return nil, fmt.Errorf("failed to setup metrics: %w", err)
		}
		shutdownFuncs = append(shutdownFuncs, shutdown)
		otel.SetMeterProvider(meterProvider)
	}

	// Set global text map propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Return comprehensive shutdown function
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for _, fn := range shutdownFuncs {
			if err := fn(shutdownCtx); err != nil {
				// Log error but continue shutdown process
				fmt.Printf("Error shutting down OTEL component: %v\n", err)
			}
		}
	}, nil
}

// setupTracing configures the trace provider with OTLP exporter
func setupTracing(ctx context.Context, res *resource.Resource, config *Config) (*sdktrace.TracerProvider, func(context.Context) error, error) {
	// Create gRPC connection options
	grpcOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(config.OTLPEndpoint),
	}

	if config.OTLPInsecure {
		grpcOpts = append(grpcOpts, otlptracegrpc.WithInsecure())
	} else {
		grpcOpts = append(grpcOpts, otlptracegrpc.WithTLSCredentials(
			insecure.NewCredentials(),
		))
	}

	// Add headers if configured
	if len(config.OTLPHeaders) > 0 {
		grpcOpts = append(grpcOpts, otlptracegrpc.WithHeaders(config.OTLPHeaders))
	}

	// Create OTLP trace exporter
	exporter, err := otlptrace.New(ctx,
		otlptracegrpc.NewClient(grpcOpts...),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Configure batch processor options
	batchOpts := []sdktrace.BatchSpanProcessorOption{
		sdktrace.WithBatchTimeout(config.SpanBatchTimeout),
		sdktrace.WithMaxExportBatchSize(config.SpanBatchSize),
		sdktrace.WithMaxQueueSize(config.GetEffectiveSpanBufferSize()),
	}

	// Create tracer provider with optimized settings
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, batchOpts...),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(
			config.GetEffectiveTracingSampleRate(),
		)),
		// Add span limits for production safety
		sdktrace.WithSpanLimits(sdktrace.SpanLimits{
			AttributeValueLengthLimit:   1024,
			AttributeCountLimit:         128,
			EventCountLimit:             128,
			LinkCountLimit:              128,
			AttributePerEventCountLimit: 64,
			AttributePerLinkCountLimit:  64,
		}),
	)

	return tp, tp.Shutdown, nil
}

// setupMetrics configures the metrics provider with OTLP exporter
func setupMetrics(ctx context.Context, res *resource.Resource, config *Config) (*metric.MeterProvider, func(context.Context) error, error) {
	// Create gRPC connection options for metrics
	grpcOpts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(config.OTLPEndpoint),
	}

	if config.OTLPInsecure {
		grpcOpts = append(grpcOpts, otlpmetricgrpc.WithInsecure())
	}

	// Add headers if configured
	if len(config.OTLPHeaders) > 0 {
		grpcOpts = append(grpcOpts, otlpmetricgrpc.WithHeaders(config.OTLPHeaders))
	}

	// Create OTLP metric exporter
	exporter, err := otlpmetricgrpc.New(ctx, grpcOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
	}

	// Create periodic reader with configured interval
	reader := metric.NewPeriodicReader(exporter,
		metric.WithInterval(config.MetricsInterval),
	)

	// Create meter provider
	mp := metric.NewMeterProvider(
		metric.WithReader(reader),
		metric.WithResource(res),
	)

	return mp, mp.Shutdown, nil
}

// GetDefaultConfig returns the default configuration for the CRI collector
func GetDefaultConfig() map[string]interface{} {
	config := DefaultConfig()

	return map[string]interface{}{
		"name":                      config.Name,
		"socket_path":               config.SocketPath,
		"event_buffer_size":         config.EventBufferSize,
		"poll_interval":             config.PollInterval.String(),
		"batch_size":                config.BatchSize,
		"flush_interval":            config.FlushInterval.String(),
		"ring_buffer_size":          config.RingBufferSize,
		"enable_metrics":            config.EnableMetrics,
		"enable_tracing":            config.EnableTracing,
		"enable_ebpf":               config.EnableEBPF,
		"kubernetes_only":           config.KubernetesOnly,
		"exclude_system_containers": config.ExcludeSystemContainers,
		"include_namespaces":        config.IncludeNamespaces,
		"exclude_namespaces":        config.ExcludeNamespaces,
		"max_memory_mb":             config.MaxMemoryMB,
		"max_cpu_percent":           config.MaxCPUPercent,
		"health_check_interval":     config.HealthCheckInterval.String(),
		"health_check_timeout":      config.HealthCheckTimeout.String(),
		// OTEL configuration
		"tracing_enabled":        config.TracingEnabled,
		"tracing_sample_rate":    config.TracingSampleRate,
		"metrics_enabled":        config.MetricsEnabled,
		"metrics_interval":       config.MetricsInterval.String(),
		"otlp_endpoint":          config.OTLPEndpoint,
		"otlp_insecure":          config.OTLPInsecure,
		"span_buffer_size":       config.SpanBufferSize,
		"span_batch_timeout":     config.SpanBatchTimeout.String(),
		"service_name":           config.ServiceName,
		"service_version":        config.ServiceVersion,
		"deployment_environment": config.DeploymentEnvironment,
	}
}

// GetProductionConfig returns production-optimized configuration
func GetProductionConfig() map[string]interface{} {
	config := ProductionConfig()

	return map[string]interface{}{
		"name":                      config.Name,
		"socket_path":               config.SocketPath,
		"event_buffer_size":         config.EventBufferSize,
		"poll_interval":             config.PollInterval.String(),
		"batch_size":                config.BatchSize,
		"flush_interval":            config.FlushInterval.String(),
		"ring_buffer_size":          config.RingBufferSize,
		"enable_metrics":            config.EnableMetrics,
		"enable_tracing":            config.EnableTracing,
		"enable_ebpf":               config.EnableEBPF,
		"kubernetes_only":           config.KubernetesOnly,
		"exclude_system_containers": config.ExcludeSystemContainers,
		"include_namespaces":        config.IncludeNamespaces,
		"exclude_namespaces":        config.ExcludeNamespaces,
		"max_memory_mb":             config.MaxMemoryMB,
		"max_cpu_percent":           config.MaxCPUPercent,
		"health_check_interval":     config.HealthCheckInterval.String(),
		"health_check_timeout":      config.HealthCheckTimeout.String(),
	}
}

// GetDevConfig returns development-optimized configuration
func GetDevConfig() map[string]interface{} {
	config := DevConfig()

	return map[string]interface{}{
		"name":                      config.Name,
		"socket_path":               config.SocketPath,
		"event_buffer_size":         config.EventBufferSize,
		"poll_interval":             config.PollInterval.String(),
		"batch_size":                config.BatchSize,
		"flush_interval":            config.FlushInterval.String(),
		"ring_buffer_size":          config.RingBufferSize,
		"enable_metrics":            config.EnableMetrics,
		"enable_tracing":            config.EnableTracing,
		"enable_ebpf":               config.EnableEBPF,
		"kubernetes_only":           config.KubernetesOnly,
		"exclude_system_containers": config.ExcludeSystemContainers,
		"include_namespaces":        config.IncludeNamespaces,
		"exclude_namespaces":        config.ExcludeNamespaces,
		"max_memory_mb":             config.MaxMemoryMB,
		"max_cpu_percent":           config.MaxCPUPercent,
		"health_check_interval":     config.HealthCheckInterval.String(),
		"health_check_timeout":      config.HealthCheckTimeout.String(),
	}
}
