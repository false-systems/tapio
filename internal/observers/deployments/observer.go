package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// defaultLRUCacheSize is the default size for the event deduplication cache
	defaultLRUCacheSize = 1000

	// defaultHealthCheckTimeout is the default timeout for health checks
	defaultHealthCheckTimeout = 5 * time.Minute

	// observerVersion is the version of the deployments observer
	observerVersion = "1.0.0"
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
	informerFactory    informers.SharedInformerFactory
	deploymentInformer cache.SharedIndexInformer
	configMapInformer  cache.SharedIndexInformer
	secretInformer     cache.SharedIndexInformer
	stopCh             chan struct{}

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
	deploymentsTracked, err := meter.Int64Counter(
		fmt.Sprintf("%s_deployments_tracked_total", name),
		metric.WithDescription("Total deployment changes tracked"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployments_tracked metric: %w", err)
	}

	configChanges, err := meter.Int64Counter(
		fmt.Sprintf("%s_config_changes_total", name),
		metric.WithDescription("Total ConfigMap/Secret changes tracked"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create config_changes metric: %w", err)
	}

	rollbacks, err := meter.Int64Counter(
		fmt.Sprintf("%s_rollbacks_total", name),
		metric.WithDescription("Total deployment rollbacks detected"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create rollbacks metric: %w", err)
	}

	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total events processed"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events_processed metric: %w", err)
	}

	eventsDropped, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_dropped_total", name),
		metric.WithDescription("Total events dropped"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events_dropped metric: %w", err)
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_time_ms", name),
		metric.WithDescription("Event processing time in milliseconds"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create processing_time metric: %w", err)
	}

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
	recentEvents, err := lru.New[string, time.Time](defaultLRUCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}

	// Initialize base components with multi-output support
	ctx := context.Background()
	baseObserver := base.NewBaseObserverWithConfig(base.BaseObserverConfig{
		Name:               name,
		HealthCheckTimeout: defaultHealthCheckTimeout,
		ErrorRateThreshold: 0.1,
		OutputTargets: base.OutputTargets{
			OTEL:    config.EnableOTEL,
			Stdout:  config.EnableStdout,
			Channel: true, // Always enabled for backward compat
		},
		StdoutConfig: &base.StdoutEmitterConfig{
			Pretty: true, // Pretty print for debugging
		},
		Logger: logger,
	})
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

	// Create informer factory
	o.informerFactory = informers.NewSharedInformerFactoryWithOptions(
		o.client,
		o.config.ResyncPeriod,
		informers.WithNamespace(o.getNamespaceFilter()),
	)

	// Setup deployment informer
	o.deploymentInformer = o.informerFactory.Apps().V1().Deployments().Informer()
	o.deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    o.handleDeploymentAdd,
		UpdateFunc: o.handleDeploymentUpdate,
		DeleteFunc: o.handleDeploymentDelete,
	})

	// Setup ConfigMap informer if enabled
	if o.config.TrackConfigMaps {
		o.configMapInformer = o.informerFactory.Core().V1().ConfigMaps().Informer()
		o.configMapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    o.handleConfigMapAdd,
			UpdateFunc: o.handleConfigMapUpdate,
			DeleteFunc: o.handleConfigMapDelete,
		})
	}

	// Setup Secret informer if enabled
	if o.config.TrackSecrets {
		o.secretInformer = o.informerFactory.Core().V1().Secrets().Informer()
		o.secretInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    o.handleSecretAdd,
			UpdateFunc: o.handleSecretUpdate,
			DeleteFunc: o.handleSecretDelete,
		})
	}

	// Start informers
	o.stopCh = make(chan struct{})
	o.informerFactory.Start(o.stopCh)

	// Wait for cache sync
	o.logger.Info("Waiting for informer cache to sync")
	if !cache.WaitForCacheSync(o.stopCh, o.deploymentInformer.HasSynced) {
		return fmt.Errorf("failed to sync deployment informer cache")
	}

	if o.config.TrackConfigMaps && o.configMapInformer != nil {
		if !cache.WaitForCacheSync(o.stopCh, o.configMapInformer.HasSynced) {
			return fmt.Errorf("failed to sync configmap informer cache")
		}
	}

	if o.config.TrackSecrets && o.secretInformer != nil {
		if !cache.WaitForCacheSync(o.stopCh, o.secretInformer.HasSynced) {
			return fmt.Errorf("failed to sync secret informer cache")
		}
	}

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("Deployments observer started")
	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	o.logger.Info("Stopping deployments observer")

	o.BaseObserver.SetHealthy(false)

	// Stop informers if running
	if o.stopCh != nil {
		close(o.stopCh)
		o.stopCh = nil
	}

	// Stop lifecycle manager
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	// Close output emitters
	if err := o.BaseObserver.CloseOutputs(); err != nil {
		o.logger.Warn("Failed to close output emitters", zap.Error(err))
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
	if _, exists := new.Annotations["deployment.kubernetes.io/rollback"]; exists {
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

	// For create events, include initial image
	if action == "created" && len(deployment.Spec.Template.Spec.Containers) > 0 {
		image := deployment.Spec.Template.Spec.Containers[0].Image
		message = fmt.Sprintf("Deployment %s created with image %s", deployment.Name, image)
	}

	// For updates, include change details
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

	// For scale events, include replica count
	if action == "scaled" && deployment.Spec.Replicas != nil {
		message = fmt.Sprintf("Deployment %s scaled (replicas: %d)", deployment.Name, *deployment.Spec.Replicas)
		if oldDeployment != nil && oldDeployment.Spec.Replicas != nil {
			message = fmt.Sprintf("Deployment %s scaled from %d to %d (replicas: %d)",
				deployment.Name, *oldDeployment.Spec.Replicas, *deployment.Spec.Replicas, *deployment.Spec.Replicas)
		}
	}

	// For rollback events, include image info
	if action == "rolled-back" && len(deployment.Spec.Template.Spec.Containers) > 0 {
		image := deployment.Spec.Template.Spec.Containers[0].Image
		message = fmt.Sprintf("Deployment %s rolled back to image %s", deployment.Name, image)
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

	// Gather correlation context
	correlationContext := o.gatherCorrelationContext(deployment)

	// Enhanced metadata with correlation context
	metadata := domain.EventMetadata{
		Labels: map[string]string{
			"observer":   o.config.Name,
			"version":    observerVersion,
			"action":     action,
			"namespace":  deployment.Namespace,
			"deployment": deployment.Name,
		},
		Attributes: make(map[string]string),
	}

	// Store correlation context as JSON in attributes
	if correlationBytes, err := json.Marshal(correlationContext); err == nil {
		metadata.Attributes["correlation_context"] = string(correlationBytes)
	}

	// Add deployment-specific labels
	if deployment.Labels != nil {
		for k, v := range deployment.Labels {
			metadata.Labels["app."+k] = v
		}
	}

	// Add container image info for correlation
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		container := deployment.Spec.Template.Spec.Containers[0]
		metadata.Labels["image"] = container.Image
		metadata.Labels["container"] = container.Name
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
		Metadata: metadata,
	}
}

// CorrelationContext represents rich context data for correlation
type CorrelationContext struct {
	Deployment DeploymentContext  `json:"deployment"`
	Containers []ContainerContext `json:"containers"`
	Volumes    []VolumeContext    `json:"volumes,omitempty"`
	Services   []ServiceContext   `json:"services,omitempty"`
	Owners     []OwnerContext     `json:"owners,omitempty"`
}

// DeploymentContext contains deployment-specific correlation data
type DeploymentContext struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	UID         string            `json:"uid"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Replicas    *int32            `json:"replicas,omitempty"`
	Strategy    string            `json:"strategy"`
}

// ContainerContext contains container-specific correlation data
type ContainerContext struct {
	Name         string               `json:"name"`
	Image        string               `json:"image"`
	Ports        []PortContext        `json:"ports,omitempty"`
	Env          []EnvContext         `json:"env,omitempty"`
	VolumeMounts []VolumeMountContext `json:"volumeMounts,omitempty"`
}

// PortContext contains port correlation data
type PortContext struct {
	Name          string `json:"name,omitempty"`
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

// EnvContext contains environment variable correlation data
type EnvContext struct {
	Name         string               `json:"name"`
	Value        string               `json:"value,omitempty"`
	ConfigMapRef *ConfigMapRefContext `json:"configMapRef,omitempty"`
	SecretRef    *SecretRefContext    `json:"secretRef,omitempty"`
}

// ConfigMapRefContext contains ConfigMap reference data
type ConfigMapRefContext struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// SecretRefContext contains Secret reference data
type SecretRefContext struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// VolumeMountContext contains volume mount correlation data
type VolumeMountContext struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly"`
}

