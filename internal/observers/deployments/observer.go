package deployments

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence"
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

	// Intelligence service for event emission
	Emitter intelligence.Service
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
	emitter  intelligence.Service

	// Deployment-specific Prometheus metrics
	deploymentUpdates *prometheus.Counter
	replicaChanges    *prometheus.Counter
	conditionChanges  *prometheus.Counter
	imageUpdates      *prometheus.Counter
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

	// Create deployment-specific Prometheus metrics
	err = base.NewPromMetricBuilder(base.GlobalRegistry, name).
		Counter(&obs.deploymentUpdates, "deployment_updates_total", "Deployment update events").
		Counter(&obs.replicaChanges, "replica_changes_total", "Replica count changes").
		Counter(&obs.conditionChanges, "condition_changes_total", "Deployment condition changes").
		Counter(&obs.imageUpdates, "image_updates_total", "Container image updates").
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

	// Increment Prometheus metrics based on event type
	ctx := context.Background()
	(*o.deploymentUpdates).Inc()

	// Check if replica changed
	replicaChanged, _, _ := detectReplicaChange(oldDeploy, newDeploy)
	if replicaChanged {
		(*o.replicaChanges).Inc()
	}

	// Check if condition changed
	condChanged, _, _ := detectConditionChange(oldDeploy, newDeploy)
	if condChanged {
		(*o.conditionChanges).Inc()
	}

	// Check if image changed
	imageChanged, _, _ := detectImageChange(oldDeploy, newDeploy)
	if imageChanged {
		(*o.imageUpdates).Inc()
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

// detectImageChange detects container image changes (supports multi-container)
func detectImageChange(oldDeploy, newDeploy *appsv1.Deployment) (bool, []string, []string) {
	if oldDeploy == nil || newDeploy == nil {
		return false, []string{}, []string{}
	}

	oldImages := extractImages(oldDeploy)
	newImages := extractImages(newDeploy)

	changed := !equalImageLists(oldImages, newImages)
	return changed, oldImages, newImages
}

// extractImages extracts all container images from deployment
func extractImages(deploy *appsv1.Deployment) []string {
	if deploy == nil {
		return []string{}
	}

	containers := deploy.Spec.Template.Spec.Containers
	images := make([]string, 0, len(containers))
	for _, container := range containers {
		images = append(images, container.Image)
	}
	return images
}

// equalImageLists compares two image lists
func equalImageLists(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
	evt.K8sData.ResourceNamespace = deploy.Namespace
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

		// Check for image changes (highest priority)
		imageChanged, oldImages, newImages := detectImageChange(oldDeploy, newDeploy)
		if imageChanged {
			evt.Type = "deployment_image_updated"
			evt.Subtype = "deployment_image_updated" // Update Subtype for NATS routing
			evt.K8sData.ImageChanged = true

			// Find which image changed (handles updates, additions, and removals)
			found := false
			maxLen := len(oldImages)
			if len(newImages) > maxLen {
				maxLen = len(newImages)
			}

			for i := 0; i < maxLen; i++ {
				oldImg := ""
				if i < len(oldImages) {
					oldImg = oldImages[i]
				}
				newImg := ""
				if i < len(newImages) {
					newImg = newImages[i]
				}

				if oldImg != newImg {
					evt.K8sData.OldImage = oldImg
					evt.K8sData.NewImage = newImg
					found = true
					break
				}
			}

			// Fallback: if somehow no change found, use first images
			if !found && len(oldImages) > 0 && len(newImages) > 0 {
				evt.K8sData.OldImage = oldImages[0]
				evt.K8sData.NewImage = newImages[0]
			}
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
