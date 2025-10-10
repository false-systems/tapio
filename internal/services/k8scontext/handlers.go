package k8scontext

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

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
		return s.storePodMetadata(podCopy)
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
		return s.storePodMetadata(newPodCopy)
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
		return s.deletePodMetadata(podCopy)
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