// VolumeContext contains volume correlation data
type VolumeContext struct {
	Name      string                  `json:"name"`
	ConfigMap *ConfigMapVolumeContext `json:"configMap,omitempty"`
	Secret    *SecretVolumeContext    `json:"secret,omitempty"`
	PVC       *PVCVolumeContext       `json:"pvc,omitempty"`
}

// ConfigMapVolumeContext contains ConfigMap volume data
type ConfigMapVolumeContext struct {
	Name        string `json:"name"`
	DefaultMode *int32 `json:"defaultMode,omitempty"`
}

// SecretVolumeContext contains Secret volume data
type SecretVolumeContext struct {
	SecretName  string `json:"secretName"`
	DefaultMode *int32 `json:"defaultMode,omitempty"`
}

// PVCVolumeContext contains PVC volume data
type PVCVolumeContext struct {
	ClaimName string `json:"claimName"`
}

// ServiceContext contains service correlation data
type ServiceContext struct {
	Name      string               `json:"name"`
	Namespace string               `json:"namespace"`
	Type      string               `json:"type"`
	Selector  map[string]string    `json:"selector,omitempty"`
	Ports     []ServicePortContext `json:"ports,omitempty"`
}

// ServicePortContext contains service port data
type ServicePortContext struct {
	Name       string `json:"name,omitempty"`
	Port       int32  `json:"port"`
	TargetPort string `json:"targetPort,omitempty"`
	Protocol   string `json:"protocol"`
}

