package helmcorrelator

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -Wall -Werror" -type helm_event helmmonitor bpf_src/helm_monitor.c

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// Collector implements the helm-correlator collector
type Collector struct {
	*base.BaseCollector       // Statistics, Health, OTEL
	*base.EventChannelManager // Event publishing
	*base.LifecycleManager    // Goroutine management

	logger *zap.Logger
	config *Config
	mu     sync.RWMutex

	// Kubernetes client and informers
	k8sClient       kubernetes.Interface
	informerFactory informers.SharedInformerFactory
	secretInformer  cache.SharedIndexInformer
	eventInformer   cache.SharedIndexInformer
	podInformer     cache.SharedIndexInformer
	jobInformer     cache.SharedIndexInformer

	// eBPF components (platform-specific, defined in collector_ebpf.go)
	ebpfState interface{}

	// Correlation engine
	correlator *HelmCorrelationEngine

	// Operation tracking
	operations sync.Map // PID -> *HelmOperation

	// Release cache for quick lookups
	releaseCache sync.Map // namespace/name -> *HelmRelease
}

var _ collectors.Collector = (*Collector)(nil)

// NewCollector creates a new helm-correlator collector
func NewCollector(name string, config *Config) (*Collector, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Override with provided config if any
	if config != nil {
		if config.BufferSize > 0 {
			cfg.BufferSize = config.BufferSize
		}
		if config.CorrelationWindow > 0 {
			cfg.CorrelationWindow = config.CorrelationWindow
		}
		if config.StuckReleaseTimeout > 0 {
			cfg.StuckReleaseTimeout = config.StuckReleaseTimeout
		}
		if config.HookTimeout > 0 {
			cfg.HookTimeout = config.HookTimeout
		}
		// Explicitly set boolean fields
		cfg.EnableEBPF = config.EnableEBPF
		cfg.EnableK8sWatching = config.EnableK8sWatching
		cfg.TrackKubectl = config.TrackKubectl
		cfg.TrackFiles = config.TrackFiles
		cfg.TrackAPI = config.TrackAPI

		if config.KubeConfig != "" {
			cfg.KubeConfig = config.KubeConfig
		}
		if len(config.Namespaces) > 0 {
			cfg.Namespaces = config.Namespaces
		}
		// Use config name if provided
		if config.Name != "" {
			cfg.Name = config.Name
		}
	}
	// Only set name if not already set
	if cfg.Name == "" {
		cfg.Name = name
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize base components (following Tapio patterns)
	ctx := context.Background()
	baseCollector := base.NewBaseCollector("helm-correlator", 5*time.Minute)
	eventManager := base.NewEventChannelManager(cfg.BufferSize, "helm-correlator", logger)
	lifecycleManager := base.NewLifecycleManager(ctx, logger)

	c := &Collector{
		BaseCollector:       baseCollector,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycleManager,
		logger:              logger.Named(name),
		config:              cfg,
		correlator:          NewHelmCorrelationEngine(logger),
	}

	// Initialize Kubernetes client if watching is enabled
	if cfg.EnableK8sWatching {
		if err := c.initK8sClient(); err != nil {
			return nil, fmt.Errorf("failed to initialize K8s client: %w", err)
		}
	}

	c.logger.Info("Helm correlator created",
		zap.String("name", name),
		zap.Bool("ebpf_enabled", cfg.EnableEBPF),
		zap.Bool("k8s_enabled", cfg.EnableK8sWatching),
		zap.Duration("correlation_window", cfg.CorrelationWindow),
	)

	return c, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.config.Name
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// IsHealthy returns true if the collector is healthy
func (c *Collector) IsHealthy() bool {
	return c.BaseCollector.IsHealthy()
}

// Start starts the helm-correlator collector
func (c *Collector) Start(ctx context.Context) error {
	c.logger.Info("Starting helm-correlator collector")

	// Start base components
	c.LifecycleManager.Start("helm-correlator", func() {
		// Cleanup function called on stop
		c.logger.Info("Helm correlator cleanup")
	})

	// Start K8s watchers if enabled
	if c.config.EnableK8sWatching {
		if err := c.startK8sWatchers(); err != nil {
			return fmt.Errorf("failed to start K8s watchers: %w", err)
		}
	}

	// Start eBPF if available and enabled (platform-specific)
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			c.logger.Warn("eBPF not available, running in K8s-only mode", zap.Error(err))
			// Continue without eBPF - still useful with just K8s watching
		}
	}

	// Start processing goroutines
	go c.processOperations()
	go c.correlateFailures()

	// Mark as healthy
	c.BaseCollector.SetHealthy(true)

	c.logger.Info("Helm correlator started successfully")
	return nil
}

// Stop stops the helm-correlator collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping helm-correlator collector")

	c.BaseCollector.SetHealthy(false)

	// Stop eBPF if running
	c.stopEBPF()

	// Stop K8s informers
	if c.informerFactory != nil {
		c.informerFactory.Shutdown()
	}

	// Stop lifecycle manager (cancels context)
	err := c.LifecycleManager.Stop(30 * time.Second)
	if err != nil {
		c.logger.Warn("Error stopping lifecycle manager", zap.Error(err))
	}

	c.logger.Info("Helm correlator stopped")
	return nil
}

