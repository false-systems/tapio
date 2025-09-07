package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/observers/base"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Observer implements the service map observer using BaseObserver
type Observer struct {
	*base.BaseObserver        // Provides Statistics() and Health()
	*base.EventChannelManager // Handles event channels
	*base.LifecycleManager    // Manages goroutines

	name   string
	config *Config
	logger *zap.Logger

	// Kubernetes client
	k8sClient kubernetes.Interface

	// Service tracking
	services    map[string]*Service    // namespace/name -> service
	connections map[string]*Connection // src:port->dst:port -> connection
	ipToService map[string][]string    // IP[:port] -> []service names
	mu          sync.RWMutex

	// eBPF state (platform-specific)
	ebpfState interface{}

	// Emission control
	lastEmitted        *ServiceMap
	lastEmitTime       time.Time
	pendingChanges     chan ChangeEvent
	changeDebouncer    *time.Timer
	significantChanges int32 // atomic counter

	// OTEL instrumentation
	tracer trace.Tracer

	// Core metrics (mandatory)
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Float64Gauge

	// Service-specific metrics
	servicesDiscovered   metric.Int64Counter
	servicesRemoved      metric.Int64Counter
	connectionsTracked   metric.Int64Counter
	dependenciesDetected metric.Int64Counter
	healthChanges        metric.Int64Counter
	k8sApiCalls          metric.Int64Counter
	ebpfEvents           metric.Int64Counter
}