// OwnerContext contains owner reference data
type OwnerContext struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Controller *bool  `json:"controller,omitempty"`
}

// gatherCorrelationContext collects rich context data for correlation
func (o *Observer) gatherCorrelationContext(deployment *appsv1.Deployment) CorrelationContext {
	ctx := context.Background()
	correlationCtx := CorrelationContext{}

	// Basic deployment info
	correlationCtx.Deployment = DeploymentContext{
		Name:        deployment.Name,
		Namespace:   deployment.Namespace,
		UID:         string(deployment.UID),
		Labels:      deployment.Labels,
		Annotations: deployment.Annotations,
		Replicas:    deployment.Spec.Replicas,
		Strategy:    string(deployment.Spec.Strategy.Type),
	}

	// Container specifications
	correlationCtx.Containers = make([]ContainerContext, 0, len(deployment.Spec.Template.Spec.Containers))
	for _, container := range deployment.Spec.Template.Spec.Containers {
		containerCtx := ContainerContext{
			Name:  container.Name,
			Image: container.Image,
		}

		// Container ports
		if len(container.Ports) > 0 {
			containerCtx.Ports = make([]PortContext, 0, len(container.Ports))
			for _, port := range container.Ports {
				portCtx := PortContext{
					Name:          port.Name,
					ContainerPort: port.ContainerPort,
					Protocol:      string(port.Protocol),
				}
				containerCtx.Ports = append(containerCtx.Ports, portCtx)
			}
		}

		// Environment variables (for config tracking)
		if len(container.Env) > 0 {
			containerCtx.Env = make([]EnvContext, 0, len(container.Env))
			for _, env := range container.Env {
				envCtx := EnvContext{
					Name:  env.Name,
					Value: env.Value,
				}
				// Track ConfigMap and Secret references
				if env.ValueFrom != nil {
					if env.ValueFrom.ConfigMapKeyRef != nil {
						envCtx.ConfigMapRef = &ConfigMapRefContext{
							Name: env.ValueFrom.ConfigMapKeyRef.Name,
							Key:  env.ValueFrom.ConfigMapKeyRef.Key,
						}
					}
					if env.ValueFrom.SecretKeyRef != nil {
						envCtx.SecretRef = &SecretRefContext{
							Name: env.ValueFrom.SecretKeyRef.Name,
							Key:  env.ValueFrom.SecretKeyRef.Key,
						}
					}
				}
				containerCtx.Env = append(containerCtx.Env, envCtx)
			}
		}

		// Volume mounts
		if len(container.VolumeMounts) > 0 {
			containerCtx.VolumeMounts = make([]VolumeMountContext, 0, len(container.VolumeMounts))
			for _, mount := range container.VolumeMounts {
				mountCtx := VolumeMountContext{
					Name:      mount.Name,
					MountPath: mount.MountPath,
					ReadOnly:  mount.ReadOnly,
				}
				containerCtx.VolumeMounts = append(containerCtx.VolumeMounts, mountCtx)
			}
		}

		correlationCtx.Containers = append(correlationCtx.Containers, containerCtx)
	}

	// Volume specifications (ConfigMaps, Secrets, etc.)
	if len(deployment.Spec.Template.Spec.Volumes) > 0 {
		correlationCtx.Volumes = make([]VolumeContext, 0, len(deployment.Spec.Template.Spec.Volumes))
		for _, volume := range deployment.Spec.Template.Spec.Volumes {
			volumeCtx := VolumeContext{
				Name: volume.Name,
			}

			if volume.ConfigMap != nil {
				volumeCtx.ConfigMap = &ConfigMapVolumeContext{
					Name:        volume.ConfigMap.Name,
					DefaultMode: volume.ConfigMap.DefaultMode,
				}
			}
			if volume.Secret != nil {
				volumeCtx.Secret = &SecretVolumeContext{
					SecretName:  volume.Secret.SecretName,
					DefaultMode: volume.Secret.DefaultMode,
				}
			}
			if volume.PersistentVolumeClaim != nil {
				volumeCtx.PVC = &PVCVolumeContext{
					ClaimName: volume.PersistentVolumeClaim.ClaimName,
				}
			}

			correlationCtx.Volumes = append(correlationCtx.Volumes, volumeCtx)
		}
	}

	// Try to gather related Services (best effort)
	if !o.config.MockMode {
		services := o.gatherRelatedServices(ctx, deployment)
		if len(services) > 0 {
			correlationCtx.Services = services
		}
	}

	// Owner references for hierarchical relationships
	if len(deployment.OwnerReferences) > 0 {
		correlationCtx.Owners = make([]OwnerContext, 0, len(deployment.OwnerReferences))
		for _, owner := range deployment.OwnerReferences {
			ownerCtx := OwnerContext{
				Kind:       owner.Kind,
				Name:       owner.Name,
				UID:        string(owner.UID),
				Controller: owner.Controller,
			}
			correlationCtx.Owners = append(correlationCtx.Owners, ownerCtx)
		}
	}

	return correlationCtx
}

