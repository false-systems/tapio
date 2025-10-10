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

	if err := s.storePodMetadata(pod); err != nil {
		fmt.Printf("failed to store pod metadata for %s/%s: %v\n", pod.Namespace, pod.Name, err)
	}
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

	// If IP changed, delete old entry
	if oldPod.Status.PodIP != "" && oldPod.Status.PodIP != newPod.Status.PodIP {
		if err := s.deletePodMetadata(oldPod); err != nil {
			fmt.Printf("failed to delete old pod metadata for %s/%s: %v\n", oldPod.Namespace, oldPod.Name, err)
		}
	}

	// Store updated metadata
	if err := s.storePodMetadata(newPod); err != nil {
		fmt.Printf("failed to store updated pod metadata for %s/%s: %v\n", newPod.Namespace, newPod.Name, err)
	}
}

// handlePodDelete is called when a pod is deleted
func (s *Service) handlePodDelete(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		fmt.Printf("handlePodDelete: unexpected type %T\n", obj)
		return
	}

	if err := s.deletePodMetadata(pod); err != nil {
		fmt.Printf("failed to delete pod metadata for %s/%s: %v\n", pod.Namespace, pod.Name, err)
	}
}

// handleServiceAdd is called when a new service is created
func (s *Service) handleServiceAdd(obj interface{}) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		fmt.Printf("handleServiceAdd: unexpected type %T\n", obj)
		return
	}

	if err := s.storeServiceMetadata(service); err != nil {
		fmt.Printf("failed to store service metadata for %s/%s: %v\n", service.Namespace, service.Name, err)
	}
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

	// If ClusterIP changed, delete old entry
	if oldService.Spec.ClusterIP != "" && oldService.Spec.ClusterIP != "None" &&
		oldService.Spec.ClusterIP != newService.Spec.ClusterIP {
		if err := s.deleteServiceMetadata(oldService); err != nil {
			fmt.Printf("failed to delete old service metadata for %s/%s: %v\n", oldService.Namespace, oldService.Name, err)
		}
	}

	// Store updated metadata
	if err := s.storeServiceMetadata(newService); err != nil {
		fmt.Printf("failed to store updated service metadata for %s/%s: %v\n", newService.Namespace, newService.Name, err)
	}
}

// handleServiceDelete is called when a service is deleted
func (s *Service) handleServiceDelete(obj interface{}) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		fmt.Printf("handleServiceDelete: unexpected type %T\n", obj)
		return
	}

	if err := s.deleteServiceMetadata(service); err != nil {
		fmt.Printf("failed to delete service metadata for %s/%s: %v\n", service.Namespace, service.Name, err)
	}
}
