package k8scontext

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// toPodInfo transforms K8s Pod to PodInfo
func toPodInfo(pod *corev1.Pod) PodInfo {
	labels := pod.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	return PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		PodIP:     pod.Status.PodIP,
		HostIP:    pod.Status.HostIP,
		Labels:    labels,
	}
}

// toServiceInfo transforms K8s Service to ServiceInfo
func toServiceInfo(service *corev1.Service) ServiceInfo {
	labels := service.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	return ServiceInfo{
		Name:      service.Name,
		Namespace: service.Namespace,
		ClusterIP: service.Spec.ClusterIP,
		Type:      string(service.Spec.Type),
		Labels:    labels,
	}
}

// makePodKey generates NATS KV key for pod
func makePodKey(ip string) string {
	return fmt.Sprintf("pod.ip.%s", ip)
}

// makeServiceKey generates NATS KV key for service
func makeServiceKey(ip string) string {
	return fmt.Sprintf("service.ip.%s", ip)
}

// shouldSkipPod returns true if pod should be skipped
func shouldSkipPod(pod *corev1.Pod) bool {
	return pod.Status.PodIP == ""
}

// shouldSkipService returns true if service should be skipped
func shouldSkipService(service *corev1.Service) bool {
	return service.Spec.ClusterIP == "" || service.Spec.ClusterIP == "None"
}

// serializePodInfo marshals PodInfo to JSON
func serializePodInfo(podInfo PodInfo) ([]byte, error) {
	data, err := json.Marshal(podInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PodInfo: %w", err)
	}
	return data, nil
}

// serializeServiceInfo marshals ServiceInfo to JSON
func serializeServiceInfo(serviceInfo ServiceInfo) ([]byte, error) {
	data, err := json.Marshal(serviceInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ServiceInfo: %w", err)
	}
	return data, nil
}

// storePodMetadata writes pod metadata to NATS KV
func (s *Service) storePodMetadata(pod *corev1.Pod) error {
	if shouldSkipPod(pod) {
		return nil
	}

	podInfo := toPodInfo(pod)
	data, err := serializePodInfo(podInfo)
	if err != nil {
		return err
	}

	key := makePodKey(pod.Status.PodIP)
	_, err = s.kv.Put(key, data)
	if err != nil {
		return fmt.Errorf("failed to store pod metadata for %s: %w", key, err)
	}

	return nil
}

// deletePodMetadata removes pod metadata from NATS KV
func (s *Service) deletePodMetadata(pod *corev1.Pod) error {
	if shouldSkipPod(pod) {
		return nil
	}

	key := makePodKey(pod.Status.PodIP)
	err := s.kv.Delete(key)
	if err != nil {
		return fmt.Errorf("failed to delete pod metadata for %s: %w", key, err)
	}

	return nil
}

// storeServiceMetadata writes service metadata to NATS KV
func (s *Service) storeServiceMetadata(svc *corev1.Service) error {
	if shouldSkipService(svc) {
		return nil
	}

	serviceInfo := toServiceInfo(svc)
	data, err := serializeServiceInfo(serviceInfo)
	if err != nil {
		return err
	}

	key := makeServiceKey(svc.Spec.ClusterIP)
	_, err = s.kv.Put(key, data)
	if err != nil {
		return fmt.Errorf("failed to store service metadata for %s: %w", key, err)
	}

	return nil
}

// deleteServiceMetadata removes service metadata from NATS KV
func (s *Service) deleteServiceMetadata(svc *corev1.Service) error {
	if shouldSkipService(svc) {
		return nil
	}

	key := makeServiceKey(svc.Spec.ClusterIP)
	err := s.kv.Delete(key)
	if err != nil {
		return fmt.Errorf("failed to delete service metadata for %s: %w", key, err)
	}

	return nil
}