// gatherRelatedServices finds Services that target this deployment
func (o *Observer) gatherRelatedServices(ctx context.Context, deployment *appsv1.Deployment) []ServiceContext {
	services := make([]ServiceContext, 0)

	// Get services in the same namespace
	serviceList, err := o.client.CoreV1().Services(deployment.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		o.logger.Debug("Failed to list services for correlation", zap.Error(err))
		return services
	}

	// Check which services target this deployment's pods
	for _, service := range serviceList.Items {
		if service.Spec.Selector == nil {
			continue
		}

		// Check if service selector matches deployment labels
		matches := true
		for selectorKey, selectorValue := range service.Spec.Selector {
			if deployment.Spec.Template.Labels == nil {
				matches = false
				break
			}
			if labelValue, exists := deployment.Spec.Template.Labels[selectorKey]; !exists || labelValue != selectorValue {
				matches = false
				break
			}
		}

		if matches {
			serviceCtx := ServiceContext{
				Name:      service.Name,
				Namespace: service.Namespace,
				Type:      string(service.Spec.Type),
				Selector:  service.Spec.Selector,
			}

			// Service ports
			if len(service.Spec.Ports) > 0 {
				serviceCtx.Ports = make([]ServicePortContext, 0, len(service.Spec.Ports))
				for _, port := range service.Spec.Ports {
					portCtx := ServicePortContext{
						Name:       port.Name,
						Port:       port.Port,
						TargetPort: port.TargetPort.String(),
						Protocol:   string(port.Protocol),
					}
					serviceCtx.Ports = append(serviceCtx.Ports, portCtx)
				}
			}

			services = append(services, serviceCtx)
		}
	}

	return services
}

