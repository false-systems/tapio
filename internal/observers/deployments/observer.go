package deployments

import (
	"context"
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// Observer tracks deployment-related changes in Kubernetes
type Observer struct {
	*base.BaseObserver        // Statistics and Health
	*base.EventChannelManager // Event channel management
	*base.LifecycleManager    // Goroutine management

	logger *zap.Logger
	config *Config
	client kubernetes.Interface

	// Informers
	deploymentInformer cache.SharedIndexInformer
	configMapInformer  cache.SharedIndexInformer
	secretInformer     cache.SharedIndexInformer

	// Deduplication
	recentEvents *lru.Cache[string, time.Time]

	// OpenTelemetry instrumentation
	tracer             trace.Tracer
	deploymentsTracked metric.Int64Counter
	configChanges      metric.Int64Counter
	rollbacks          metric.Int64Counter
	eventsProcessed    metric.Int64Counter
	eventsDropped      metric.Int64Counter
	processingTime     metric.Float64Histogram
}

// NewObserver creates a new deployments observer
func NewObserver(name string, config *Config) (*Observer, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OpenTelemetry
	meter := otel.Meter("tapio.observers.deployments")
	tracer := otel.Tracer("tapio.observers.deployments")

	// Create metrics
	deploymentsTracked, _ := meter.Int64Counter(
		fmt.Sprintf("%s_deployments_tracked_total", name),
		metric.WithDescription("Total deployment changes tracked"),
	)
	configChanges, _ := meter.Int64Counter(
		fmt.Sprintf("%s_config_changes_total", name),
		metric.WithDescription("Total ConfigMap/Secret changes tracked"),
	)
	rollbacks, _ := meter.Int64Counter(
		fmt.Sprintf("%s_rollbacks_total", name),
		metric.WithDescription("Total deployment rollbacks detected"),
	)
	eventsProcessed, _ := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total events processed"),
	)
	eventsDropped, _ := meter.Int64Counter(
		fmt.Sprintf("%s_events_dropped_total", name),
		metric.WithDescription("Total events dropped"),
	)
	processingTime, _ := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_time_ms", name),
		metric.WithDescription("Event processing time in milliseconds"),
	)

	// Create K8s client
	var client kubernetes.Interface
	if config.MockMode {
		client = fake.NewSimpleClientset()
	} else if config.KubeConfig != "" {
		restConfig, err := clientcmd.BuildConfigFromFlags("", config.KubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
		}
		client, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create client from kubeconfig: %w", err)
		}
	} else {
		restConfig, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
		}
		client, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create client from in-cluster config: %w", err)
		}
	}

	// Create LRU cache for deduplication
	recentEvents, err := lru.New[string, time.Time](1000)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}

	// Initialize base components
	ctx := context.Background()
	baseObserver := base.NewBaseObserver(name, 5*time.Minute)
	eventManager := base.NewEventChannelManager(config.BufferSize, name, logger)
	lifecycleManager := base.NewLifecycleManager(ctx, logger)

	// Store config name
	config.Name = name

	o := &Observer{
		BaseObserver:        baseObserver,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycleManager,
		logger:              logger.Named(name),
		config:              config,
		client:              client,
		recentEvents:        recentEvents,
		tracer:              tracer,
		deploymentsTracked:  deploymentsTracked,
		configChanges:       configChanges,
		rollbacks:           rollbacks,
		eventsProcessed:     eventsProcessed,
		eventsDropped:       eventsDropped,
		processingTime:      processingTime,
	}

	o.logger.Info("Deployments observer created",
		zap.String("name", name),
		zap.Int("buffer_size", config.BufferSize))

	return o, nil
}

