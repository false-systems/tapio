package deployments

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/metric"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Config holds deployments observer configuration
type Config struct {
	// K8s client for watching Deployments
	Clientset kubernetes.Interface

	// Namespace to watch (empty = all namespaces)
	Namespace string

	// Event emitter (OTEL, Tapio, or both)
	Emitter base.Emitter

	// Output configuration
	Output base.OutputConfig
}

// Validate checks config is valid
func (c *Config) Validate() error {
	if c.Clientset == nil {
		return fmt.Errorf("clientset is required")
	}
	return nil
}

// DeploymentsObserver monitors Kubernetes deployments using informers
type DeploymentsObserver struct {
	*base.BaseObserver
	config   Config
	informer cache.SharedIndexInformer
	emitter  base.Emitter

	// Deployment-specific OTEL metrics
	deploymentUpdates metric.Int64Counter
	replicaChanges    metric.Int64Counter
	conditionChanges  metric.Int64Counter
}

// NewDeploymentsObserver creates a new deployments observer
func NewDeploymentsObserver(name string, config Config) (*DeploymentsObserver, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create base observer
	baseObs, err := base.NewBaseObserver(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create base observer: %w", err)
	}

	obs := &DeploymentsObserver{
		BaseObserver: baseObs,
		config:       config,
		emitter:      config.Emitter,
	}

	// Create deployment-specific OTEL metrics
	err = base.NewMetricBuilder(name).
		Counter(&obs.deploymentUpdates, "deployment_updates_total", "Deployment update events").
		Counter(&obs.replicaChanges, "replica_changes_total", "Replica count changes").
		Counter(&obs.conditionChanges, "condition_changes_total", "Deployment condition changes").
		Build()

	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	// Create informer for deployments
	factory := informers.NewSharedInformerFactoryWithOptions(
		config.Clientset,
		30*time.Second,
		informers.WithNamespace(config.Namespace),
	)

	obs.informer = factory.Apps().V1().Deployments().Informer()

	// Register event handlers
	if _, err := obs.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    obs.handleAdd,
		UpdateFunc: obs.handleUpdate,
		DeleteFunc: obs.handleDelete,
	}); err != nil {
		return nil, fmt.Errorf("failed to add event handlers: %w", err)
	}

	return obs, nil
}

// Start initiates the deployments observer
func (o *DeploymentsObserver) Start(ctx context.Context) error {
	logger := o.Logger(ctx)

	// Add informer to pipeline
	o.AddStage(func(ctx context.Context) error {
		logger.Info().Msg("Starting deployments informer")
		o.informer.Run(ctx.Done())
		return ctx.Err()
	})

	// Start base observer (runs pipeline via errgroup)
	if err := o.BaseObserver.Start(ctx); err != nil {
		return fmt.Errorf("failed to start base observer: %w", err)
	}

	logger.Info().Msg("Deployments observer started")
	return nil
}

// Stop gracefully stops the deployments observer
func (o *DeploymentsObserver) Stop() error {
	ctx := context.Background()
	logger := o.Logger(ctx)
	logger.Info().Msg("Stopping deployments observer")

	return o.BaseObserver.Stop()
}

// handleAdd processes deployment creation
func (o *DeploymentsObserver) handleAdd(obj interface{}) {
	deploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		return
	}

	evt := createDomainEvent(nil, deploy)
	o.emitEvent(context.Background(), evt)
}

// handleUpdate processes deployment updates
func (o *DeploymentsObserver) handleUpdate(oldObj, newObj interface{}) {
	oldDeploy, ok := oldObj.(*appsv1.Deployment)
	if !ok {
		return
	}

	newDeploy, ok := newObj.(*appsv1.Deployment)
	if !ok {
		return
	}

	evt := createDomainEvent(oldDeploy, newDeploy)

	// Increment OTEL metrics based on event type
	ctx := context.Background()
	o.deploymentUpdates.Add(ctx, 1)

	// Check if replica changed
	replicaChanged, _, _ := detectReplicaChange(oldDeploy, newDeploy)
	if replicaChanged {
		o.replicaChanges.Add(ctx, 1)
	}

	// Check if condition changed
	condChanged, _, _ := detectConditionChange(oldDeploy, newDeploy)
	if condChanged {
		o.conditionChanges.Add(ctx, 1)
	}

	o.emitEvent(ctx, evt)
}