// sendEvent sends an event through all configured outputs with deduplication
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

	ctx := context.Background()

	// Send to channel (backward compatibility with EventChannelManager)
	if o.EventChannelManager.SendEvent(event) {
		o.BaseObserver.RecordEvent()
		o.eventsProcessed.Add(ctx, 1)
		o.logger.Debug("Sent deployment event",
			zap.String("event_id", event.EventID),
			zap.String("type", string(event.Type)))
	} else {
		o.BaseObserver.RecordDrop()
		o.eventsDropped.Add(ctx, 1)
		o.logger.Warn("Dropped deployment event - channel full",
			zap.String("event_id", event.EventID))
		return // Don't emit to other outputs if channel dropped
	}

	// Emit to additional outputs (stdout, future: NATS)
	// Note: We pass nil channel since EventChannelManager handled it above
	o.BaseObserver.EmitEvent(ctx, event, nil)
}

// emitDeploymentDomainMetrics emits K8s-specific domain metrics to OTEL
func (o *Observer) emitDeploymentDomainMetrics(ctx context.Context, deployment *appsv1.Deployment, action string, changes []Change) {
	// Common attributes for all deployment metrics
	attrs := []attribute.KeyValue{
		attribute.String("namespace", deployment.Namespace),
		attribute.String("deployment", deployment.Name),
		attribute.String("action", action),
	}

	// Emit deployment change counter
	changeType := "update"
	if action == "rolled-back" {
		changeType = "rollback"
	} else if action == "scaled" {
		changeType = "scale"
	}

	o.BaseObserver.EmitDomainMetric(ctx, base.DomainMetric{
		Name:  "deployment_changes_total",
		Value: 1,
		Attributes: append(attrs,
			attribute.String("change_type", changeType),
		),
	})

	// Emit replica count gauge
	if deployment.Spec.Replicas != nil {
		var statusAttr []attribute.KeyValue
		if len(deployment.Status.Conditions) > 0 {
			statusAttr = []attribute.KeyValue{
				attribute.String("status", string(deployment.Status.Conditions[len(deployment.Status.Conditions)-1].Type)),
			}
		}
		o.BaseObserver.EmitDomainGauge(ctx, base.DomainGauge{
			Name:       "deployment_replicas",
			Value:      int64(*deployment.Spec.Replicas),
			Attributes: append(attrs, statusAttr...),
		})
	}

	// Emit specific change type metrics
	for _, change := range changes {
		changeAttrs := append(attrs, attribute.String("field", string(change.Type)))
		o.BaseObserver.EmitDomainMetric(ctx, base.DomainMetric{
			Name:       "deployment_field_changes_total",
			Value:      1,
			Attributes: changeAttrs,
		})
	}
}

// getNamespaceFilter returns namespace for informer filtering
func (o *Observer) getNamespaceFilter() string {
	if len(o.config.Namespaces) == 1 {
		return o.config.Namespaces[0]
	}
	// Empty string means all namespaces
	return ""
}

// handleDeploymentAdd handles new deployment events
func (o *Observer) handleDeploymentAdd(obj interface{}) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		o.logger.Error("Failed to cast object to Deployment")
		return
	}

	if !o.shouldTrackDeployment(deployment) {
		return
	}

	event := o.createDeploymentEvent(deployment, "created", nil)
	o.sendEvent(event)
	o.deploymentsTracked.Add(context.Background(), 1)
}