// Start begins watching deployment changes
func (o *Observer) Start(ctx context.Context) error {
	o.logger.Info("Starting deployments observer")

	if o.config.MockMode {
		o.logger.Info("Running in mock mode - no actual K8s watching")
		o.BaseObserver.SetHealthy(true)
		return nil
	}

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("Deployments observer started")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping deployments observer")

	o.BaseObserver.SetHealthy(false)

	// Stop lifecycle manager
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	// Close event channel
	o.EventChannelManager.Close()

	o.logger.Info("Deployments observer stopped")
	return nil
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// shouldTrackDeployment checks if a deployment should be tracked
func (o *Observer) shouldTrackDeployment(deployment *appsv1.Deployment) bool {
	// Check namespace filter
	if len(o.config.Namespaces) > 0 {
		found := false
		for _, ns := range o.config.Namespaces {
			if deployment.Namespace == ns || ns == "" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check annotation filter
	if o.config.AnnotationFilter != "" {
		if _, exists := deployment.Annotations[o.config.AnnotationFilter]; !exists {
			return false
		}
	}

	// Skip system deployments if configured
	if o.config.IgnoreSystemDeployments {
		if deployment.Namespace == "kube-system" ||
			deployment.Namespace == "kube-public" ||
			deployment.Namespace == "kube-node-lease" {
			return false
		}
	}

	return true
}

// hasSignificantChange checks if a deployment update is significant
func (o *Observer) hasSignificantChange(old, new *appsv1.Deployment) bool {
	// Image change
	if len(old.Spec.Template.Spec.Containers) != len(new.Spec.Template.Spec.Containers) {
		return true
	}
	for i := range old.Spec.Template.Spec.Containers {
		if old.Spec.Template.Spec.Containers[i].Image != new.Spec.Template.Spec.Containers[i].Image {
			return true
		}
	}

	// Replica change
	if old.Spec.Replicas != nil && new.Spec.Replicas != nil {
		if *old.Spec.Replicas != *new.Spec.Replicas {
			return true
		}
	}

	// Strategy change
	if old.Spec.Strategy.Type != new.Spec.Strategy.Type {
		return true
	}

	return false
}

// isRollback detects if a deployment update is a rollback
func (o *Observer) isRollback(old, new *appsv1.Deployment) bool {
	// Check if revision went backwards
	oldRevision := old.Annotations["deployment.kubernetes.io/revision"]
	newRevision := new.Annotations["deployment.kubernetes.io/revision"]

	if oldRevision != "" && newRevision != "" && newRevision < oldRevision {
		return true
	}

	// Check if rollback annotation exists
	if _, exists := new.Annotations["kubectl.kubernetes.io/rollback"]; exists {
		return true
	}

	return false
}

// createDeploymentEvent creates a CollectorEvent for a deployment change
func (o *Observer) createDeploymentEvent(deployment *appsv1.Deployment, action string, oldDeployment *appsv1.Deployment) *domain.CollectorEvent {
	start := time.Now()
	defer func() {
		if o.processingTime != nil {
			o.processingTime.Record(context.Background(), float64(time.Since(start).Milliseconds()))
		}
	}()

	// Build message with change details
	message := fmt.Sprintf("Deployment %s %s", deployment.Name, action)
	if oldDeployment != nil && action == "updated" {
		// Include change details
		if len(deployment.Spec.Template.Spec.Containers) > 0 && len(oldDeployment.Spec.Template.Spec.Containers) > 0 {
			oldImage := oldDeployment.Spec.Template.Spec.Containers[0].Image
			newImage := deployment.Spec.Template.Spec.Containers[0].Image
			if oldImage != newImage {
				message = fmt.Sprintf("Deployment %s updated: image %s -> %s", deployment.Name, oldImage, newImage)
			}
		}
	}

	// Create K8s resource data
	resourceData := domain.K8sResourceData{
		Kind:       "Deployment",
		Name:       deployment.Name,
		Namespace:  deployment.Namespace,
		APIVersion: "apps/v1",
	}

	// Create K8s API event data
	k8sEventData := &domain.K8sAPIEventData{
		Action:         action,
		Reason:         fmt.Sprintf("Deployment%s", action),
		Message:        message,
		Type:           "Normal",
		Count:          1,
		FirstTime:      time.Now(),
		LastTime:       time.Now(),
		InvolvedObject: resourceData,
		Source: domain.EventSource{
			Component: "deployments-observer",
		},
	}

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("deployment-%s-%s-%d", deployment.Namespace, deployment.Name, time.Now().UnixNano()),
		Timestamp: time.Now(),
		Type:      domain.EventTypeK8sDeployment,
		Source:    fmt.Sprintf("deployments-%s", o.config.Name),
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			KubernetesEvent: k8sEventData,
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer":   o.config.Name,
				"version":    "1.0.0",
				"action":     action,
				"namespace":  deployment.Namespace,
				"deployment": deployment.Name,
			},
		},
	}
}

// sendEvent sends an event through the channel with deduplication
func (o *Observer) sendEvent(event *domain.CollectorEvent) {
	// Check for duplicate
	eventKey := fmt.Sprintf("%s-%s", event.Type, event.EventID)
	if lastSeen, exists := o.recentEvents.Get(eventKey); exists {
		if time.Since(lastSeen) < o.config.DeduplicationWindow {
			return // Skip duplicate
		}
	}

	// Update last seen
	o.recentEvents.Add(eventKey, time.Now())

	// Send event
	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		o.eventsProcessed.Add(context.Background(), 1)
		o.logger.Debug("Sent deployment event",
			zap.String("event_id", event.EventID),
			zap.String("type", string(event.Type)))
	} else {
		o.BaseObserver.RecordDrop()
		o.eventsDropped.Add(context.Background(), 1)
		o.logger.Warn("Dropped deployment event - channel full",
			zap.String("event_id", event.EventID))
	}
}
