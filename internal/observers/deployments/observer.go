package deployments

import (
	"fmt"

	"github.com/yairfalse/tapio/internal/base"
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