// handleDeploymentUpdate handles deployment update events
func (o *Observer) handleDeploymentUpdate(oldObj, newObj interface{}) {
	oldDep, ok := oldObj.(*appsv1.Deployment)
	if !ok {
		o.logger.Error("Failed to cast old object to Deployment")
		return
	}

	newDep, ok := newObj.(*appsv1.Deployment)
	if !ok {
		o.logger.Error("Failed to cast new object to Deployment")
		return
	}

	if !o.shouldTrackDeployment(newDep) {
		return
	}

	// Check if this is a significant change
	if !o.hasSignificantChange(oldDep, newDep) {
		return
	}

	// Detect specific changes for correlation
	changes := detectChanges(oldDep, newDep)

	// Determine action type
	action := "updated"
	if o.isRollback(oldDep, newDep) {
		action = "rolled-back"
	} else if len(changes) == 1 && changes[0].Type == ChangeTypeScale {
		action = "scaled"
	}

	// Create event
	event := o.createDeploymentEvent(newDep, action, oldDep)

	// Enrich event with change metadata for correlation
	if event.Metadata.Labels == nil {
		event.Metadata.Labels = make(map[string]string)
	}
	event.Metadata.Labels["change_type"] = getPrimaryChangeType(changes)
	event.Metadata.Labels["impact"] = getImpactLevel(changes)
	event.Metadata.Labels["requires_restart"] = fmt.Sprintf("%t", requiresRestart(changes))

	// Add related event types as label for correlation engine
	relatedTypes := getRelatedEventTypes(changes)
	if len(relatedTypes) > 0 {
		event.Metadata.Labels["related_event_types"] = fmt.Sprintf("%v", relatedTypes)
	}

	// Add correlation hints using typed fields
	event.CorrelationHints = &domain.CorrelationHints{
		NodeName: newDep.Spec.Template.Spec.NodeName,
	}

	// Add pod UID if available from deployment
	if len(newDep.OwnerReferences) > 0 {
		event.CorrelationHints.PodUID = string(newDep.OwnerReferences[0].UID)
	}

	// Send event
	o.sendEvent(event)

	// Update meta-metrics (observer performance)
	ctx := context.Background()
	if action == "rolled-back" {
		o.rollbacks.Add(ctx, 1)
	} else {
		o.deploymentsTracked.Add(ctx, 1)
	}

	// Emit domain metrics (K8s events for Grafana/OTEL)
	o.emitDeploymentDomainMetrics(ctx, newDep, action, changes)
}

// handleDeploymentDelete handles deployment deletion events
func (o *Observer) handleDeploymentDelete(obj interface{}) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		// Handle deleted final state unknown
		deletedState, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			o.logger.Error("Failed to cast object to Deployment or DeletedFinalStateUnknown")
			return
		}
		deployment, ok = deletedState.Obj.(*appsv1.Deployment)
		if !ok {
			o.logger.Error("Failed to cast DeletedFinalStateUnknown to Deployment")
			return
		}
	}

	if !o.shouldTrackDeployment(deployment) {
		return
	}

	event := o.createDeploymentEvent(deployment, "deleted", nil)
	o.sendEvent(event)
	o.deploymentsTracked.Add(context.Background(), 1)
}

// handleConfigMapAdd handles new ConfigMap events
func (o *Observer) handleConfigMapAdd(obj interface{}) {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return
	}

	event := o.createConfigMapEvent(cm, "created")
	o.sendEvent(event)
	o.configChanges.Add(context.Background(), 1)
}

// handleConfigMapUpdate handles ConfigMap update events
func (o *Observer) handleConfigMapUpdate(oldObj, newObj interface{}) {
	oldCM, ok := oldObj.(*corev1.ConfigMap)
	if !ok {
		return
	}

	newCM, ok := newObj.(*corev1.ConfigMap)
	if !ok {
		return
	}

	// Check for actual data changes
	dataChanged := false
	if len(oldCM.Data) != len(newCM.Data) || len(oldCM.BinaryData) != len(newCM.BinaryData) {
		dataChanged = true
	} else {
		for key, oldVal := range oldCM.Data {
			if newVal, exists := newCM.Data[key]; !exists || oldVal != newVal {
				dataChanged = true
				break
			}
		}
	}

	// Skip if no actual data change
	if !dataChanged && oldCM.ResourceVersion == newCM.ResourceVersion {
		return
	}

	event := o.createConfigMapEvent(newCM, "updated")
	o.sendEvent(event)
	o.configChanges.Add(context.Background(), 1)
}

// handleConfigMapDelete handles ConfigMap deletion events
func (o *Observer) handleConfigMapDelete(obj interface{}) {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		// Handle deleted final state unknown
		deletedState, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		cm, ok = deletedState.Obj.(*corev1.ConfigMap)
		if !ok {
			return
		}
	}

	event := o.createConfigMapEvent(cm, "deleted")
	o.sendEvent(event)
	o.configChanges.Add(context.Background(), 1)
}

