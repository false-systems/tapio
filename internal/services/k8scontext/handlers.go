package k8scontext

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// NOTE: interface{} parameters required by Kubernetes client-go cache.ResourceEventHandler
// All handlers perform type assertions with proper error handling (see service.go event handler wrappers)

// handlePodAdd is called when a new pod is created
func (s *Service) handlePodAdd(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		s.logger.Error().
			Str("handler", "handlePodAdd").
			Str("unexpected_type", fmt.Sprintf("%T", obj)).
			Msg("type assertion failed")
		return
	}

	// Enqueue async write to NATS KV
	podCopy := pod.DeepCopy()
	s.enqueueEvent(func() error {
		if err := s.storePodMetadata(podCopy); err != nil {
			return err
		}
		return s.storeOwnerMetadata(podCopy)
	})
}

// handlePodUpdate is called when a pod is updated
func (s *Service) handlePodUpdate(oldObj, newObj interface{}) {
	oldPod, ok := oldObj.(*corev1.Pod)
	if !ok {
		s.logger.Error().
			Str("handler", "handlePodUpdate").
			Str("unexpected_type", fmt.Sprintf("%T", oldObj)).
			Str("object", "old").
			Msg("type assertion failed")
		return
	}

	newPod, ok := newObj.(*corev1.Pod)
	if !ok {
		s.logger.Error().
			Str("handler", "handlePodUpdate").
			Str("unexpected_type", fmt.Sprintf("%T", newObj)).
			Str("object", "new").
			Msg("type assertion failed")
		return
	}

	oldPodCopy := oldPod.DeepCopy()
	newPodCopy := newPod.DeepCopy()

	// Task 1: Store metadata (existing)
	s.enqueueEvent(func() error {
		// If IP changed, delete old entry
		if oldPodCopy.Status.PodIP != "" && oldPodCopy.Status.PodIP != newPodCopy.Status.PodIP {
			if err := s.deletePodMetadata(oldPodCopy); err != nil {
				return fmt.Errorf("failed to delete old pod: %w", err)
			}
		}

		// Store updated metadata
		if err := s.storePodMetadata(newPodCopy); err != nil {
			return err
		}
		return s.storeOwnerMetadata(newPodCopy)
	})

	// Task 2: Detect changes and emit events (NEW)
	s.detectPodChanges(s.ctx, oldPodCopy, newPodCopy)
}

// handlePodDelete is called when a pod is deleted
func (s *Service) handlePodDelete(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		s.logger.Error().
			Str("handler", "handlePodDelete").
			Str("unexpected_type", fmt.Sprintf("%T", obj)).
			Msg("type assertion failed")
		return
	}

	podCopy := pod.DeepCopy()
	s.enqueueEvent(func() error {
		if err := s.deletePodMetadata(podCopy); err != nil {
			return err
		}
		return s.deleteOwnerMetadata(podCopy)
	})
}

// handleServiceAdd is called when a new service is created
func (s *Service) handleServiceAdd(obj interface{}) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		s.logger.Error().
			Str("handler", "handleServiceAdd").
			Str("unexpected_type", fmt.Sprintf("%T", obj)).
			Msg("type assertion failed")
		return
	}

	serviceCopy := service.DeepCopy()
	s.enqueueEvent(func() error {
		return s.storeServiceMetadata(serviceCopy)
	})
}

// handleServiceUpdate is called when a service is updated
func (s *Service) handleServiceUpdate(oldObj, newObj interface{}) {
	oldService, ok := oldObj.(*corev1.Service)
	if !ok {
		s.logger.Error().
			Str("handler", "handleServiceUpdate").
			Str("unexpected_type", fmt.Sprintf("%T", oldObj)).
			Str("object", "old").
			Msg("type assertion failed")
		return
	}

	newService, ok := newObj.(*corev1.Service)
	if !ok {
		s.logger.Error().
			Str("handler", "handleServiceUpdate").
			Str("unexpected_type", fmt.Sprintf("%T", newObj)).
			Str("object", "new").
			Msg("type assertion failed")
		return
	}

	oldServiceCopy := oldService.DeepCopy()
	newServiceCopy := newService.DeepCopy()

	// Task 1: Store metadata (existing)
	s.enqueueEvent(func() error {
		// If ClusterIP changed, delete old entry
		if oldServiceCopy.Spec.ClusterIP != "" && oldServiceCopy.Spec.ClusterIP != "None" &&
			oldServiceCopy.Spec.ClusterIP != newServiceCopy.Spec.ClusterIP {
			if err := s.deleteServiceMetadata(oldServiceCopy); err != nil {
				return fmt.Errorf("failed to delete old service: %w", err)
			}
		}

		// Store updated metadata
		return s.storeServiceMetadata(newServiceCopy)
	})

	// Task 2: Detect changes and emit events (NEW)
	s.detectServiceChanges(s.ctx, oldServiceCopy, newServiceCopy)
}