// initK8sClient initializes the Kubernetes client
func (c *Collector) initK8sClient() error {
	var config *rest.Config
	var err error

	if c.config.KubeConfig != "" {
		// Use kubeconfig file
		config, err = clientcmd.BuildConfigFromFlags("", c.config.KubeConfig)
	} else {
		// Use in-cluster config
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		return fmt.Errorf("failed to create K8s config: %w", err)
	}

	c.k8sClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create K8s client: %w", err)
	}

	// Create informer factory
	options := []informers.SharedInformerOption{}

	// Add namespace filter if specified
	if len(c.config.Namespaces) > 0 {
		// For simplicity, we'll handle single namespace initially
		// Multi-namespace requires multiple factories
		if len(c.config.Namespaces) == 1 {
			options = append(options, informers.WithNamespace(c.config.Namespaces[0]))
		}
	}

	c.informerFactory = informers.NewSharedInformerFactoryWithOptions(c.k8sClient, 0, options...)

	return nil
}

// startK8sWatchers starts the Kubernetes informers
func (c *Collector) startK8sWatchers() error {
	// Secret informer for Helm releases
	c.secretInformer = c.informerFactory.Core().V1().Secrets().Informer()
	c.secretInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onSecretAdd,
		UpdateFunc: c.onSecretUpdate,
		DeleteFunc: c.onSecretDelete,
	})

	// Event informer for K8s events
	c.eventInformer = c.informerFactory.Core().V1().Events().Informer()
	c.eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.onEventAdd,
	})

	// Pod informer for pod status
	c.podInformer = c.informerFactory.Core().V1().Pods().Informer()
	c.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: c.onPodUpdate,
	})

	// Job informer for hook jobs
	c.jobInformer = c.informerFactory.Batch().V1().Jobs().Informer()
	c.jobInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: c.onJobUpdate,
	})

	// Start informers
	c.informerFactory.Start(c.LifecycleManager.Context().Done())

	// Wait for caches to sync
	if !cache.WaitForCacheSync(c.LifecycleManager.Context().Done(),
		c.secretInformer.HasSynced,
		c.eventInformer.HasSynced,
		c.podInformer.HasSynced,
		c.jobInformer.HasSynced) {
		return fmt.Errorf("failed to sync K8s caches")
	}

	c.logger.Info("K8s watchers started and synced")
	return nil
}

// processOperations processes Helm operations and correlates with K8s
func (c *Collector) processOperations() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			return

		case <-ticker.C:
			// Check for stuck or failed operations
			now := time.Now()
			c.operations.Range(func(key, value interface{}) bool {
				op := value.(*HelmOperation)

				// Check if operation is stuck
				if op.EndTime.IsZero() && now.Sub(op.StartTime) > c.config.StuckReleaseTimeout {
					c.logger.Warn("Helm operation appears stuck",
						zap.String("operation_id", op.ID),
						zap.String("command", op.Command),
						zap.Duration("duration", now.Sub(op.StartTime)),
					)

					// Attempt correlation
					c.correlateOperation(op)
				}

				// Clean up old completed operations
				if !op.EndTime.IsZero() && now.Sub(op.EndTime) > c.config.CorrelationWindow {
					c.operations.Delete(key)
				}

				return true
			})
		}
	}
}

// correlateFailures continuously correlates failures
func (c *Collector) correlateFailures() {
	// This runs when we detect failures from K8s events or eBPF
	// Implementation depends on the correlation engine
	ctx := c.LifecycleManager.Context()
	<-ctx.Done()
}

// correlateOperation correlates a Helm operation with K8s state
func (c *Collector) correlateOperation(op *HelmOperation) {
	// Build correlation context
	ctx := &CorrelationContext{
		Operation: op,
		TimeWindow: TimeWindow{
			Start: op.StartTime.Add(-30 * time.Second), // Look back a bit
			End:   time.Now(),
		},
	}

	// Get related K8s resources
	// This would query our cached data from informers
	// For now, placeholder

	// Run correlation
	rootCause := c.correlator.Correlate(ctx)
	if rootCause != nil {
		c.emitFailureEvent(rootCause)
		op.RootCause = rootCause
		op.Failed = true
	}
}

// emitFailureEvent emits a domain event for a Helm failure
func (c *Collector) emitFailureEvent(rootCause *RootCause) {
	event := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("helm-%d", time.Now().UnixNano()),
		Type:      domain.EventTypeK8sEvent, // Using K8s event type for Helm
		Severity:  domain.EventSeverityError,
		Source:    "helm-correlator",
		Timestamp: rootCause.FailureTime,
		EventData: domain.EventDataContainer{
			KubernetesEvent: &domain.K8sAPIEventData{
				Reason:  rootCause.Pattern,
				Message: rootCause.Summary,
				Type:    "Warning",
				InvolvedObject: domain.K8sResourceData{
					Kind:      "HelmRelease",
					Name:      rootCause.ReleaseName,
					Namespace: rootCause.Namespace,
				},
				Source: domain.EventSource{
					Component: "helm-correlator",
					Host:      "",
				},
			},
		},
		Metadata: domain.EventMetadata{
			Tags: []string{
				"helm-failure",
				rootCause.Pattern,
				fmt.Sprintf("release:%s", rootCause.ReleaseName),
			},
			Labels: map[string]string{
				"release":    rootCause.ReleaseName,
				"namespace":  rootCause.Namespace,
				"pattern":    rootCause.Pattern,
				"root_cause": rootCause.Summary,
				"details":    rootCause.Details,
				"resolution": rootCause.Resolution,
				"impact":     rootCause.Impact,
			},
		},
		CorrelationHints: &domain.CorrelationHints{
			// Add correlation hints if needed
			ProcessID: 0, // Could add PID if from eBPF
		},
	}

	// Send event through EventChannelManager
	if !c.EventChannelManager.SendEvent(event) {
		c.logger.Warn("Failed to send Helm failure event - channel full")
	}

	c.logger.Info("Helm failure detected and emitted",
		zap.String("pattern", rootCause.Pattern),
		zap.String("release", rootCause.ReleaseName),
		zap.String("summary", rootCause.Summary),
		zap.Float32("confidence", rootCause.Confidence),
	)
}
