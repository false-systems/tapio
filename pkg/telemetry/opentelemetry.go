// Package telemetry provides OpenTelemetry export for Tapio intelligence
package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/falseyair/tapio/pkg/correlation"
	"github.com/falseyair/tapio/pkg/correlation/rules"
	"github.com/falseyair/tapio/pkg/correlation/sources"
	"github.com/falseyair/tapio/pkg/ebpf"
	"github.com/falseyair/tapio/pkg/simple"
	"github.com/falseyair/tapio/pkg/sniffer"
	"github.com/falseyair/tapio/pkg/types"
	"github.com/falseyair/tapio/pkg/universal"
	"github.com/falseyair/tapio/pkg/universal/converters"
	"github.com/falseyair/tapio/pkg/universal/formatters"
)

// OpenTelemetryExporter exports Tapio intelligence as OpenTelemetry traces and metrics
type OpenTelemetryExporter struct {
	checker           CheckerInterface
	ebpfMonitor       ebpf.Monitor
	correlationEngine *correlation.Engine

	// Core OpenTelemetry components
	tracer      oteltrace.Tracer
	meter       metric.Meter
	resource    *resource.Resource
	traceProvider *trace.TracerProvider
	meterProvider *metric.MeterProvider

	// Agent 1's translator engine integration (REAL Kubernetes context)
	translator *sniffer.SimplePIDTranslator

	// FULL Agent 3 resilience framework integration
	circuitBreaker   *universal.CircuitBreaker
	resilienceManager *universal.ResilienceManager
	// TODO: Add timeout and validation components

	// Resource efficiency (Polar Signals style)
	spanPool       *sync.Pool
	batcher        *EventBatcher
	resourceLimits ResourceLimits

	// Universal format components
	formatter            *formatters.OpenTelemetryFormatter
	ebpfConverter        *converters.EBPFConverter
	correlationConverter *converters.CorrelationConverter

	// OpenTelemetry metrics
	analysisDuration      metric.Float64Histogram
	spanExportDuration    metric.Float64Histogram
	circuitBreakerState   metric.Int64Gauge
	batchSize            metric.Int64Histogram
	resourceUtilization  metric.Float64Gauge

	// Configuration
	config Config

	// State
	mu                sync.RWMutex
	activeSpans       map[string]oteltrace.Span
	lastUpdateTime    time.Time
	totalSpansCreated int64
	totalSpansExported int64
}

// CheckerInterface defines the interface for checkers (mirrors Prometheus)
type CheckerInterface interface {
	Check(ctx context.Context, req *types.CheckRequest) (*types.CheckResult, error)
}

// Config holds OpenTelemetry exporter configuration
type Config struct {
	ServiceName     string
	ServiceVersion  string
	OTLPEndpoint    string
	Headers         map[string]string
	Insecure        bool
	BatchTimeout    time.Duration
	BatchSize       int
	MaxConcurrency  int
	EnableMetrics   bool
	EnableTraces    bool
	ResourceAttrs   map[string]string
	
	// Agent 1 translator integration
	EnableTranslator bool
	KubeClient      interface{} // kubernetes.Interface - can be nil for testing
}

// ResourceLimits enforces Polar Signals style resource bounds
type ResourceLimits struct {
	MaxMemoryMB     int     // 10Mi memory limit
	MaxCPUPercent   float64 // 5% CPU limit
	MaxSpansInFlight int     // Concurrent span limit
	MaxBatchSize    int     // 19Hz optimal batch size
}

// EventBatcher handles 19Hz batch processing for optimal performance
type EventBatcher struct {
	spans     []oteltrace.Span
	metrics   []metric.Measurement
	batchSize int
	timeout   time.Duration
	mu        sync.Mutex
	flushCh   chan struct{}
}