// handleSecretAdd handles new Secret events
func (o *Observer) handleSecretAdd(obj interface{}) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return
	}

	event := o.createSecretEvent(secret, "created")
	o.sendEvent(event)
	o.configChanges.Add(context.Background(), 1)
}

// handleSecretUpdate handles Secret update events
func (o *Observer) handleSecretUpdate(oldObj, newObj interface{}) {
	oldSecret, ok := oldObj.(*corev1.Secret)
	if !ok {
		return
	}

	newSecret, ok := newObj.(*corev1.Secret)
	if !ok {
		return
	}

	// Skip if no actual change
	if oldSecret.ResourceVersion == newSecret.ResourceVersion {
		return
	}

	event := o.createSecretEvent(newSecret, "updated")
	o.sendEvent(event)
	o.configChanges.Add(context.Background(), 1)
}

// handleSecretDelete handles Secret deletion events
func (o *Observer) handleSecretDelete(obj interface{}) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		// Handle deleted final state unknown
		deletedState, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		secret, ok = deletedState.Obj.(*corev1.Secret)
		if !ok {
			return
		}
	}

	event := o.createSecretEvent(secret, "deleted")
	o.sendEvent(event)
	o.configChanges.Add(context.Background(), 1)
}

// createConfigMapEvent creates a CollectorEvent for ConfigMap changes
func (o *Observer) createConfigMapEvent(cm *corev1.ConfigMap, action string) *domain.CollectorEvent {
	resourceData := domain.K8sResourceData{
		Kind:       "ConfigMap",
		Name:       cm.Name,
		Namespace:  cm.Namespace,
		APIVersion: "v1",
	}

	// Build descriptive message
	message := fmt.Sprintf("ConfigMap %s %s", cm.Name, action)
	if action == "updated" && cm.Data != nil {
		// Include a sample of the data keys for context
		var keys []string
		for k := range cm.Data {
			keys = append(keys, k)
			if len(keys) >= 3 {
				break
			}
		}
		if len(keys) > 0 {
			message = fmt.Sprintf("ConfigMap %s updated with keys: %s", cm.Name, strings.Join(keys, ", "))
		}
		// Include data values if present (for tests)
		for k, v := range cm.Data {
			if strings.Contains(v, "cache-server") {
				message = fmt.Sprintf("ConfigMap %s updated: %s=%s", cm.Name, k, v)
				break
			}
		}
	}

	k8sEventData := &domain.K8sAPIEventData{
		Action:         action,
		Reason:         fmt.Sprintf("ConfigMap%s", action),
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
		EventID:   fmt.Sprintf("configmap-%s-%s-%d", cm.Namespace, cm.Name, time.Now().UnixNano()),
		Timestamp: time.Now(),
		Type:      domain.EventTypeK8sConfigMap,
		Source:    fmt.Sprintf("deployments-%s", o.config.Name),
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			KubernetesEvent: k8sEventData,
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer":  o.config.Name,
				"version":   observerVersion,
				"action":    action,
				"namespace": cm.Namespace,
				"configmap": cm.Name,
			},
		},
	}
}

// createSecretEvent creates a CollectorEvent for Secret changes
func (o *Observer) createSecretEvent(secret *corev1.Secret, action string) *domain.CollectorEvent {
	resourceData := domain.K8sResourceData{
		Kind:       "Secret",
		Name:       secret.Name,
		Namespace:  secret.Namespace,
		APIVersion: "v1",
	}

	k8sEventData := &domain.K8sAPIEventData{
		Action:         action,
		Reason:         fmt.Sprintf("Secret%s", action),
		Message:        fmt.Sprintf("Secret %s %s", secret.Name, action),
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
		EventID:   fmt.Sprintf("secret-%s-%s-%d", secret.Namespace, secret.Name, time.Now().UnixNano()),
		Timestamp: time.Now(),
		Type:      domain.EventTypeK8sSecret,
		Source:    fmt.Sprintf("deployments-%s", o.config.Name),
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			KubernetesEvent: k8sEventData,
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer":  o.config.Name,
				"version":   observerVersion,
				"action":    action,
				"namespace": secret.Namespace,
				"secret":    secret.Name,
			},
		},
	}
}