// NewObserver creates a new service map observer
func NewObserver(name string, config *Config) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if config.Logger == nil {
		logger, err := zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
		config.Logger = logger
	}

	// Initialize OTEL components
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create core metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total events dropped by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Float64Gauge(
		fmt.Sprintf("%s_buffer_usage_ratio", name),
		metric.WithDescription(fmt.Sprintf("Buffer usage ratio for %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	// Create service-specific metrics
	servicesDiscovered, err := meter.Int64Counter(
		fmt.Sprintf("%s_services_discovered_total", name),
		metric.WithDescription(fmt.Sprintf("Total services discovered by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create services discovered counter", zap.Error(err))
	}

	servicesRemoved, err := meter.Int64Counter(
		fmt.Sprintf("%s_services_removed_total", name),
		metric.WithDescription(fmt.Sprintf("Total services removed by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create services removed counter", zap.Error(err))
	}

	connectionsTracked, err := meter.Int64Counter(
		fmt.Sprintf("%s_connections_tracked_total", name),
		metric.WithDescription(fmt.Sprintf("Total connections tracked by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create connections tracked counter", zap.Error(err))
	}

	dependenciesDetected, err := meter.Int64Counter(
		fmt.Sprintf("%s_dependencies_detected_total", name),
		metric.WithDescription(fmt.Sprintf("Total dependencies detected by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create dependencies detected counter", zap.Error(err))
	}

	healthChanges, err := meter.Int64Counter(
		fmt.Sprintf("%s_health_changes_total", name),
		metric.WithDescription(fmt.Sprintf("Total health changes detected by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create health changes counter", zap.Error(err))
	}

	k8sApiCalls, err := meter.Int64Counter(
		fmt.Sprintf("%s_k8s_api_calls_total", name),
		metric.WithDescription(fmt.Sprintf("Total Kubernetes API calls by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create k8s api calls counter", zap.Error(err))
	}

	ebpfEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF events processed by %s", name)),
	)
	if err != nil {
		config.Logger.Warn("Failed to create ebpf events counter", zap.Error(err))
	}

	// Initialize base components
	baseConfig := base.BaseObserverConfig{
		Name:               name,
		HealthCheckTimeout: 5 * time.Minute,
		ErrorRateThreshold: 0.05, // 5% error rate threshold
		Logger:             config.Logger.Named("base"),
	}
	baseObserver := base.NewBaseObserverWithConfig(baseConfig)

	eventChannel := base.NewEventChannelManager(config.BufferSize, name, config.Logger)
	lifecycle := base.NewLifecycleManager(context.Background(), config.Logger)

	// Initialize Kubernetes client if enabled
	var k8sClient kubernetes.Interface
	if config.EnableK8sDiscovery {
		client, err := createK8sClient(config.KubeConfig)
		if err != nil {
			config.Logger.Warn("Failed to create Kubernetes client, K8s discovery disabled",
				zap.Error(err))
		} else {
			k8sClient = client
		}
	}

	o := &Observer{
		BaseObserver:        baseObserver,
		EventChannelManager: eventChannel,
		LifecycleManager:    lifecycle,

		name:      name,
		config:    config,
		logger:    config.Logger.Named(name),
		k8sClient: k8sClient,

		services:       make(map[string]*Service),
		connections:    make(map[string]*Connection),
		ipToService:    make(map[string][]string),
		pendingChanges: make(chan ChangeEvent, 1000),
		lastEmitTime:   time.Now(),

		// OTEL
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,

		// Service-specific
		servicesDiscovered:   servicesDiscovered,
		servicesRemoved:      servicesRemoved,
		connectionsTracked:   connectionsTracked,
		dependenciesDetected: dependenciesDetected,
		healthChanges:        healthChanges,
		k8sApiCalls:          k8sApiCalls,
		ebpfEvents:           ebpfEvents,
	}

	o.logger.Info("Service map observer created", zap.String("name", name))
	return o, nil
}

// Name returns observer name
func (o *Observer) Name() string {
	return o.name
}

// Start starts the service map observer
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting service map observer")

	// Mark as healthy
	o.BaseObserver.SetHealthy(true)

	// Start Kubernetes discovery if enabled
	if o.k8sClient != nil {
		o.LifecycleManager.Start("k8s-discovery", func() {
			o.watchKubernetesServices(ctx)
		})
	}

	// Start eBPF monitoring if enabled
	if o.config.EnableEBPF {
		if err := o.initializeEBPF(ctx); err != nil {
			o.logger.Warn("Failed to initialize eBPF, connection tracking disabled", zap.Error(err))
		}
	}

	// Start change processor
	o.LifecycleManager.Start("change-processor", func() {
		o.processChanges(ctx)
	})

	// Start periodic snapshot emission
	if o.config.FullSnapshotInterval > 0 {
		o.LifecycleManager.Start("snapshot-emitter", func() {
			o.emitSnapshots(ctx)
		})
	}

	o.logger.Info("Service map observer started")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping service map observer")

	// Stop lifecycle manager
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Error("Failed to stop lifecycle manager", zap.Error(err))
	}

	// Cleanup eBPF if initialized
	if o.ebpfState != nil {
		o.cleanupEBPF()
	}

	// Close event channel
	o.EventChannelManager.Close()

	// Mark as unhealthy
	o.BaseObserver.SetHealthy(false)

	o.logger.Info("Service map observer stopped")
	return nil
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// IsHealthy returns health status
func (o *Observer) IsHealthy() bool {
	health := o.BaseObserver.Health()
	return health.Status == domain.HealthHealthy
}

// Statistics returns observer statistics
func (o *Observer) Statistics() interface{} {
	stats := o.BaseObserver.Statistics()

	// Add service-specific stats
	if stats.CustomMetrics == nil {
		stats.CustomMetrics = make(map[string]string)
	}

	o.mu.RLock()
	stats.CustomMetrics["services_count"] = fmt.Sprintf("%d", len(o.services))
	stats.CustomMetrics["connections_count"] = fmt.Sprintf("%d", len(o.connections))
	o.mu.RUnlock()

	stats.CustomMetrics["significant_changes"] = fmt.Sprintf("%d", atomic.LoadInt32(&o.significantChanges))
	stats.CustomMetrics["ebpf_enabled"] = fmt.Sprintf("%v", o.ebpfState != nil)

	return stats
}

// Health returns health status
func (o *Observer) Health() *domain.HealthStatus {
	health := o.BaseObserver.Health()
	health.Component = o.name

	// Add error count from statistics
	stats := o.BaseObserver.Statistics()
	if stats != nil {
		health.ErrorCount = stats.ErrorCount
	}

	// Check K8s connectivity if enabled
	if o.config.EnableK8sDiscovery && o.k8sClient == nil {
		health.Status = domain.HealthDegraded
		health.Message = "Kubernetes discovery enabled but client not connected"
	}

	return health
}

// createK8sClient creates a Kubernetes client
func createK8sClient(kubeconfig string) (kubernetes.Interface, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		// Use the provided kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		// Try in-cluster config first
		config, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to default kubeconfig
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			configOverrides := &clientcmd.ConfigOverrides{}
			kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
			config, err = kubeConfig.ClientConfig()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, nil
}