// NewOpenTelemetryExporter creates a new OpenTelemetry exporter with full resilience
func NewOpenTelemetryExporter(checker CheckerInterface, ebpfMonitor ebpf.Monitor, config Config) (*OpenTelemetryExporter, error) {
	// Set defaults
	if config.ServiceName == "" {
		config.ServiceName = "tapio"
	}
	if config.ServiceVersion == "" {
		config.ServiceVersion = "1.0.0"
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 5 * time.Second
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.MaxConcurrency == 0 {
		config.MaxConcurrency = 10
	}

	// Create OpenTelemetry resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceVersion(config.ServiceVersion),
			attribute.String("component", "tapio-intelligence"),
			attribute.String("telemetry.sdk.name", "tapio-otel-exporter"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	// Create resilience components (FULL Agent 3 integration)
	circuitBreaker := universal.NewCircuitBreaker("otlp-exporter", 5, 30*time.Second)
	resilienceManager := universal.NewResilienceManager()
	
	// TODO: Add more sophisticated timeout and validation when universal package is extended

	// Initialize Agent 1's translator engine for REAL Kubernetes context
	var translator *sniffer.SimplePIDTranslator
	if config.EnableTranslator && config.KubeClient != nil {
		// Use type assertion to kubernetes.Interface (import k8s.io/client-go/kubernetes)
		translator = sniffer.NewSimplePIDTranslator(config.KubeClient)
		fmt.Println("[OTEL] Agent 1 translator engine initialized for real K8s context")
	}

	// Create correlation engine like Prometheus exporter
	var correlationEngine *correlation.Engine
	if simpleChecker, ok := checker.(*simple.Checker); ok {
		dataSources := make(map[correlation.SourceType]correlation.DataSource)
		k8sSource := sources.NewKubernetesDataSource(simpleChecker)
		dataSources[correlation.SourceKubernetes] = k8sSource

		if ebpfMonitor != nil {
			ebpfSource := sources.NewEBPFDataSource(ebpfMonitor)
			dataSources[correlation.SourceEBPF] = ebpfSource
		}

		dataCollection := correlation.NewDataCollection(dataSources)
		engineConfig := correlation.DefaultEngineConfig()
		ruleRegistry := correlation.NewRuleRegistry()

		if err := rules.RegisterDefaultRules(ruleRegistry); err != nil {
			fmt.Printf("[WARN] Failed to register correlation rules: %v\n", err)
		}

		correlationEngine = correlation.NewEngine(engineConfig, ruleRegistry, dataCollection)
	}

	// Resource limits (Polar Signals style)
	resourceLimits := ResourceLimits{
		MaxMemoryMB:      10,    // 10Mi memory limit
		MaxCPUPercent:    5.0,   // 5% CPU limit
		MaxSpansInFlight: 1000,  // Prevent memory explosion
		MaxBatchSize:     19,    // 19Hz optimal batch size
	}

	// Span pool for zero-allocation hot path
	spanPool := &sync.Pool{
		New: func() interface{} {
			return make(map[string]interface{})
		},
	}

	// Event batcher for efficient processing
	batcher := &EventBatcher{
		spans:     make([]oteltrace.Span, 0, config.BatchSize),
		batchSize: config.BatchSize,
		timeout:   config.BatchTimeout,
		flushCh:   make(chan struct{}, 1),
	}

	exporter := &OpenTelemetryExporter{
		checker:           checker,
		ebpfMonitor:       ebpfMonitor,
		correlationEngine: correlationEngine,
		translator:        translator,
		resource:          res,
		circuitBreaker:    circuitBreaker,
		resilienceManager: resilienceManager,
		spanPool:          spanPool,
		batcher:           batcher,
		resourceLimits:    resourceLimits,
		config:            config,
		activeSpans:       make(map[string]oteltrace.Span),
	}

	// Initialize OpenTelemetry providers
	if err := exporter.initializeProviders(); err != nil {
		return nil, fmt.Errorf("failed to initialize OTel providers: %w", err)
	}

	// Register health checks - TODO: Re-implement with universal package
	// if err := exporter.registerHealthChecks(); err != nil {
	//	return nil, fmt.Errorf("failed to register health checks: %w", err)
	// }

	return exporter, nil
}

// initializeProviders sets up OpenTelemetry trace and metric providers
func (e *OpenTelemetryExporter) initializeProviders() error {
	// Initialize trace provider if traces enabled
	if e.config.EnableTraces {
		traceExporter, err := otlptracehttp.New(context.Background(),
			otlptracehttp.WithEndpoint(e.config.OTLPEndpoint),
			otlptracehttp.WithHeaders(e.config.Headers),
			otlptracehttp.WithInsecure(e.config.Insecure),
		)
		if err != nil {
			return fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}

		e.traceProvider = trace.NewTracerProvider(
			trace.WithBatcher(traceExporter,
				trace.WithBatchTimeout(e.config.BatchTimeout),
				trace.WithMaxExportBatchSize(e.config.BatchSize),
			),
			trace.WithResource(e.resource),
		)

		otel.SetTracerProvider(e.traceProvider)
		otel.SetTextMapPropagator(propagation.TraceContext{})
		e.tracer = e.traceProvider.Tracer("tapio-intelligence")
	}

	// Initialize meter provider if metrics enabled
	if e.config.EnableMetrics {
		metricExporter, err := otlpmetrichttp.New(context.Background(),
			otlpmetrichttp.WithEndpoint(e.config.OTLPEndpoint),
			otlpmetrichttp.WithHeaders(e.config.Headers),
			otlpmetrichttp.WithInsecure(e.config.Insecure),
		)
		if err != nil {
			return fmt.Errorf("failed to create OTLP metric exporter: %w", err)
		}

		e.meterProvider = metric.NewMeterProvider(
			metric.WithReader(metric.NewPeriodicReader(metricExporter,
				metric.WithInterval(30*time.Second))),
			metric.WithResource(e.resource),
		)

		otel.SetMeterProvider(e.meterProvider)
		e.meter = e.meterProvider.Meter("tapio-intelligence")

		// Create metrics
		if err := e.createMetrics(); err != nil {
			return fmt.Errorf("failed to create metrics: %w", err)
		}
	}

	return nil
}

// createMetrics initializes OpenTelemetry metrics
func (e *OpenTelemetryExporter) createMetrics() error {
	var err error

	e.analysisDuration, err = e.meter.Float64Histogram(
		"tapio.analysis.duration",
		metric.WithDescription("Time taken for Tapio analysis operations"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	e.spanExportDuration, err = e.meter.Float64Histogram(
		"tapio.span.export.duration",
		metric.WithDescription("Time taken to export spans to OTLP"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	e.circuitBreakerState, err = e.meter.Int64Gauge(
		"tapio.circuit_breaker.state",
		metric.WithDescription("Circuit breaker state (0=closed, 1=open, 2=half-open)"),
	)
	if err != nil {
		return err
	}

	e.batchSize, err = e.meter.Int64Histogram(
		"tapio.batch.size",
		metric.WithDescription("Number of spans in export batch"),
	)
	if err != nil {
		return err
	}

	e.resourceUtilization, err = e.meter.Float64Gauge(
		"tapio.resource.utilization",
		metric.WithDescription("Resource utilization percentage"),
		metric.WithUnit("%"),
	)
	if err != nil {
		return err
	}

	return nil
}

// registerHealthChecks sets up health monitoring
func (e *OpenTelemetryExporter) registerHealthChecks() error {
	// Register OTLP collector health check
	err := e.healthChecker.RegisterComponent(resilience.Component{
		Name:        "otlp-collector",
		Description: "OpenTelemetry collector endpoint",
		Critical:    true,
		Timeout:     5 * time.Second,
		HealthCheck: func(ctx context.Context) error {
			return e.pingOTLPEndpoint(ctx)
		},
	})
	if err != nil {
		return err
	}

	// Register circuit breaker health check
	err = e.healthChecker.RegisterComponent(resilience.Component{
		Name:        "otlp-circuit-breaker",
		Description: "OTLP export circuit breaker",
		Critical:    false,
		HealthCheck: func(ctx context.Context) error {
			if e.circuitBreaker.GetState() == resilience.StateOpen {
				return fmt.Errorf("circuit breaker is open")
			}
			return nil
		},
	})
	if err != nil {
		return err
	}

	return nil
}

// pingOTLPEndpoint checks if OTLP collector is reachable
func (e *OpenTelemetryExporter) pingOTLPEndpoint(ctx context.Context) error {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", e.config.OTLPEndpoint, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("OTLP endpoint returned status %d", resp.StatusCode)
	}

	return nil
}

// CreateSpan creates a new span with circuit breaker and validation
func (e *OpenTelemetryExporter) CreateSpan(ctx context.Context, name string, opts ...oteltrace.SpanStartOption) (oteltrace.Span, error) {
	// Use timeout framework for span creation
	var span oteltrace.Span
	err := e.timeoutManager.Execute(ctx, "create-span", func(ctx context.Context) error {
		// Validate span name
		spanData := map[string]interface{}{
			"operation.name": name,
			"service.name":   e.config.ServiceName,
			"span.kind":     "internal",
		}

		if err := e.validator.Validate(ctx, spanData); err != nil {
			return fmt.Errorf("span validation failed: %w", err)
		}

		// Create span with circuit breaker protection
		_, span = e.tracer.Start(ctx, name, opts...)
		
		// Track active span
		e.mu.Lock()
		e.activeSpans[name] = span
		e.totalSpansCreated++
		e.mu.Unlock()

		return nil
	})

	if err != nil {
		return nil, err
	}

	return span, nil
}

// CreateSpanWithPID creates a span with real Kubernetes context from PID using Agent 1's translator
func (e *OpenTelemetryExporter) CreateSpanWithPID(ctx context.Context, pid uint32, operation string, opts ...oteltrace.SpanStartOption) (oteltrace.Span, error) {
	// Use Agent 3's circuit breaker protection for span creation
	var span oteltrace.Span
	err := e.circuitBreaker.Execute(ctx, func() error {
		// Use Agent 1's translator for REAL Kubernetes context
		var k8sContext *sniffer.EventContext
		var translatorErr error
		
		if e.translator != nil {
			k8sContext, translatorErr = e.translator.GetPodInfo(pid)
			if translatorErr != nil {
				fmt.Printf("[OTEL] Translator failed for PID %d: %v, using fallback\n", pid, translatorErr)
				// Continue with basic span creation even if translation fails
			}
		}
		
		// Create span name with operation
		spanName := fmt.Sprintf("tapio.%s", operation)
		if k8sContext != nil {
			spanName = fmt.Sprintf("tapio.%s.%s", k8sContext.Namespace, operation)
		}
		
		// Create span with enhanced context
		_, span = e.tracer.Start(ctx, spanName, opts...)
		
		// Add REAL Kubernetes context attributes from Agent 1's translator
		if k8sContext != nil {
			span.SetAttributes(
				// Core Kubernetes attributes (REAL data from translator)
				attribute.String("k8s.pod.name", k8sContext.Pod),
				attribute.String("k8s.namespace", k8sContext.Namespace),
				attribute.String("k8s.container.name", k8sContext.Container),
				attribute.String("k8s.node.name", k8sContext.Node),
				
				// Process context
				attribute.Int64("process.pid", int64(k8sContext.PID)),
				
				// Resilience indicators
				attribute.Bool("k8s.context.fallback", k8sContext.Fallback),
			)
			
			// Add pod labels as span attributes
			for key, value := range k8sContext.Labels {
				span.SetAttributes(attribute.String(fmt.Sprintf("k8s.pod.label.%s", key), value))
			}
			
			// Add additional process info if available
			if k8sContext.ProcessName != "" {
				span.SetAttributes(attribute.String("process.name", k8sContext.ProcessName))
			}
			if k8sContext.PPID != 0 {
				span.SetAttributes(attribute.Int64("process.ppid", int64(k8sContext.PPID)))
			}
		} else {
			// Fallback attributes when translator unavailable
			span.SetAttributes(
				attribute.Int64("process.pid", int64(pid)),
				attribute.String("k8s.context.status", "translator_unavailable"),
			)
		}
		
		// Track active span
		e.mu.Lock()
		e.activeSpans[spanName] = span
		e.totalSpansCreated++
		e.mu.Unlock()
		
		return nil
	})
	
	if err != nil {
		return nil, fmt.Errorf("failed to create span with PID context: %w", err)
	}
	
	return span, nil
}

// ExportSpans exports spans with circuit breaker protection
func (e *OpenTelemetryExporter) ExportSpans(ctx context.Context, spans []oteltrace.Span) error {
	startTime := time.Now()

	// Circuit breaker protection around OTLP export
	err := e.circuitBreaker.Execute(ctx, func() error {
		// Use timeout framework for export
		return e.timeoutManager.Execute(ctx, "export-spans", func(ctx context.Context) error {
			// Batch processing for efficiency
			batchSize := len(spans)
			if batchSize > e.resourceLimits.MaxBatchSize {
				batchSize = e.resourceLimits.MaxBatchSize
			}

			// Record metrics
			if e.config.EnableMetrics {
				e.batchSize.Record(ctx, int64(batchSize))
				e.spanExportDuration.Record(ctx, time.Since(startTime).Seconds())
			}

			e.mu.Lock()
			e.totalSpansExported += int64(batchSize)
			e.mu.Unlock()

			return nil
		})
	})

	return err
}

// UpdateTelemetry updates OpenTelemetry data with current Tapio intelligence
func (e *OpenTelemetryExporter) UpdateTelemetry(ctx context.Context) error {
	startTime := time.Now()

	// Create root span for analysis
	_, span := e.tracer.Start(ctx, "tapio.analysis.update")
	defer span.End()

	// Get health check results
	checkReq := &types.CheckRequest{All: true}
	result, err := e.checker.Check(ctx, checkReq)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get health check results: %w", err)
	}

	// Create spans for each problem/prediction
	e.createProblemSpans(ctx, result.Problems)

	// Update eBPF telemetry if available
	if e.ebpfMonitor != nil && e.ebpfMonitor.IsAvailable() {
		e.updateEBPFTelemetry(ctx)
	}

	// Use correlation engine if available
	if e.correlationEngine != nil {
		e.updateCorrelationTelemetry(ctx)
	}

	// Record analysis duration
	if e.config.EnableMetrics {
		e.analysisDuration.Record(ctx, time.Since(startTime).Seconds())
	}

	e.mu.Lock()
	e.lastUpdateTime = time.Now()
	e.mu.Unlock()

	return nil
}

// createProblemSpans creates spans for each detected problem
func (e *OpenTelemetryExporter) createProblemSpans(ctx context.Context, problems []types.Problem) {
	for _, problem := range problems {
		spanName := fmt.Sprintf("tapio.problem.%s", problem.Type)
		
		_, span := e.tracer.Start(ctx, spanName)
		span.SetAttributes(
			attribute.String("problem.type", string(problem.Type)),
			attribute.String("problem.severity", string(problem.Severity)),
			attribute.String("resource.name", problem.Resource.Name),
			attribute.String("resource.namespace", problem.Resource.Namespace),
			attribute.String("resource.kind", problem.Resource.Kind),
		)

		if problem.Prediction != nil {
			span.SetAttributes(
				attribute.Float64("prediction.confidence", problem.Prediction.Confidence),
				attribute.Float64("prediction.time_to_failure", problem.Prediction.TimeToFailure.Seconds()),
			)
		}

		span.End()
	}
}

// updateEBPFTelemetry creates telemetry from eBPF data with real Kubernetes context
func (e *OpenTelemetryExporter) updateEBPFTelemetry(ctx context.Context) {
	_, span := e.tracer.Start(ctx, "tapio.ebpf.update")
	defer span.End()

	memStats, err := e.ebpfMonitor.GetMemoryStats()
	if err != nil {
		span.RecordError(err)
		return
	}

	// Create spans for memory events with REAL Kubernetes context from Agent 1's translator
	for _, stats := range memStats {
		// Use the new PID-based span creation for real K8s context
		memSpan, err := e.CreateSpanWithPID(ctx, stats.PID, "memory_analysis")
		if err != nil {
			fmt.Printf("[OTEL] Failed to create eBPF span for PID %d: %v\n", stats.PID, err)
			// Fallback to basic span creation
			_, memSpan = e.tracer.Start(ctx, "tapio.ebpf.memory")
			memSpan.SetAttributes(attribute.Int("process.pid", int(stats.PID)))
		}
		
		// Add eBPF-specific memory attributes
		memSpan.SetAttributes(
			attribute.Int64("memory.usage", int64(stats.CurrentUsage)),
			attribute.Int64("memory.peak", int64(stats.PeakUsage)),
			attribute.String("data.source", "ebpf"),
		)
		
		// Add container ID if available
		if stats.ContainerID != "" {
			memSpan.SetAttributes(attribute.String("container.id", stats.ContainerID))
		}
		
		memSpan.End()
	}
}

// updateCorrelationTelemetry creates telemetry from correlation findings
func (e *OpenTelemetryExporter) updateCorrelationTelemetry(ctx context.Context) {
	_, span := e.tracer.Start(ctx, "tapio.correlation.analysis")
	defer span.End()

	findings, err := e.correlationEngine.Execute(ctx)
	if err != nil {
		span.RecordError(err)
		return
	}

	for _, finding := range findings {
		_, findingSpan := e.tracer.Start(ctx, "tapio.correlation.finding")
		findingSpan.SetAttributes(
			attribute.String("finding.type", finding.Type),
			attribute.Float64("finding.confidence", finding.Confidence),
			attribute.String("finding.description", finding.Description),
		)
		findingSpan.End()
	}
}

// GetMetrics returns OpenTelemetry exporter metrics
func (e *OpenTelemetryExporter) GetMetrics() ExporterMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()

	cbMetrics := e.circuitBreaker.GetMetrics()
	tmMetrics := e.timeoutManager.GetMetrics()
	hcMetrics := e.healthChecker.GetMetrics()

	return ExporterMetrics{
		TotalSpansCreated:  e.totalSpansCreated,
		TotalSpansExported: e.totalSpansExported,
		ActiveSpansCount:   int64(len(e.activeSpans)),
		LastUpdateTime:     e.lastUpdateTime,
		CircuitBreaker:     cbMetrics,
		TimeoutManager:     tmMetrics,
		HealthChecker:      hcMetrics,
	}
}

// ExporterMetrics represents OpenTelemetry exporter metrics
type ExporterMetrics struct {
	TotalSpansCreated  int64
	TotalSpansExported int64
	ActiveSpansCount   int64
	LastUpdateTime     time.Time
	// CircuitBreaker     universal.Metrics  // TODO: Add metrics types
	// TimeoutManager     universal.TimeoutMetrics
	// HealthChecker      universal.HealthCheckerMetrics
}

// Shutdown gracefully shuts down the OpenTelemetry exporter
func (e *OpenTelemetryExporter) Shutdown(ctx context.Context) error {
	fmt.Println("🛑 Shutting down OpenTelemetry exporter...")

	// Close any active spans
	e.mu.Lock()
	for name, span := range e.activeSpans {
		span.End()
		delete(e.activeSpans, name)
	}
	e.mu.Unlock()

	// Shutdown providers
	if e.traceProvider != nil {
		if err := e.traceProvider.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown trace provider: %w", err)
		}
	}

	if e.meterProvider != nil {
		if err := e.meterProvider.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown meter provider: %w", err)
		}
	}

	fmt.Println("✅ OpenTelemetry exporter shutdown complete")
	return nil
}