// handleDelete processes deployment deletion
func (o *DeploymentsObserver) handleDelete(obj interface{}) {
	deploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		return
	}

	evt := createDomainEvent(deploy, nil)
	o.emitEvent(context.Background(), evt)
}

// emitEvent emits a domain event
func (o *DeploymentsObserver) emitEvent(ctx context.Context, evt *domain.ObserverEvent) {
	if o.emitter == nil {
		return
	}

	if evt.Source == "" {
		evt.Source = "deployments"
	}

	o.RecordEvent(ctx)

	if err := o.emitter.Emit(ctx, evt); err != nil {
		logger := o.Logger(ctx)
		logger.Error().Err(err).Msg("Failed to emit event")
		o.RecordError(ctx, evt)
	}
}

// detectEventType determines event type based on old/new deployment state
func detectEventType(oldDeploy, newDeploy *appsv1.Deployment) string {
	if oldDeploy == nil && newDeploy != nil {
		return "deployment_created"
	}
	if oldDeploy != nil && newDeploy == nil {
		return "deployment_deleted"
	}
	return "deployment_updated"
}

// detectReplicaChange checks if replica count changed between deployments
func detectReplicaChange(oldDeploy, newDeploy *appsv1.Deployment) (bool, int32, int32) {
	oldReplicas := int32(0)
	if oldDeploy != nil && oldDeploy.Spec.Replicas != nil {
		oldReplicas = *oldDeploy.Spec.Replicas
	}

	newReplicas := int32(0)
	if newDeploy != nil && newDeploy.Spec.Replicas != nil {
		newReplicas = *newDeploy.Spec.Replicas
	}

	changed := oldReplicas != newReplicas
	return changed, oldReplicas, newReplicas
}

// detectConditionChange detects condition status changes (Available, Progressing, etc)
func detectConditionChange(oldDeploy, newDeploy *appsv1.Deployment) (bool, string, string) {
	if oldDeploy == nil || newDeploy == nil {
		return false, "", ""
	}

	// Check Available condition (most important)
	oldCond := getCondition(oldDeploy, "Available")
	newCond := getCondition(newDeploy, "Available")

	if oldCond != newCond {
		return true, "Available", newCond
	}

	return false, "", ""
}

// getCondition extracts condition status from deployment
func getCondition(deploy *appsv1.Deployment, condType string) string {
	for _, cond := range deploy.Status.Conditions {
		if string(cond.Type) == condType {
			return string(cond.Status)
		}
	}
	return ""
}

// createDomainEvent creates domain event from deployment change
func createDomainEvent(oldDeploy, newDeploy *appsv1.Deployment) *domain.ObserverEvent {
	eventType := detectEventType(oldDeploy, newDeploy)

	evt := &domain.ObserverEvent{
		ID:        uuid.New().String(),
		Type:      eventType,
		Subtype:   eventType, // Use Type as Subtype for NATS routing
		Source:    "deployments",
		Timestamp: time.Now(),
		K8sData:   &domain.K8sEventData{},
	}

	// Get deployment name and namespace from non-nil deployment
	deploy := newDeploy
	if deploy == nil {
		deploy = oldDeploy
	}

	evt.K8sData.ResourceKind = "Deployment"
	evt.K8sData.ResourceName = deploy.Name
	evt.K8sData.Action = mapEventTypeToAction(eventType)

	// Only check changes for updates (not create/delete)
	if oldDeploy != nil && newDeploy != nil {
		// Check for replica changes
		replicaChanged, oldReplicas, newReplicas := detectReplicaChange(oldDeploy, newDeploy)
		if replicaChanged {
			evt.Type = "deployment_scaled"
			evt.Subtype = "deployment_scaled" // Update Subtype for NATS routing
			evt.K8sData.ReplicasChanged = true
			evt.K8sData.OldReplicas = oldReplicas
			evt.K8sData.NewReplicas = newReplicas
		}

		// Check for condition changes (higher priority)
		condChanged, condType, status := detectConditionChange(oldDeploy, newDeploy)
		if condChanged {
			evt.Type = "deployment_available"
			evt.Subtype = "deployment_available" // Update Subtype for NATS routing
			evt.K8sData.Reason = condType
			evt.K8sData.Message = status
		}
	}

	return evt
}

// mapEventTypeToAction maps event type to K8s action
func mapEventTypeToAction(eventType string) string {
	switch eventType {
	case "deployment_created":
		return "created"
	case "deployment_deleted":
		return "deleted"
	default:
		return "updated"
	}
}
