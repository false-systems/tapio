package deployments

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
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
			evt.K8sData.ReplicasChanged = true
			evt.K8sData.OldReplicas = oldReplicas
			evt.K8sData.NewReplicas = newReplicas
		}

		// Check for condition changes (higher priority)
		condChanged, condType, status := detectConditionChange(oldDeploy, newDeploy)
		if condChanged {
			evt.Type = "deployment_available"
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