// handleServiceDelete is called when a service is deleted
func (s *Service) handleServiceDelete(obj interface{}) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		s.logger.Error().
			Str("handler", "handleServiceDelete").
			Str("unexpected_type", fmt.Sprintf("%T", obj)).
			Msg("type assertion failed")
		return
	}

	serviceCopy := service.DeepCopy()
	s.enqueueEvent(func() error {
		return s.deleteServiceMetadata(serviceCopy)
	})
}

// handleDeploymentAdd is called when a new deployment is created
func (s *Service) handleDeploymentAdd(obj interface{}) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		s.logger.Error().
			Str("handler", "handleDeploymentAdd").
			Str("unexpected_type", fmt.Sprintf("%T", obj)).
			Msg("type assertion failed")
		return
	}

	deploymentCopy := deployment.DeepCopy()
	s.enqueueEvent(func() error {
		return s.storeDeploymentMetadata(deploymentCopy)
	})
}

// handleDeploymentUpdate is called when a deployment is updated
func (s *Service) handleDeploymentUpdate(oldObj, newObj interface{}) {
	oldDeployment, ok := oldObj.(*appsv1.Deployment)
	if !ok {
		s.logger.Error().
			Str("handler", "handleDeploymentUpdate").
			Str("unexpected_type", fmt.Sprintf("%T", oldObj)).
			Str("object", "old").
			Msg("type assertion failed")
		return
	}

	newDeployment, ok := newObj.(*appsv1.Deployment)
	if !ok {
		s.logger.Error().
			Str("handler", "handleDeploymentUpdate").
			Str("unexpected_type", fmt.Sprintf("%T", newObj)).
			Str("object", "new").
			Msg("type assertion failed")
		return
	}

	deploymentCopy := newDeployment.DeepCopy()
	oldDeploymentCopy := oldDeployment.DeepCopy()

	// Task 1: Store metadata (existing)
	s.enqueueEvent(func() error {
		return s.storeDeploymentMetadata(deploymentCopy)
	})

	// Task 2: Detect changes and emit events (NEW)
	s.detectDeploymentChanges(s.ctx, oldDeploymentCopy, deploymentCopy)
}

// handleDeploymentDelete is called when a deployment is deleted
func (s *Service) handleDeploymentDelete(obj interface{}) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		s.logger.Error().
			Str("handler", "handleDeploymentDelete").
			Str("unexpected_type", fmt.Sprintf("%T", obj)).
			Msg("type assertion failed")
		return
	}

	deploymentCopy := deployment.DeepCopy()
	s.enqueueEvent(func() error {
		return s.deleteDeploymentMetadata(deploymentCopy)
	})
}

// handleReplicaSetAdd is called when a new replicaset is created
// We use this to track Pod → Deployment ownership
func (s *Service) handleReplicaSetAdd(obj interface{}) {
	// ReplicaSet events are used for ownership tracking
	// For now, we don't store ReplicaSet metadata directly
}

// handleReplicaSetUpdate is called when a replicaset is updated
func (s *Service) handleReplicaSetUpdate(oldObj, newObj interface{}) {
	// No-op for now
}

// handleReplicaSetDelete is called when a replicaset is deleted
func (s *Service) handleReplicaSetDelete(obj interface{}) {
	// No-op for now
}

// handleNodeAdd is called when a new node is created
func (s *Service) handleNodeAdd(obj interface{}) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		s.logger.Error().
			Str("handler", "handleNodeAdd").
			Str("unexpected_type", fmt.Sprintf("%T", obj)).
			Msg("type assertion failed")
		return
	}

	nodeCopy := node.DeepCopy()
	s.enqueueEvent(func() error {
		return s.storeNodeMetadata(nodeCopy)
	})
}

// handleNodeUpdate is called when a node is updated
func (s *Service) handleNodeUpdate(oldObj, newObj interface{}) {
	oldNode, ok := oldObj.(*corev1.Node)
	if !ok {
		s.logger.Error().
			Str("handler", "handleNodeUpdate").
			Str("unexpected_type", fmt.Sprintf("%T", oldObj)).
			Str("object", "old").
			Msg("type assertion failed")
		return
	}

	newNode, ok := newObj.(*corev1.Node)
	if !ok {
		s.logger.Error().
			Str("handler", "handleNodeUpdate").
			Str("unexpected_type", fmt.Sprintf("%T", newObj)).
			Str("object", "new").
			Msg("type assertion failed")
		return
	}

	nodeCopy := newNode.DeepCopy()
	oldNodeCopy := oldNode.DeepCopy()

	// Task 1: Store metadata (existing)
	s.enqueueEvent(func() error {
		return s.storeNodeMetadata(nodeCopy)
	})

	// Task 2: Detect changes and emit events (NEW)
	s.detectNodeChanges(s.ctx, oldNodeCopy, nodeCopy)
}

// handleNodeDelete is called when a node is deleted
func (s *Service) handleNodeDelete(obj interface{}) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		s.logger.Error().
			Str("handler", "handleNodeDelete").
			Str("unexpected_type", fmt.Sprintf("%T", obj)).
			Msg("type assertion failed")
		return
	}

	nodeCopy := node.DeepCopy()
	s.enqueueEvent(func() error {
		return s.deleteNodeMetadata(nodeCopy)
	})
}
