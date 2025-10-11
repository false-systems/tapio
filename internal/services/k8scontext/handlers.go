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
		fmt.Printf("handlePodAdd: unexpected type %T\n", obj)
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
		fmt.Printf("handlePodUpdate: unexpected old type %T\n", oldObj)
		return
	}

	newPod, ok := newObj.(*corev1.Pod)
	if !ok {
		fmt.Printf("handlePodUpdate: unexpected new type %T\n", newObj)
		return
	}

	oldPodCopy := oldPod.DeepCopy()
	newPodCopy := newPod.DeepCopy()

	// Enqueue async operations
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
}

// handlePodDelete is called when a pod is deleted
func (s *Service) handlePodDelete(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		fmt.Printf("handlePodDelete: unexpected type %T\n", obj)
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
		fmt.Printf("handleServiceAdd: unexpected type %T\n", obj)
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
		fmt.Printf("handleServiceUpdate: unexpected old type %T\n", oldObj)
		return
	}

	newService, ok := newObj.(*corev1.Service)
	if !ok {
		fmt.Printf("handleServiceUpdate: unexpected new type %T\n", newObj)
		return
	}

	oldServiceCopy := oldService.DeepCopy()
	newServiceCopy := newService.DeepCopy()

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
}

// handleServiceDelete is called when a service is deleted
func (s *Service) handleServiceDelete(obj interface{}) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		fmt.Printf("handleServiceDelete: unexpected type %T\n", obj)
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
		fmt.Printf("handleDeploymentAdd: unexpected type %T\n", obj)
		return
	}

	deploymentCopy := deployment.DeepCopy()
	s.enqueueEvent(func() error {
		return s.storeDeploymentMetadata(deploymentCopy)
	})
}

// handleDeploymentUpdate is called when a deployment is updated
func (s *Service) handleDeploymentUpdate(oldObj, newObj interface{}) {
	_, ok := oldObj.(*appsv1.Deployment)
	if !ok {
		fmt.Printf("handleDeploymentUpdate: unexpected old type %T\n", oldObj)
		return
	}

	newDeployment, ok := newObj.(*appsv1.Deployment)
	if !ok {
		fmt.Printf("handleDeploymentUpdate: unexpected new type %T\n", newObj)
		return
	}

	deploymentCopy := newDeployment.DeepCopy()
	s.enqueueEvent(func() error {
		return s.storeDeploymentMetadata(deploymentCopy)
	})
}

// handleDeploymentDelete is called when a deployment is deleted
func (s *Service) handleDeploymentDelete(obj interface{}) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		fmt.Printf("handleDeploymentDelete: unexpected type %T\n", obj)
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
		fmt.Printf("handleNodeAdd: unexpected type %T\n", obj)
		return
	}

	nodeCopy := node.DeepCopy()
	s.enqueueEvent(func() error {
		return s.storeNodeMetadata(nodeCopy)
	})
}

// handleNodeUpdate is called when a node is updated
func (s *Service) handleNodeUpdate(oldObj, newObj interface{}) {
	_, ok := oldObj.(*corev1.Node)
	if !ok {
		fmt.Printf("handleNodeUpdate: unexpected old type %T\n", oldObj)
		return
	}

	newNode, ok := newObj.(*corev1.Node)
	if !ok {
		fmt.Printf("handleNodeUpdate: unexpected new type %T\n", newObj)
		return
	}

	nodeCopy := newNode.DeepCopy()
	s.enqueueEvent(func() error {
		return s.storeNodeMetadata(nodeCopy)
	})
}

// handleNodeDelete is called when a node is deleted
func (s *Service) handleNodeDelete(obj interface{}) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		fmt.Printf("handleNodeDelete: unexpected type %T\n", obj)
		return
	}

	nodeCopy := node.DeepCopy()
	s.enqueueEvent(func() error {
		return s.deleteNodeMetadata(nodeCopy)
	})
}
